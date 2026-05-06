package corpus

// Compare returns the empty string when expected and got are byte-equal,
// otherwise a unified diff suitable for inclusion in a t.Fatalf message.
// Pure: no I/O, no global state.
func Compare(expected, got []byte) string {
	return UnifiedDiff(expected, got)
}
