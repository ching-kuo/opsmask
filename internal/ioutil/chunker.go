package ioutil

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"unicode/utf8"
)

const DefaultMaxLine = 16 << 20
const targetChunk = 64 << 10

type Chunker struct {
	r       *bufio.Reader
	maxLine int
	lineLen int
	carry   []byte
}

func NewChunker(r io.Reader, maxLine int) *Chunker {
	if maxLine <= 0 {
		maxLine = DefaultMaxLine
	}
	return &Chunker{r: bufio.NewReaderSize(r, DefaultMaxLine), maxLine: maxLine}
}

func (c *Chunker) Next() ([]byte, error) {
	var out []byte
	for len(out) < targetChunk {
		part, err := c.nextPart()
		if len(part) > 0 {
			out = append(out, part...)
		}
		if err == io.EOF {
			if len(out) > 0 {
				return out, nil
			}
			return nil, io.EOF
		}
		if err != nil {
			if len(out) > 0 {
				c.carry = append(part, c.carry...)
			}
			return nil, err
		}
		if len(part) == 0 {
			break
		}
	}
	return out, nil
}

func (c *Chunker) nextPart() ([]byte, error) {
	part, err := c.r.ReadSlice('\n')
	if len(part) == 0 && errors.Is(err, io.EOF) {
		if len(c.carry) > 0 {
			out := c.carry
			c.carry = nil
			return out, nil
		}
		return nil, io.EOF
	}
	c.lineLen += len(part)
	if c.lineLen > c.maxLine {
		return nil, fmt.Errorf("line exceeds --max-line (%d bytes)", c.maxLine)
	}
	if len(part) > 0 && part[len(part)-1] == '\n' {
		c.lineLen = 0
	}
	out := append(c.carry, part...)
	c.carry = nil
	if errors.Is(err, bufio.ErrBufferFull) {
		out, c.carry = splitUTF8Carry(out)
		return out, nil
	}
	if errors.Is(err, io.EOF) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	return out, nil
}

func splitUTF8Carry(b []byte) ([]byte, []byte) {
	for n := 1; n <= 3 && n < len(b); n++ {
		split := len(b) - n
		suffix := b[split:]
		if utf8.RuneStart(suffix[0]) && !utf8.FullRune(suffix) {
			return b[:split], append([]byte(nil), suffix...)
		}
	}
	return b, nil
}
