package exec_test

import (
	"context"
	"strings"
	"testing"

	maskexec "github.com/ching-kuo/opsmask/internal/exec"
)

func TestIsExecChildPresenceSemantics(t *testing.T) {
	t.Setenv("OPSMASK_EXEC_CHILD", "")
	if !maskexec.IsExecChild() {
		t.Fatal("empty marker value should still count as present")
	}
}

func TestRunInjectsExecChildMarker(t *testing.T) {
	var out strings.Builder
	res := maskexec.Run(context.Background(), []string{"sh", "-c", "printf %s \"$OPSMASK_EXEC_CHILD\""}, maskexec.RunOptions{Stdout: &out})
	if res.ExitCode != 0 {
		t.Fatalf("exit = %d class=%s", res.ExitCode, res.ErrorClass)
	}
	if out.String() != "1" {
		t.Fatalf("OPSMASK_EXEC_CHILD = %q, want 1", out.String())
	}
}
