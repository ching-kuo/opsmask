package detect

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"math"
	"regexp"
	"sort"
	"strings"

	"github.com/ching-kuo/opsmask/internal/detect/rules"
	"github.com/ching-kuo/opsmask/internal/policy"
)

type Rule struct {
	Name, Type   string
	Policy       policy.Policy
	Regex        *regexp.Regexp
	Keywords     [][]byte
	MaxMatchSpan int
	SubMatch     int // if >0, use this capture group index as the match bounds
	MinEntropy   float64
	Check        func([]byte) bool
}

type Match struct {
	Start, End int
	Rule       Rule
	Value      []byte
}

func BuiltinRules() ([]Rule, error) {
	specs := rules.Builtins()
	out := make([]Rule, 0, len(specs))
	for _, s := range specs {
		re, err := regexp.Compile(s.Pattern)
		if err != nil {
			return nil, err
		}
		var keywords [][]byte
		if len(s.Keywords) > 0 {
			keywords = make([][]byte, len(s.Keywords))
			for i, k := range s.Keywords {
				keywords[i] = []byte(k)
			}
		}
		r := Rule{Name: s.Name, Type: s.Type, Policy: s.Policy, Regex: re, Keywords: keywords, MaxMatchSpan: s.MaxMatchSpan, SubMatch: s.SubMatch, MinEntropy: s.MinEntropy}
		if s.Type == "jwt" {
			r.Check = validJWT
		}
		out = append(out, r)
	}
	return out, nil
}

func FindMatches(rules []Rule, b []byte) []Match {
	var ms []Match
	for _, r := range rules {
		for _, loc := range ruleFindAll(r, b) {
			if loc[1] <= loc[0] || loc[1]-loc[0] > maxSpan(r.MaxMatchSpan) {
				continue
			}
			value := b[loc[0]:loc[1]]
			if r.MinEntropy > 0 && shannonEntropy(value) < r.MinEntropy {
				continue
			}
			if r.Check != nil && !r.Check(value) {
				continue
			}
			ms = append(ms, Match{Start: loc[0], End: loc[1], Rule: r, Value: append([]byte(nil), value...)})
		}
	}
	return nonOverlapping(ms)
}

func ruleFindAll(r Rule, b []byte) [][2]int {
	if r.SubMatch > 0 {
		// grp: flat index into FindAllSubmatchIndex result ([full_start,full_end,g1_start,g1_end,…])
		grp := r.SubMatch * 2
		return scanWindows(r, b, func(slice []byte, offset int) [][2]int {
			all := r.Regex.FindAllSubmatchIndex(slice, -1)
			out := make([][2]int, 0, len(all))
			for _, m := range all {
				if grp+1 >= len(m) || m[grp] < 0 {
					continue
				}
				out = append(out, [2]int{m[grp] + offset, m[grp+1] + offset})
			}
			return out
		})
	}
	return scanWindows(r, b, func(slice []byte, offset int) [][2]int {
		return regexLocations(r.Regex.FindAllIndex(slice, -1), offset)
	})
}

func scanWindows(r Rule, b []byte, scan func([]byte, int) [][2]int) [][2]int {
	if len(r.Keywords) == 0 {
		return scan(b, 0)
	}
	var out [][2]int
	for _, rg := range keywordRanges(b, r.Keywords, maxSpan(r.MaxMatchSpan)) {
		out = append(out, scan(b[rg[0]:rg[1]], rg[0])...)
	}
	return out
}

func regexLocations(locs [][]int, offset int) [][2]int {
	out := make([][2]int, 0, len(locs))
	for _, loc := range locs {
		out = append(out, [2]int{loc[0] + offset, loc[1] + offset})
	}
	return out
}

func keywordRanges(b []byte, keys [][]byte, span int) [][2]int {
	seen := map[[2]int]bool{}
	var ranges [][2]int
	window := span
	if window < 256 {
		window = 256
	}
	for _, k := range keys {
		start := 0
		for {
			i := bytes.Index(b[start:], k)
			if i < 0 {
				break
			}
			pos := start + i
			a, z := pos-window, pos+len(k)+window
			if a < 0 {
				a = 0
			}
			if z > len(b) {
				z = len(b)
			}
			rg := [2]int{a, z}
			if !seen[rg] {
				seen[rg] = true
				ranges = append(ranges, rg)
			}
			start = pos + len(k)
		}
	}
	return ranges
}

func maxSpan(n int) int {
	if n <= 0 {
		return 4096
	}
	return n
}

func nonOverlapping(ms []Match) []Match {
	sort.SliceStable(ms, func(i, j int) bool {
		if ms[i].Start != ms[j].Start {
			return ms[i].Start < ms[j].Start
		}
		return ms[i].End-ms[i].Start > ms[j].End-ms[j].Start
	})
	out := make([]Match, 0, len(ms))
	last := -1
	for _, m := range ms {
		if m.Start < last {
			continue
		}
		out = append(out, m)
		last = m.End
	}
	return out
}

// validJWT checks that the candidate has a JWT-like JSON header plus a JSON
// payload containing at least one common registered/public claim. The header
// carries alg/typ in common JWTs; requiring those fields in the payload caused
// bearer tokens with ordinary sub/iat payloads to be missed.
func validJWT(b []byte) bool {
	parts := strings.Split(string(b), ".")
	if len(parts) != 3 {
		return false
	}
	headerRaw, err := decodeJWTPart(parts[0])
	if err != nil {
		return false
	}
	var header map[string]any
	if json.Unmarshal(headerRaw, &header) != nil {
		return false
	}
	if !hasStringClaim(header, "alg", "typ") {
		return false
	}
	payloadRaw, err := decodeJWTPart(parts[1])
	if err != nil {
		return false
	}
	var payload map[string]any
	if json.Unmarshal(payloadRaw, &payload) != nil {
		return false
	}
	for _, k := range []string{"sub", "iss", "aud", "exp", "nbf", "iat", "jti"} {
		if _, ok := payload[k]; ok {
			return true
		}
	}
	return false
}

func decodeJWTPart(part string) ([]byte, error) {
	if raw, err := base64.RawURLEncoding.DecodeString(part); err == nil {
		return raw, nil
	}
	return base64.URLEncoding.DecodeString(part)
}

func hasStringClaim(obj map[string]any, keys ...string) bool {
	for _, k := range keys {
		if v, ok := obj[k].(string); ok && v != "" {
			return true
		}
	}
	return false
}

func shannonEntropy(b []byte) float64 {
	if len(b) == 0 {
		return 0
	}
	var counts [256]int
	for _, c := range b {
		counts[c]++
	}
	var entropy float64
	length := float64(len(b))
	for _, n := range counts {
		if n == 0 {
			continue
		}
		p := float64(n) / length
		entropy -= p * math.Log2(p)
	}
	return entropy
}
