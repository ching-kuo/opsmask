package exec

import (
	"context"
	"errors"
	"io"
	"os"
	osexec "os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/ching-kuo/opsmask/internal/detect"
	"github.com/ching-kuo/opsmask/internal/engine"
	maskio "github.com/ching-kuo/opsmask/internal/ioutil"
	"github.com/ching-kuo/opsmask/internal/pseudo"
)

type RunOptions struct {
	Env       []string
	Stdin     io.Reader
	Stdout    io.Writer
	Stderr    io.Writer
	Timeout   time.Duration
	KillGrace time.Duration
	Rules     []detect.Rule
	Alloc     *pseudo.Allocator
}

type RunResult struct {
	ExitCode   int
	Duration   time.Duration
	ErrorClass string
	// Masking stats aggregated across stdout and stderr after maskStream
	// drains both pipes. CLI exec ignores these; the MCP exec tool surfaces
	// them in its result so the agent can see how much was masked.
	Masked    int
	Destroyed int
	ByType    map[string]int
}

func Run(ctx context.Context, argv []string, opts RunOptions) RunResult {
	start := time.Now()
	if opts.KillGrace <= 0 {
		opts.KillGrace = 5 * time.Second
	}
	cmd := osexec.Command(argv[0], argv[1:]...)
	cmd.Env = opts.Env
	cmd.Stdin = opts.Stdin
	setProcessGroup(cmd)
	// Manage pipes manually instead of StdoutPipe/StderrPipe. cmd.Wait closes
	// any StdoutPipe/StderrPipe on completion, which can race with the
	// reader goroutines that drain output for engine.Process. Manual pipes
	// stay open until our readers see EOF (after the child closes its
	// write ends on exit), then we close them ourselves.
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		return RunResult{ExitCode: 125, Duration: time.Since(start), ErrorClass: "wrapper"}
	}
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		closeAll(stdoutR, stdoutW)
		return RunResult{ExitCode: 125, Duration: time.Since(start), ErrorClass: "wrapper"}
	}
	cmd.Stdout = stdoutW
	cmd.Stderr = stderrW
	// CloseOnExecAll sets FD_CLOEXEC on every fd >= 3 the parent has open,
	// including the pipe fds we just created. Safe because the child's
	// stdout/stderr (fd 1/2) come from dup2 after fork — dup2 clears
	// FD_CLOEXEC, so writes survive exec. The parent's higher-numbered
	// pipe fds keeping FD_CLOEXEC means a future grandchild fork wouldn't
	// inherit them.
	CloseOnExecAll()
	if err := cmd.Start(); err != nil {
		closeAll(stdoutR, stdoutW, stderrR, stderrW)
		code := 125
		class := "wrapper"
		if errors.Is(err, osexec.ErrNotFound) {
			code, class = 127, "not_found"
		}
		return RunResult{ExitCode: code, Duration: time.Since(start), ErrorClass: class}
	}
	// Close the parent's copies of the write ends now that the child has
	// inherited them. Without this, the readers never see EOF because the
	// kernel keeps the pipe open as long as any process holds the write end.
	_ = stdoutW.Close()
	_ = stderrW.Close()
	var streamWg sync.WaitGroup
	streamWg.Add(2)
	var stdoutStats, stderrStats engine.Stats
	go func() {
		defer streamWg.Done()
		stdoutStats = maskStream(ctx, stdoutR, opts.Stdout, opts)
		_ = stdoutR.Close()
	}()
	go func() {
		defer streamWg.Done()
		stderrStats = maskStream(ctx, stderrR, opts.Stderr, opts)
		_ = stderrR.Close()
	}()

	waitCh := make(chan error, 1)
	go func() { waitCh <- cmd.Wait() }()

	waitErr, timedOut := waitForChild(ctx, cmd, waitCh, opts.Timeout, opts.KillGrace)
	streamWg.Wait()

	merged := mergeStats(stdoutStats, stderrStats)
	if timedOut {
		return RunResult{ExitCode: 124, Duration: time.Since(start), ErrorClass: "timeout", Masked: merged.Masked, Destroyed: merged.Destroyed, ByType: merged.ByType}
	}
	code := 0
	class := ""
	if waitErr != nil {
		var ee *osexec.ExitError
		if errors.As(waitErr, &ee) {
			code = ee.ExitCode()
			if code != 0 {
				class = "non_zero"
			}
		} else {
			code = 125
			class = "wrapper"
		}
	}
	return RunResult{ExitCode: code, Duration: time.Since(start), ErrorClass: class, Masked: merged.Masked, Destroyed: merged.Destroyed, ByType: merged.ByType}
}

func mergeStats(a, b engine.Stats) engine.Stats {
	if len(a.ByType) == 0 && len(b.ByType) == 0 {
		return engine.Stats{Masked: a.Masked + b.Masked, Destroyed: a.Destroyed + b.Destroyed}
	}
	out := engine.Stats{Masked: a.Masked + b.Masked, Destroyed: a.Destroyed + b.Destroyed, ByType: map[string]int{}}
	for k, v := range a.ByType {
		out.ByType[k] += v
	}
	for k, v := range b.ByType {
		out.ByType[k] += v
	}
	return out
}

func waitForChild(ctx context.Context, cmd *osexec.Cmd, waitCh chan error, timeout, grace time.Duration) (waitErr error, timedOut bool) {
	var timerC <-chan time.Time
	if timeout > 0 {
		t := time.NewTimer(timeout)
		defer t.Stop()
		timerC = t.C
	}
	select {
	case err := <-waitCh:
		return err, false
	case <-timerC:
	case <-ctx.Done():
	}
	// Defensive: every caller is post-Start so cmd.Process is non-nil, but
	// guarding here keeps a future contract change from turning a nil deref
	// into a process-group SIGKILL on pid -1.
	if cmd.Process == nil {
		return <-waitCh, false
	}
	signalGroup(cmd.Process.Pid, syscall.SIGTERM)
	graceTimer := time.NewTimer(grace)
	defer graceTimer.Stop()
	select {
	case err := <-waitCh:
		return err, true
	case <-graceTimer.C:
		signalGroup(cmd.Process.Pid, syscall.SIGKILL)
		return <-waitCh, true
	}
}

func closeAll(closers ...io.Closer) {
	for _, c := range closers {
		_ = c.Close()
	}
}

// Allocator is shared across stdout and stderr goroutines; pseudo.Allocator's
// mutex guarantees safe concurrent CommitPlans.
func maskStream(ctx context.Context, src io.Reader, dst io.Writer, opts RunOptions) engine.Stats {
	if dst == nil {
		_, _ = io.Copy(io.Discard, src)
		return engine.Stats{}
	}
	if len(opts.Rules) == 0 || opts.Alloc == nil {
		_, _ = io.Copy(dst, src)
		return engine.Stats{}
	}
	stats, _ := engine.Process(ctx, src, dst, opts.Rules, opts.Alloc, engine.Options{MaxLine: maskio.DefaultMaxLine})
	return stats
}
