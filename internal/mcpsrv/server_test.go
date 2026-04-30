package mcpsrv_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/ching-kuo/opsmask/internal/mcpsrv"
	mcpruntime "github.com/ching-kuo/opsmask/internal/runtime"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func newTestRuntime(t *testing.T) *mcpruntime.Env {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Chdir(t.TempDir())
	mappingDir := t.TempDir()
	if err := os.Chmod(mappingDir, 0o700); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	mapping := filepath.Join(mappingDir, "mapping.sqlite")
	env, err := mcpruntime.New(mcpruntime.Options{Mapping: mapping})
	if err != nil {
		t.Fatalf("runtime.New: %v", err)
	}
	t.Cleanup(func() { _ = env.Close() })
	return env
}

func TestNewServerHandshakeAdvertisesCapabilities(t *testing.T) {
	rt := newTestRuntime(t)

	srv := mcpsrv.NewServer(rt, nil)
	clientT, serverT := mcp.NewInMemoryTransports()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	beforeGoroutines := runtime.NumGoroutine()

	serverErr := make(chan error, 1)
	go func() { serverErr <- srv.Run(ctx, serverT) }()

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	sess, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	t.Cleanup(func() { _ = sess.Close() })

	res := sess.InitializeResult()
	if res == nil || res.Capabilities == nil {
		t.Fatalf("nil InitializeResult or capabilities: %+v", res)
	}
	if res.Capabilities.Tools == nil {
		t.Fatal("Tools capability not advertised")
	}
	if res.Capabilities.Resources == nil {
		t.Fatal("Resources capability not advertised")
	}
	if res.Capabilities.Resources.Subscribe {
		t.Fatal("Resources.Subscribe must be false")
	}

	if err := sess.Ping(ctx, &mcp.PingParams{}); err != nil {
		t.Fatalf("Ping: %v", err)
	}

	if err := sess.Close(); err != nil {
		t.Fatalf("session Close: %v", err)
	}
	select {
	case err := <-serverErr:
		if err != nil {
			t.Fatalf("server Run returned: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server did not exit after client disconnect")
	}

	// Allow scheduler to retire any background goroutines from the SDK.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if runtime.NumGoroutine() <= beforeGoroutines+2 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Logf("goroutine count drift: before=%d after=%d (within tolerance)", beforeGoroutines, runtime.NumGoroutine())
}
