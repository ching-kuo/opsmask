package ioutil

import (
	"bytes"
	"unicode/utf8"
)

var redactedBinary = []byte("[REDACTED_BINARY]")

// ReplaceBinaryRuns collapses runs of binary bytes (NUL, control chars
// except tab/LF/CR, or invalid UTF-8) into [REDACTED_BINARY]. warn is
// invoked once for each binary run encountered; callers that want
// at-most-once semantics across a larger scope pass a latching closure.
func ReplaceBinaryRuns(in []byte, warn func()) []byte {
	if !hasBinary(in) {
		return in
	}
	var out bytes.Buffer
	for i := 0; i < len(in); {
		r, size := utf8.DecodeRune(in[i:])
		binary := false
		if r == utf8.RuneError && size == 1 {
			binary = true
		} else if r == 0 || (r < 0x20 && r != '\t' && r != '\n' && r != '\r') {
			binary = true
		}
		if !binary {
			out.Write(in[i : i+size])
			i += size
			continue
		}
		if warn != nil {
			warn()
		}
		out.Write(redactedBinary)
		for i < len(in) {
			r, size = utf8.DecodeRune(in[i:])
			if !(r == utf8.RuneError && size == 1) &&
				r != 0 && !(r < 0x20 && r != '\t' && r != '\n' && r != '\r') {
				break
			}
			i += size
		}
	}
	return out.Bytes()
}

func hasBinary(in []byte) bool {
	for i := 0; i < len(in); {
		r, size := utf8.DecodeRune(in[i:])
		if r == utf8.RuneError && size == 1 {
			return true
		}
		if r == 0 || (r < 0x20 && r != '\t' && r != '\n' && r != '\r') {
			return true
		}
		i += size
	}
	return false
}
