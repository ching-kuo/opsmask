package mcpsrv

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ching-kuo/opsmask/internal/detect"
	"github.com/ching-kuo/opsmask/internal/runtime"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	// RFC 6570 form-style query expansion: {?limit} matches optional
	// `?limit=N`. The SDK uses github.com/yosida95/uritemplate/v3 for
	// matching, which requires this explicit form.
	mappingResourceURI = "opsmask://mapping/{type}{?limit}"
	mappingDefaultLim  = 50
	mappingMaxLim      = 500
)

// MappingResourceBody is the JSON shape returned by the mapping resource.
// Plaintext (RealValue) and HMACFull are deliberately absent; the resource
// returns sentinel-token form only so the MCP client cannot use it as a
// cross-store correlation oracle.
type MappingResourceBody struct {
	Type      string                 `json:"type"`
	Entries   []MappingResourceEntry `json:"entries"`
	Truncated bool                   `json:"truncated"`
}

// MappingResourceEntry is one row, projected to safe fields. Length is the
// byte count of the underlying real value — useful for capacity planning
// without leaking content.
type MappingResourceEntry struct {
	Token  string `json:"token"`
	Length int    `json:"length"`
}

func registerMappingResource(srv *mcp.Server, rt *runtime.Env, audit AuditWriter) {
	srv.AddResourceTemplate(&mcp.ResourceTemplate{
		URITemplate: mappingResourceURI,
		Name:        "mapping",
		Description: "Read pseudonym tokens by type. Returns tokens only — no plaintext, no HMAC bytes.",
		MIMEType:    "application/json",
	}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		start := time.Now()
		body, typ, err := readMappingResource(ctx, rt, req.Params.URI)
		writeAudit(audit, McpCallRecord{
			Tool:            "resource:mapping",
			ArgsSummary:     map[string]any{"type": typ, "uri": req.Params.URI},
			OK:              err == nil,
			ErrClass:        errClass(err),
			ResultSizeBytes: len(body),
			DurationMs:      time.Since(start).Milliseconds(),
		})
		if err != nil {
			return nil, err
		}
		return &mcp.ReadResourceResult{
			Contents: []*mcp.ResourceContents{{
				URI:      req.Params.URI,
				MIMEType: "application/json",
				Text:     string(body),
			}},
		}, nil
	})
}

func readMappingResource(ctx context.Context, rt *runtime.Env, uri string) ([]byte, string, error) {
	typ, limit, err := parseMappingURI(uri)
	if err != nil {
		return nil, "", err
	}
	rows, err := rt.Store.List(ctx, typ, limit+1) // +1 to detect truncation
	if err != nil {
		return nil, typ, fmt.Errorf("list mappings: %w", err)
	}
	truncated := false
	if len(rows) > limit {
		rows = rows[:limit]
		truncated = true
	}
	body := MappingResourceBody{
		Type:    typ,
		Entries: make([]MappingResourceEntry, 0, len(rows)),
	}
	for _, r := range rows {
		body.Entries = append(body.Entries, MappingResourceEntry{
			Token:  detect.RenderToken(r.Type, r.Index, false),
			Length: len(r.RealValue),
		})
	}
	body.Truncated = truncated
	out, err := json.Marshal(body)
	if err != nil {
		return nil, typ, err
	}
	return out, typ, nil
}

// parseMappingURI accepts opsmask://mapping/{type}[?limit=N]. The MCP SDK
// resolves the URI template before dispatch but still hands the raw URI to
// the handler; this function tolerates both the resolved form and the raw
// templated form (during tests/spec compliance).
func parseMappingURI(uri string) (string, int, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return "", 0, fmt.Errorf("invalid mapping URI %q: %w", uri, err)
	}
	if u.Scheme != "opsmask" {
		return "", 0, errors.New("mapping URI must use opsmask:// scheme")
	}
	if u.Host != "mapping" {
		return "", 0, errors.New("mapping URI must use opsmask://mapping/{type}")
	}
	typ := strings.Trim(u.Path, "/")
	if typ == "" || typ == "{type}" {
		return "", 0, errors.New("mapping URI requires a type segment")
	}
	limit := mappingDefaultLim
	if v := u.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return "", 0, fmt.Errorf("invalid limit %q", v)
		}
		limit = n
	}
	if limit > mappingMaxLim {
		limit = mappingMaxLim
	}
	return typ, limit, nil
}
