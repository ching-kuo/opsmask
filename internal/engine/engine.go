package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"syscall"
	"unicode/utf8"

	"github.com/ching-kuo/opsmask/internal/detect"
	"github.com/ching-kuo/opsmask/internal/ioutil"
	"github.com/ching-kuo/opsmask/internal/policy"
	"github.com/ching-kuo/opsmask/internal/pseudo"
)

const (
	boundaryCarry = 4095
	tokenProbe    = 8 << 10
)

type Options struct {
	ASCIITokens bool
	MaxLine     int
	Warn        func(string)
}

type Stats struct {
	Masked    int
	Destroyed int
	ByType    map[string]int
}

func Process(ctx context.Context, r io.Reader, w io.Writer, rules []detect.Rule, alloc *pseudo.Allocator, opts Options) (Stats, error) {
	stats := Stats{ByType: map[string]int{}}
	ch := ioutil.NewChunker(r, opts.MaxLine)
	tokenASCII := opts.ASCIITokens
	tokenFormChosen := opts.ASCIITokens
	var pending []byte
	// binaryWarned latches across the whole run so we emit at most one
	// "binary input replaced" warning regardless of how many segments contain
	// binary runs (R7).
	var binaryWarned bool
	warnOnce := func() {
		if !binaryWarned && opts.Warn != nil {
			opts.Warn("binary input replaced with [REDACTED_BINARY]")
		}
		binaryWarned = true
	}

	for !tokenFormChosen {
		chunk, err := ch.Next()
		if err == io.EOF {
			tokenASCII = isStrictASCIIPrefix(pending)
			tokenFormChosen = true
			break
		}
		if err != nil {
			return stats, err
		}
		pending = append(pending, chunk...)
		if len(pending) >= tokenProbe {
			tokenASCII = isStrictASCIIPrefix(pending[:tokenProbe])
			tokenFormChosen = true
			break
		}
	}

	for {
		chunk, err := ch.Next()
		if err == io.EOF {
			if len(pending) > 0 {
				if err := processSegment(ctx, pending, w, rules, alloc, tokenASCII, warnOnce, &stats); err != nil {
					return stats, err
				}
			}
			return stats, nil
		}
		if err != nil {
			return stats, err
		}
		combined := append(pending, chunk...)
		if len(combined) <= boundaryCarry {
			pending = combined
			continue
		}
		processLen := alignUTF8Boundary(combined, len(combined)-boundaryCarry)
		if err := processSegment(ctx, combined[:processLen], w, rules, alloc, tokenASCII, warnOnce, &stats); err != nil {
			return stats, err
		}
		// Allocate a fresh slice for the carry. `combined` may alias `pending`'s
		// backing array, so appending `combined[processLen:]` onto `pending[:0]`
		// would overlap within the same array for certain chunk sizes.
		pending = append([]byte(nil), combined[processLen:]...)
	}
}

func processSegment(ctx context.Context, chunk []byte, w io.Writer, rules []detect.Rule, alloc *pseudo.Allocator, ascii bool, warn func(), stats *Stats) error {
	chunk = detect.InertEscape(chunk)
	chunk = ioutil.ReplaceBinaryRuns(chunk, warn)
	masked, err := maskChunk(ctx, chunk, rules, alloc, ascii, stats)
	if err != nil {
		return err
	}
	if _, err := w.Write(masked); err != nil {
		// EPIPE means the downstream consumer closed (e.g. `| head`).
		// Treat it as a clean stop, not an error.
		if errors.Is(err, syscall.EPIPE) {
			return nil
		}
		return err
	}
	return nil
}

func maskChunk(ctx context.Context, b []byte, rules []detect.Rule, alloc *pseudo.Allocator, ascii bool, stats *Stats) ([]byte, error) {
	ms := detect.FindMatches(rules, b)
	if len(ms) == 0 {
		return b, nil
	}
	plans := make([]pseudo.Plan, len(ms))
	for i, m := range ms {
		if m.Rule.Policy == policy.Pseudonymize {
			plans[i] = alloc.Plan(m.Rule.Type, m.Value)
		}
	}
	if err := alloc.CommitPlans(ctx, plans); err != nil {
		return nil, err
	}
	var out bytes.Buffer
	pos := 0
	for i, m := range ms {
		out.Write(b[pos:m.Start])
		stats.ByType[m.Rule.Type]++
		switch m.Rule.Policy {
		case policy.Destroy:
			stats.Destroyed++
			out.WriteString("[REDACTED_" + strings.ToUpper(m.Rule.Type) + "]")
		case policy.Pseudonymize:
			stats.Masked++
			// Reuse the plan computed above; alloc.Plan recomputes HMAC-SHA256.
			out.WriteString(detect.RenderToken(m.Rule.Type, plans[i].Index, ascii))
		default:
			return nil, fmt.Errorf("invalid policy %q for %s", m.Rule.Policy, m.Rule.Name)
		}
		pos = m.End
	}
	out.Write(b[pos:])
	return out.Bytes(), nil
}

// alignUTF8Boundary walks back up to 3 bytes from cut so the returned split
// never falls inside a multibyte rune. Required because the engine carry is
// defined by byte distance (detection span), not rune boundaries; without
// alignment a straddled rune sends invalid-UTF-8 bytes into ReplaceBinaryRuns
// and gets misclassified as binary.
func alignUTF8Boundary(b []byte, cut int) int {
	if cut <= 0 || cut >= len(b) {
		return cut
	}
	for n := 0; n < 4 && cut-n > 0; n++ {
		pos := cut - n
		if utf8.RuneStart(b[pos]) && utf8.FullRune(b[pos:]) {
			return pos
		}
	}
	return cut
}

func isStrictASCIIPrefix(b []byte) bool {
	limit := len(b)
	if limit > tokenProbe {
		limit = tokenProbe
	}
	for _, c := range b[:limit] {
		if c >= 0x80 {
			return false
		}
	}
	return true
}
