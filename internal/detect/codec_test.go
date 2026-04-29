package detect

import (
	"bytes"
	"testing"
)

func TestTokenRoundTripAndInert(t *testing.T) {
	for _, ascii := range []bool{false, true} {
		tok := RenderToken("ip4", "a3f2b1c4d5e6f708", ascii)
		parsed, ok := ParseToken([]byte(tok))
		if !ok || parsed.Type != "ip4" || parsed.Index != "a3f2b1c4d5e6f708" {
			t.Fatalf("parse failed: %#v %v", parsed, ok)
		}
		escaped := InertEscape([]byte("literal " + tok))
		if bytes.Contains(escaped, []byte(tok)) {
			t.Fatalf("token was not escaped: %s", escaped)
		}
		decoded, n := InertDecode(escaped)
		if n != 1 || !bytes.Contains(decoded, []byte(tok)) {
			t.Fatalf("decode=%q n=%d", decoded, n)
		}
	}
}
