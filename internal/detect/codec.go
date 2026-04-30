package detect

import (
	"bytes"
	"encoding/base64"
	"regexp"
)

// tokenSentinel matches the byte signature shared by both token forms
// (⟪opsmask:…⟫ and [[opsmask:…]]). The fast-path bypass in InertEscape
// uses it to skip the regex when no token is present in the chunk.
var tokenSentinel = []byte("opsmask:")

var (
	tokenRe = regexp.MustCompile(`(?:⟪opsmask:([a-z0-9_]+):([0-9a-f]{16})⟫)|(?:\[\[opsmask:([a-z0-9_]+):([0-9a-f]{16})\]\])`)
	inertRe = regexp.MustCompile(`\[OPSMASK_ESCAPED_SENTINEL:([A-Za-z0-9_-]+)\]`)
)

type Token struct{ Type, Index string }

func RenderToken(typ, index string, ascii bool) string {
	if ascii {
		return "[[opsmask:" + typ + ":" + index + "]]"
	}
	return "⟪opsmask:" + typ + ":" + index + "⟫"
}

func ParseToken(s []byte) (Token, bool) {
	m := tokenRe.FindSubmatch(s)
	if m == nil || len(m[0]) != len(s) {
		return Token{}, false
	}
	if len(m[1]) > 0 {
		return Token{Type: string(m[1]), Index: string(m[2])}, true
	}
	return Token{Type: string(m[3]), Index: string(m[4])}, true
}

func TokenRegexp() *regexp.Regexp { return tokenRe }

func InertEscape(in []byte) []byte {
	if !bytes.Contains(in, tokenSentinel) {
		return in
	}
	return tokenRe.ReplaceAllFunc(in, func(m []byte) []byte {
		enc := base64.RawURLEncoding.EncodeToString(m)
		return []byte("[OPSMASK_ESCAPED_SENTINEL:" + enc + "]")
	})
}

func InertDecode(in []byte) ([]byte, int) {
	count := 0
	out := inertRe.ReplaceAllFunc(in, func(m []byte) []byte {
		sub := inertRe.FindSubmatch(m)
		if len(sub) != 2 {
			return m
		}
		decoded, err := base64.RawURLEncoding.DecodeString(string(sub[1]))
		if err != nil {
			return m
		}
		count++
		return decoded
	})
	return out, count
}
