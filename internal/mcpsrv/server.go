// Package mcpsrv exposes OpsMask's masking, detection, exec, and
// observability capabilities to MCP clients (Claude Desktop, Claude
// Code, Cursor, Copilot) over the standard MCP stdio JSON-RPC transport.
package mcpsrv

import (
	"github.com/ching-kuo/opsmask/internal/runtime"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Version is the server implementation version reported during the MCP
// handshake. Set at build time via -ldflags or overridden by the caller.
var Version = "dev"

// AuditWriter is the lean MCP-call audit sink. Production wires
// *AuditFile (mcp_calls.jsonl); tests can pass a fake or nil. The
// server calls Close on shutdown so in-flight records flush before
// the runtime store closes.
type AuditWriter interface {
	Write(rec McpCallRecord) error
	Close() error
}

// Caps are the configurable per-call size caps. Production callers can leave
// the zero value; NewServer applies defaults (4 MiB each). Tests construct
// smaller caps to exercise INPUT_TOO_LARGE.
type Caps struct {
	MaxTextBytes       int
	MaxExecOutputBytes int
}

// DefaultCaps returns the v1 production caps. The 4 MiB ceiling is dictated
// by JSON-RPC stdio framing budgets; per-line is plenty for realistic log
// snippets while keeping memory bounded.
func DefaultCaps() Caps {
	return Caps{
		MaxTextBytes:       4 << 20,
		MaxExecOutputBytes: 4 << 20,
	}
}

// NewServer constructs an MCP server bound to the given runtime and audit
// writer. Tools and the mapping resource are registered with default caps.
//
// Capabilities advertised: tools and resources, both without subscription
// support — the v1 contract is read-on-demand snapshots only.
func NewServer(rt *runtime.Env, audit AuditWriter) *mcp.Server {
	return NewServerWithCaps(rt, audit, DefaultCaps())
}

// NewServerWithCaps is NewServer with explicit Caps. Used in tests.
func NewServerWithCaps(rt *runtime.Env, audit AuditWriter, caps Caps) *mcp.Server {
	if caps.MaxTextBytes <= 0 {
		caps.MaxTextBytes = DefaultCaps().MaxTextBytes
	}
	if caps.MaxExecOutputBytes <= 0 {
		caps.MaxExecOutputBytes = DefaultCaps().MaxExecOutputBytes
	}
	impl := &mcp.Implementation{
		Name:    "opsmask",
		Title:   "OpsMask",
		Version: Version,
	}
	opts := &mcp.ServerOptions{
		Instructions: "OpsMask masks sensitive log text before it reaches an LLM. " +
			"Tools never return plaintext; unmask is intentionally CLI-only.",
		Capabilities: &mcp.ServerCapabilities{
			Tools:     &mcp.ToolCapabilities{},
			Resources: &mcp.ResourceCapabilities{Subscribe: false},
		},
	}
	srv := mcp.NewServer(impl, opts)
	if rt != nil {
		registerTextTools(srv, rt, audit, caps)
		registerObservabilityTools(srv, rt, audit)
		registerExecTool(srv, rt, audit, caps)
		registerMappingResource(srv, rt, audit)
	}
	return srv
}
