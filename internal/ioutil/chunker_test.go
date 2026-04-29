package ioutil

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestChunkerRejectsOverMaxLine(t *testing.T) {
	ch := NewChunker(strings.NewReader("abcdef\n"), 3)
	_, err := ch.Next()
	if err == nil || !strings.Contains(err.Error(), "--max-line") {
		t.Fatalf("expected max-line error, got %v", err)
	}
}

func TestChunkerStreamsLongLineWhenAllowed(t *testing.T) {
	input := strings.Repeat("a", DefaultMaxLine+10) + "\n"
	ch := NewChunker(strings.NewReader(input), len(input)+1)
	var out bytes.Buffer
	for {
		b, err := ch.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if len(b) > DefaultMaxLine+3 {
			t.Fatalf("chunk too large: %d", len(b))
		}
		out.Write(b)
	}
	if out.String() != input {
		t.Fatalf("output mismatch")
	}
}

func TestChunkerReassemblesSplitRune(t *testing.T) {
	input := strings.Repeat("a", DefaultMaxLine-1) + "☃\n"
	ch := NewChunker(strings.NewReader(input), len(input)+1)
	var out bytes.Buffer
	for {
		b, err := ch.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		out.Write(b)
	}
	if out.String() != input {
		t.Fatalf("split rune was not reassembled")
	}
}

func TestReplaceBinaryRuns(t *testing.T) {
	warns := 0
	got := ReplaceBinaryRuns([]byte("a\x00\x01b\t\n"), func() { warns++ })
	if string(got) != "a[REDACTED_BINARY]b\t\n" {
		t.Fatalf("got %q", got)
	}
	if warns != 1 {
		t.Fatalf("warns=%d", warns)
	}
}
