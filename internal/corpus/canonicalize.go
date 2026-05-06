// Package corpus provides the detection-regression test corpus harness and
// CLI tooling. It composes the masking engine against scenario files under
// testdata/corpus/ and canonicalizes engine output so goldens stay stable
// across allocator-secret changes.
package corpus

import (
	"github.com/ching-kuo/opsmask/internal/detect"
)

// Canonicalize replaces the ID in every opsmask token with '*' while
// preserving the token class, delimiter form (Unicode ⟪…⟫ or ASCII [[…]]),
// count, and position. Destroyed-form markers ([REDACTED_<KIND>]) are
// already deterministic and pass through unchanged.
func Canonicalize(masked []byte) []byte {
	re := detect.TokenRegexp()
	return re.ReplaceAllFunc(masked, func(m []byte) []byte {
		// Determine form by first byte: '⟪' starts with 0xE2 in UTF-8.
		if len(m) > 0 && m[0] == '[' {
			// ASCII form: [[opsmask:<class>:<id>]]
			return rewriteASCII(m)
		}
		return rewriteUnicode(m)
	})
}

// rewriteASCII rewrites [[opsmask:<class>:<id>]] -> [[opsmask:<class>:*]].
// Caller guarantees m matches the ASCII-form regex branch.
func rewriteASCII(m []byte) []byte {
	// Find last ':' before trailing "]]".
	end := len(m) - 2 // before "]]"
	colon := -1
	for i := end - 1; i >= 0; i-- {
		if m[i] == ':' {
			colon = i
			break
		}
	}
	if colon < 0 {
		return m
	}
	out := make([]byte, 0, colon+4)
	out = append(out, m[:colon+1]...)
	out = append(out, '*')
	out = append(out, m[end:]...)
	return out
}

// rewriteUnicode rewrites ⟪opsmask:<class>:<id>⟫ -> ⟪opsmask:<class>:*⟫.
// '⟫' is U+27EB, encoded as the 3-byte sequence E2 9F AB.
func rewriteUnicode(m []byte) []byte {
	const closeLen = 3 // ⟫ is 3 bytes in UTF-8
	end := len(m) - closeLen
	colon := -1
	for i := end - 1; i >= 0; i-- {
		if m[i] == ':' {
			colon = i
			break
		}
	}
	if colon < 0 {
		return m
	}
	out := make([]byte, 0, colon+1+1+closeLen)
	out = append(out, m[:colon+1]...)
	out = append(out, '*')
	out = append(out, m[end:]...)
	return out
}
