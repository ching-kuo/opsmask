package exec_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/ching-kuo/opsmask/internal/config"
	maskexec "github.com/ching-kuo/opsmask/internal/exec"
	"github.com/ching-kuo/opsmask/internal/pseudo"
)

func TestOrchestrateHookRunsBashAndAuditsHookSource(t *testing.T) {
	setAuditDir(t)
	rt := maskexec.Runtime{Alloc: pseudo.New([]byte("01234567890123456789012345678901"), nil), Cfg: config.ExecConfig{Scope: config.ScopeFreeform}}
	var out bytes.Buffer
	res, err := maskexec.OrchestrateHook(context.Background(), rt, "printf hi", maskexec.HookOptions{Stdout: &out})
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 0 || out.String() != "hi" {
		t.Fatalf("exit=%d out=%q", res.ExitCode, out.String())
	}
	if res.Audit.Source != maskexec.SourceHook {
		t.Fatalf("source = %q", res.Audit.Source)
	}
}

func TestRegularOrchestrateStillDeniesBash(t *testing.T) {
	setAuditDir(t)
	rt := maskexec.Runtime{Cfg: config.ExecConfig{Enabled: true, Scope: config.ScopeFreeform, Allow: []config.AllowEntry{{Name: "all", MatchFunc: func([]string) bool { return true }}}}}
	res, err := maskexec.Orchestrate(context.Background(), rt, []string{"bash", "-c", "true"}, maskexec.OrchestrateOptions{Source: maskexec.SourceCLI})
	if !errors.Is(err, maskexec.ErrPolicyDenied) || res.Audit.DenyMatch != "bash" {
		t.Fatalf("err = %v, want bash policy denial", err)
	}
}
