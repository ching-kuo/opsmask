package exec

import (
	"context"
	"errors"
	"io"
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
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return RunResult{ExitCode: 125, Duration: time.Since(start), ErrorClass: "wrapper"}
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return RunResult{ExitCode: 125, Duration: time.Since(start), ErrorClass: "wrapper"}
	}
	CloseOnExecAll()
	if err := cmd.Start(); err != nil {
		code := 125
		class := "wrapper"
		if errors.Is(err, osexec.ErrNotFound) {
			code, class = 127, "not_found"
		}
		return RunResult{ExitCode: code, Duration: time.Since(start), ErrorClass: class}
	}
	var streamWg sync.WaitGroup
	streamWg.Add(2)
	go func() { defer streamWg.Done(); maskStream(ctx, stdout, opts.Stdout, opts) }()
	go func() { defer streamWg.Done(); maskStream(ctx, stderr, opts.Stderr, opts) }()

	waitCh := make(chan error, 1)
	go func() { waitCh <- cmd.Wait() }()

	waitErr, timedOut := waitForChild(ctx, cmd, waitCh, opts.Timeout, opts.KillGrace)
	streamWg.Wait()

	if timedOut {
		return RunResult{ExitCode: 124, Duration: time.Since(start), ErrorClass: "timeout"}
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
	return RunResult{ExitCode: code, Duration: time.Since(start), ErrorClass: class}
}

func waitForChild(ctx context.Context, cmd *osexec.Cmd, waitCh chan error, timeout, grace time.Duration) (error, bool) {
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

// Allocator is shared across stdout and stderr goroutines; pseudo.Allocator's
// mutex guarantees safe concurrent CommitPlans.
func maskStream(ctx context.Context, src io.Reader, dst io.Writer, opts RunOptions) {
	if dst == nil {
		_, _ = io.Copy(io.Discard, src)
		return
	}
	if len(opts.Rules) == 0 || opts.Alloc == nil {
		_, _ = io.Copy(dst, src)
		return
	}
	_, _ = engine.Process(ctx, src, dst, opts.Rules, opts.Alloc, engine.Options{MaxLine: maskio.DefaultMaxLine})
}
