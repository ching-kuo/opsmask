// Package mcpsrv exposes the OpsMask masking, detection, exec, and observability
// capabilities to MCP clients (Claude Code, Cursor, Copilot) over stdio JSON-RPC.
//
// The package wires the relocated runtime, the shared exec orchestrator, and
// the audit writers into a Server constructed via NewServer. Subsequent units
// (U5/U6) attach the actual tool and resource handlers; U1 stands up only the
// scaffolding required for a clean handshake.
package mcpsrv

import (
	"github.com/ching-kuo/opsmask/internal/runtime"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Version is the server implementation version reported during the MCP
// handshake. Set at build time via -ldflags or overridden by the caller.
var Version = "dev"

// AuditWriter is the lean MCP-call audit sink. Callers pass an implementation
// (production: internal/mcpsrv.OpenAuditWriter; tests: an in-memory fake) and
// the server uses it to record every tool/resource invocation.
//
// U2 finalizes the contract; U1 keeps the type opaque so future tool handlers
// can take a dependency on it without further plumbing.
type AuditWriter interface {
	// Close releases the underlying file handle. The server calls Close as
	// part of orderly shutdown so in-flight records flush before the runtime
	// store is closed.
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
