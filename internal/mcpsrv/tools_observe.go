package mcpsrv

import (
	"context"
	"time"

	"github.com/ching-kuo/opsmask/internal/runtime"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// MappingStatsInput has no fields — listed for schema completeness so AddTool
// can infer the parameter object.
type MappingStatsInput struct{}

// MappingStatsOutput mirrors store.Stats. Fields are token-only counts; no
// plaintext or HMAC bytes ever flow through this tool.
type MappingStatsOutput struct {
	Total  int            `json:"total"`
	ByType map[string]int `json:"by_type"`
}

// ListDetectorsInput has no fields.
type ListDetectorsInput struct{}

// ListDetectorsOutput names the rules currently active for this server.
type ListDetectorsOutput struct {
	Detectors []DetectorInfo `json:"detectors"`
}

// DetectorInfo describes one detector.
type DetectorInfo struct {
	Name   string `json:"name"`
	Type   string `json:"type"`
	Policy string `json:"policy"`
}

func registerObservabilityTools(srv *mcp.Server, rt *runtime.Env, audit AuditWriter) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "mapping_stats",
		Description: "Return per-type counts of pseudonyms currently in the mapping store.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ MappingStatsInput) (*mcp.CallToolResult, MappingStatsOutput, error) {
		start := time.Now()
		stats, err := rt.Store.Stats(ctx)
		writeAudit(audit, McpCallRecord{
			Tool:       "mapping_stats",
			OK:         err == nil,
			ErrClass:   errClass(err),
			DurationMs: time.Since(start).Milliseconds(),
		})
		if err != nil {
			return nil, MappingStatsOutput{}, err
		}
		return nil, MappingStatsOutput{Total: stats.Total, ByType: stats.ByType}, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_detectors",
		Description: "Return the active detector rule set (built-ins plus project rules).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ ListDetectorsInput) (*mcp.CallToolResult, ListDetectorsOutput, error) {
		start := time.Now()
		out := ListDetectorsOutput{Detectors: make([]DetectorInfo, 0, len(rt.Rules))}
		for _, r := range rt.Rules {
			out.Detectors = append(out.Detectors, DetectorInfo{
				Name:   r.Name,
				Type:   r.Type,
				Policy: string(r.Policy),
			})
		}
		writeAudit(audit, McpCallRecord{
			Tool:            "list_detectors",
			OK:              true,
			ResultSizeBytes: len(out.Detectors),
			DurationMs:      time.Since(start).Milliseconds(),
		})
		return nil, out, nil
	})
}
