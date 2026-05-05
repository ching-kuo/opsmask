package mcpsrv_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ching-kuo/opsmask/internal/config"
	"github.com/ching-kuo/opsmask/internal/mcpsrv"
	mcpruntime "github.com/ching-kuo/opsmask/internal/runtime"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func startServer(t *testing.T, audit mcpsrv.AuditWriter, caps mcpsrv.Caps) (*mcp.ClientSession, func()) {
	t.Helper()
	rt := newTestRuntime(t)
	return startServerWithRuntime(t, rt, audit, caps)
}

func startServerWithRuntime(t *testing.T, rt *mcpruntime.Env, audit mcpsrv.AuditWriter, caps mcpsrv.Caps) (*mcp.ClientSession, func()) {
	t.Helper()
	srv := mcpsrv.NewServerWithCaps(rt, audit, caps)
	clientT, serverT := mcp.NewInMemoryTransports()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)

	go func() { _ = srv.Run(ctx, serverT) }()

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	sess, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		cancel()
		t.Fatalf("Connect: %v", err)
	}
	return sess, func() { _ = sess.Close(); cancel() }
}

// callTool issues a single CallTool request and unmarshals the structured
// content into the typed output.
func callTool[Out any](t *testing.T, sess *mcp.ClientSession, name string, args map[string]any) Out {
	t.Helper()
	res, err := sess.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool %s: %v", name, err)
	}
	if res.IsError {
		var msg strings.Builder
		for _, c := range res.Content {
			b, _ := c.MarshalJSON()
			msg.Write(b)
		}
		t.Fatalf("CallTool %s returned error: %s", name, msg.String())
	}
	var out Out
	if res.StructuredContent != nil {
		raw, err := json.Marshal(res.StructuredContent)
		if err != nil {
			t.Fatalf("re-marshal structured content: %v", err)
		}
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("unmarshal structured content: %v", err)
		}
	}
	return out
}

func TestMaskTextHappyPath(t *testing.T) {
	sess, cleanup := startServer(t, nil, mcpsrv.DefaultCaps())
	defer cleanup()

	out := callTool[mcpsrv.MaskTextOutput](t, sess, "mask_text", map[string]any{
		"text": "the IP is 10.0.0.1 and email a@b.com",
	})
	if out.Masked < 2 {
		t.Fatalf("Masked = %d, want >= 2", out.Masked)
	}
	if strings.Contains(out.Text, "10.0.0.1") {
		t.Fatalf("masked output still contains plaintext: %q", out.Text)
	}
	if !strings.Contains(out.Text, "opsmask:") {
		t.Fatalf("masked output missing sentinel token: %q", out.Text)
	}
}

func TestMaskTextUsesTrustedHostnameInternalTLDs(t *testing.T) {
	rt := newRuntimeWithTrustedProjectConfig(t, "detectors:\n  hostname:\n    internal_tlds: [acme]\n", "")
	sess, cleanup := startServerWithRuntime(t, rt, nil, mcpsrv.DefaultCaps())
	defer cleanup()

	out := callTool[mcpsrv.MaskTextOutput](t, sess, "mask_text", map[string]any{
		"text":         "db-1.prod.acme",
		"ascii_tokens": true,
	})
	if out.ByType["hostname"] != 1 || strings.Contains(out.Text, "db-1.prod.acme") {
		t.Fatalf("out = %+v", out)
	}
}

func TestMaskTextIgnoresConfigOnlyHostnameInternalTLDs(t *testing.T) {
	configPath := writePrivateConfigFile(t, "detectors:\n  hostname:\n    internal_tlds: [acme]\n")
	rt := newRuntimeWithTrustedProjectConfig(t, "", configPath)
	sess, cleanup := startServerWithRuntime(t, rt, nil, mcpsrv.DefaultCaps())
	defer cleanup()

	out := callTool[mcpsrv.MaskTextOutput](t, sess, "mask_text", map[string]any{
		"text":         "db-1.prod.acme",
		"ascii_tokens": true,
	})
	if out.ByType["hostname"] != 0 || !strings.Contains(out.Text, "db-1.prod.acme") {
		t.Fatalf("out = %+v", out)
	}
}

func TestMaskTextInputTooLarge(t *testing.T) {
	caps := mcpsrv.Caps{MaxTextBytes: 64, MaxExecOutputBytes: 1 << 20}
	sess, cleanup := startServer(t, nil, caps)
	defer cleanup()

	res, err := sess.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "mask_text",
		Arguments: map[string]any{"text": strings.Repeat("a", 200)},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected error result")
	}
	body, _ := json.Marshal(res.Content)
	if !strings.Contains(string(body), "INPUT_TOO_LARGE") {
		t.Fatalf("error content = %s, want INPUT_TOO_LARGE", body)
	}
}

func TestDetectTextDoesNotPersist(t *testing.T) {
	sess, cleanup := startServer(t, nil, mcpsrv.DefaultCaps())
	defer cleanup()

	// First call detect_text — must NOT persist any rows.
	det := callTool[mcpsrv.DetectTextOutput](t, sess, "detect_text", map[string]any{
		"text": "10.0.0.1 and a@b.com",
	})
	if det.Count < 2 {
		t.Fatalf("Count = %d, want >= 2", det.Count)
	}

	stats := callTool[mcpsrv.MappingStatsOutput](t, sess, "mapping_stats", map[string]any{})
	if stats.Total != 0 {
		t.Fatalf("after detect_text mapping_stats.Total = %d, want 0", stats.Total)
	}

	// Now call mask_text — this MUST persist.
	_ = callTool[mcpsrv.MaskTextOutput](t, sess, "mask_text", map[string]any{
		"text": "10.0.0.1 and a@b.com",
	})
	stats = callTool[mcpsrv.MappingStatsOutput](t, sess, "mapping_stats", map[string]any{})
	if stats.Total == 0 {
		t.Fatal("after mask_text mapping_stats.Total = 0, want > 0")
	}
}

func TestDetectTextReturnsMatchesWhenAsked(t *testing.T) {
	sess, cleanup := startServer(t, nil, mcpsrv.DefaultCaps())
	defer cleanup()

	out := callTool[mcpsrv.DetectTextOutput](t, sess, "detect_text", map[string]any{
		"text":            "ip 10.0.0.1 ip 10.0.0.2",
		"include_matches": true,
	})
	if len(out.Matches) != 2 {
		t.Fatalf("matches = %d, want 2", len(out.Matches))
	}
}

func TestListDetectorsReturnsBuiltins(t *testing.T) {
	sess, cleanup := startServer(t, nil, mcpsrv.DefaultCaps())
	defer cleanup()

	out := callTool[mcpsrv.ListDetectorsOutput](t, sess, "list_detectors", map[string]any{})
	if len(out.Detectors) == 0 {
		t.Fatal("Detectors empty")
	}
}

func TestSchemaBudgetIsBounded(t *testing.T) {
	sess, cleanup := startServer(t, nil, mcpsrv.DefaultCaps())
	defer cleanup()

	tools, err := sess.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	body, err := json.Marshal(tools)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if len(body) > 4096 {
		t.Fatalf("tool list schema = %d bytes, want < 4096", len(body))
	}
}

func newRuntimeWithTrustedProjectConfig(t *testing.T, body string, configPath string) *mcpruntime.Env {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	projectRoot := t.TempDir()
	if body != "" {
		opsmaskDir := filepath.Join(projectRoot, ".opsmask")
		if err := os.Mkdir(opsmaskDir, 0o700); err != nil {
			t.Fatal(err)
		}
		cfg := filepath.Join(opsmaskDir, "config.yaml")
		if err := os.WriteFile(cfg, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := config.Trust(cfg); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(projectRoot)
	mappingDir := t.TempDir()
	if err := os.Chmod(mappingDir, 0o700); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	env, err := mcpruntime.New(mcpruntime.Options{
		Mapping: filepath.Join(mappingDir, "mapping.sqlite"),
		Config:  configPath,
	})
	if err != nil {
		t.Fatalf("runtime.New: %v", err)
	}
	t.Cleanup(func() { _ = env.Close() })
	return env
}

func writePrivateConfigFile(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfg, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return cfg
}
