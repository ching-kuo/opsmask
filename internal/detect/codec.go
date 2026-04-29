package detect

import (
	"encoding/base64"
	"regexp"
)

var (
	tokenRe = regexp.MustCompile(`(?:⟪llm-mask:([a-z0-9_]+):([0-9a-f]{16})⟫)|(?:\[\[llm-mask:([a-z0-9_]+):([0-9a-f]{16})\]\])`)
	inertRe = regexp.MustCompile(`\[LLM_MASK_ESCAPED_SENTINEL:([A-Za-z0-9_-]+)\]`)
)

type Token struct{ Type, Index string }

func RenderToken(typ, index string, ascii bool) string {
	if ascii {
		return "[[llm-mask:" + typ + ":" + index + "]]"
	}
	return "⟪llm-mask:" + typ + ":" + index + "⟫"
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
	return tokenRe.ReplaceAllFunc(in, func(m []byte) []byte {
		enc := base64.RawURLEncoding.EncodeToString(m)
		return []byte("[LLM_MASK_ESCAPED_SENTINEL:" + enc + "]")
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
