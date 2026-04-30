package detect

import (
	"context"

	"github.com/ching-kuo/opsmask/internal/policy"
)

// ScanStats mirrors engine.Stats so callers handling either path
// (mask vs detect-only) can use the same shape.
type ScanStats struct {
	Masked    int
	Destroyed int
	ByType    map[string]int
}

// Match offsets returned when the caller asks for them. Start/End are byte
// offsets into the original input; Type is the rule type.
type ScanMatch struct {
	Start, End int
	Type       string
}

// Scan runs the detector rules over b without persisting any pseudonyms.
// It is the side-effect-free path used by the MCP `detect_text` tool.
//
// The returned counts populate ByType for every rule that matched. Masked
// counts pseudonymize-policy rules; Destroyed counts destroy-policy rules.
// Both keep parity with engine.Process so the same code-paths can compute
// totals.
//
// Cancellation: Scan returns ctx.Err() at two checkpoints — before
// FindMatches (which is CPU-bound on long inputs) and during result
// aggregation. For multi-megabyte inputs Scan returns within ~10 ms of
// cancellation in practice; the bound depends on FindMatches' inner loop
// which the standard regexp package does not interrupt.
func Scan(ctx context.Context, b []byte, rules []Rule) (ScanStats, []ScanMatch, error) {
	stats := ScanStats{ByType: map[string]int{}}
	if err := ctx.Err(); err != nil {
		return stats, nil, err
	}
	if len(b) == 0 || len(rules) == 0 {
		return stats, nil, nil
	}
	matches := FindMatches(rules, b)
	if err := ctx.Err(); err != nil {
		return stats, nil, err
	}
	out := make([]ScanMatch, 0, len(matches))
	for _, m := range matches {
		stats.ByType[m.Rule.Type]++
		switch m.Rule.Policy {
		case policy.Destroy:
			stats.Destroyed++
		default:
			stats.Masked++
		}
		out = append(out, ScanMatch{Start: m.Start, End: m.End, Type: m.Rule.Type})
	}
	return stats, out, nil
}
