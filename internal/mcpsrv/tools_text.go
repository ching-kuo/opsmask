package mcpsrv

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ching-kuo/opsmask/internal/detect"
	"github.com/ching-kuo/opsmask/internal/engine"
	"github.com/ching-kuo/opsmask/internal/runtime"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// MaskTextInput is the input schema for the mask_text tool.
type MaskTextInput struct {
	Text        string `json:"text" jsonschema:"text to mask"`
	ASCIITokens bool   `json:"ascii_tokens,omitempty" jsonschema:"emit [[opsmask:...]] ASCII tokens instead of unicode brackets"`
}

// MaskTextOutput is the output schema for the mask_text tool.
type MaskTextOutput struct {
	Text      string         `json:"text"`
	Masked    int            `json:"masked"`
	Destroyed int            `json:"destroyed"`
	ByType    map[string]int `json:"by_type"`
}

// DetectTextInput is the input schema for the detect_text tool.
type DetectTextInput struct {
	Text           string `json:"text" jsonschema:"text to scan"`
	IncludeMatches bool   `json:"include_matches,omitempty" jsonschema:"include per-match offsets in the response"`
}

// DetectTextMatch is one match returned when IncludeMatches is true.
type DetectTextMatch struct {
	Type  string `json:"type"`
	Start int    `json:"start"`
	End   int    `json:"end"`
}

// DetectTextOutput is the output schema for the detect_text tool.
type DetectTextOutput struct {
	Count   int               `json:"count"`
	ByType  map[string]int    `json:"by_type"`
	Matches []DetectTextMatch `json:"matches,omitempty"`
}

func registerTextTools(srv *mcp.Server, rt *runtime.Env, audit AuditWriter, caps Caps) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "mask_text",
		Description: "Mask sensitive values in text using project detectors. Persists pseudonyms.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in MaskTextInput) (*mcp.CallToolResult, MaskTextOutput, error) {
		start := time.Now()
		out, err := handleMaskText(ctx, rt, in, caps)
		writeAudit(audit, McpCallRecord{
			Tool:            "mask_text",
			ArgsSummary:     map[string]any{"text_bytes": len(in.Text), "ascii_tokens": in.ASCIITokens},
			OK:              err == nil,
			ErrClass:        errClass(err),
			ResultSizeBytes: len(out.Text),
			DurationMs:      time.Since(start).Milliseconds(),
		})
		return nil, out, err
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "detect_text",
		Description: "Scan text for sensitive values without persisting. Returns counts by type.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in DetectTextInput) (*mcp.CallToolResult, DetectTextOutput, error) {
		start := time.Now()
		out, err := handleDetectText(ctx, rt, in, caps)
		writeAudit(audit, McpCallRecord{
			Tool:            "detect_text",
			ArgsSummary:     map[string]any{"text_bytes": len(in.Text), "include_matches": in.IncludeMatches},
			OK:              err == nil,
			ErrClass:        errClass(err),
			ResultSizeBytes: out.Count,
			DurationMs:      time.Since(start).Milliseconds(),
		})
		return nil, out, err
	})
}

func handleMaskText(ctx context.Context, rt *runtime.Env, in MaskTextInput, caps Caps) (MaskTextOutput, error) {
	if len(in.Text) > caps.MaxTextBytes {
		return MaskTextOutput{}, fmt.Errorf("INPUT_TOO_LARGE: text=%d bytes exceeds cap=%d", len(in.Text), caps.MaxTextBytes)
	}
	// Owned bounded buffer — never inherit a writer from the SDK or transport.
	// engine.Process must operate on an in-memory writer for the cancellation
	// contract from U3 to hold.
	var out bytes.Buffer
	stats, err := engine.Process(ctx, strings.NewReader(in.Text), &out, rt.Rules, rt.Alloc, engine.Options{
		ASCIITokens: in.ASCIITokens,
	})
	if err != nil {
		return MaskTextOutput{}, err
	}
	return MaskTextOutput{
		Text:      out.String(),
		Masked:    stats.Masked,
		Destroyed: stats.Destroyed,
		ByType:    stats.ByType,
	}, nil
}

func handleDetectText(ctx context.Context, rt *runtime.Env, in DetectTextInput, caps Caps) (DetectTextOutput, error) {
	if len(in.Text) > caps.MaxTextBytes {
		return DetectTextOutput{}, fmt.Errorf("INPUT_TOO_LARGE: text=%d bytes exceeds cap=%d", len(in.Text), caps.MaxTextBytes)
	}
	stats, matches, err := detect.Scan(ctx, []byte(in.Text), rt.Rules)
	if err != nil {
		return DetectTextOutput{}, err
	}
	res := DetectTextOutput{Count: stats.Masked + stats.Destroyed, ByType: stats.ByType}
	if in.IncludeMatches {
		res.Matches = make([]DetectTextMatch, len(matches))
		for i, m := range matches {
			res.Matches[i] = DetectTextMatch{Type: m.Type, Start: m.Start, End: m.End}
		}
	}
	return res, nil
}

func writeAudit(w AuditWriter, rec McpCallRecord) {
	// Fail-open: log to server stderr only (no MCP-visible signal) per the
	// risk-table contract. The CLI mcp serve wires os.Stderr at startup;
	// tests pass nil and skip auditing.
	if w == nil {
		return
	}
	if af, ok := w.(*AuditFile); ok && af != nil {
		_ = af.Write(rec)
	}
}

func errClass(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	if i := strings.Index(msg, ":"); i > 0 {
		return msg[:i]
	}
	return msg
}
