package corpus

import (
	"bytes"
	"fmt"
	"strings"
)

// diffContext is the number of unchanged lines emitted on each side of a
// hunk, matching the convention of `diff -u`.
const diffContext = 3

// UnifiedDiff returns a unified-diff representation of the difference
// between expected and got. The output is empty when the byte slices are
// equal. When they differ, the output contains `--- expected` / `+++ got`
// headers and one or more `@@` hunks with surrounding context. Pure
// function: no I/O, no global state.
func UnifiedDiff(expected, got []byte) string {
	if bytes.Equal(expected, got) {
		return ""
	}
	a := splitLines(expected)
	b := splitLines(got)
	ops := lcsDiff(a, b)
	hunks := buildHunks(ops)
	var sb strings.Builder
	sb.WriteString("--- expected\n")
	sb.WriteString("+++ got\n")
	for _, h := range hunks {
		sb.WriteString(h)
	}
	return sb.String()
}

// splitLines breaks input on '\n' via strings.SplitAfter, so each line keeps
// its trailing '\n'. The empty trailing chunk produced by a terminating
// newline is dropped; a final unterminated line is returned verbatim with no
// trailing '\n'. (buildHunks renders such a line indistinguishably from a
// terminated one by appending '\n', so there is no "\ No newline" marker.)
func splitLines(in []byte) []string {
	if len(in) == 0 {
		return nil
	}
	s := string(in)
	parts := strings.SplitAfter(s, "\n")
	// SplitAfter keeps trailing newline on each chunk; if the last char is
	// '\n', SplitAfter emits a trailing empty string we must drop.
	if len(parts) > 0 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	return parts
}

type op struct {
	kind byte // ' ', '-', '+'
	line string
}

// lcsDiff computes a line-level diff via classic LCS dynamic programming.
// Inputs are small (test goldens), so O(N*M) memory is acceptable.
func lcsDiff(a, b []string) []op {
	n, m := len(a), len(b)
	// dp[i][j] = LCS length of a[i:] and b[j:].
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}
	ops := make([]op, 0, n+m)
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			ops = append(ops, op{' ', a[i]})
			i++
			j++
		case dp[i+1][j] >= dp[i][j+1]:
			ops = append(ops, op{'-', a[i]})
			i++
		default:
			ops = append(ops, op{'+', b[j]})
			j++
		}
	}
	for ; i < n; i++ {
		ops = append(ops, op{'-', a[i]})
	}
	for ; j < m; j++ {
		ops = append(ops, op{'+', b[j]})
	}
	return ops
}

// buildHunks groups ops into hunks separated by long runs of unchanged
// lines, prepending the standard `@@ -start,len +start,len @@` header.
func buildHunks(ops []op) []string {
	if len(ops) == 0 {
		return nil
	}
	type pending struct {
		ops         []op
		oldStart    int // 1-based
		oldLen      int
		newStart    int
		newLen      int
		hasMutation bool
	}

	var hunks []string
	var cur *pending
	oldLine, newLine := 1, 1
	contextSinceLast := 0

	flush := func() {
		if cur == nil || !cur.hasMutation {
			cur = nil
			return
		}
		// Trim trailing pure-context lines beyond diffContext.
		trim := 0
		for k := len(cur.ops) - 1; k >= 0; k-- {
			if cur.ops[k].kind != ' ' {
				break
			}
			trim++
		}
		if trim > diffContext {
			drop := trim - diffContext
			cur.ops = cur.ops[:len(cur.ops)-drop]
			cur.oldLen -= drop
			cur.newLen -= drop
		}
		var sb strings.Builder
		fmt.Fprintf(&sb, "@@ -%d,%d +%d,%d @@\n", cur.oldStart, cur.oldLen, cur.newStart, cur.newLen)
		for _, o := range cur.ops {
			line := o.line
			sb.WriteByte(o.kind)
			sb.WriteString(line)
			if !strings.HasSuffix(line, "\n") {
				sb.WriteByte('\n')
			}
		}
		hunks = append(hunks, sb.String())
		cur = nil
	}

	startHunk := func(precedingContext []op, oldL, newL int) {
		// precedingContext are the last <=diffContext context ops.
		startOldLine := oldL - len(precedingContext)
		startNewLine := newL - len(precedingContext)
		cur = &pending{
			oldStart: startOldLine,
			newStart: startNewLine,
		}
		for _, o := range precedingContext {
			cur.ops = append(cur.ops, o)
			cur.oldLen++
			cur.newLen++
		}
	}

	contextBuf := make([]op, 0, diffContext)
	for _, o := range ops {
		switch o.kind {
		case ' ':
			if cur != nil {
				cur.ops = append(cur.ops, o)
				cur.oldLen++
				cur.newLen++
				contextSinceLast++
				if contextSinceLast >= 2*diffContext {
					flush()
					contextSinceLast = 0
					contextBuf = contextBuf[:0]
				}
			}
			// Maintain rolling context buffer of the last diffContext lines.
			if len(contextBuf) == diffContext {
				contextBuf = contextBuf[1:diffContext]
			}
			contextBuf = append(contextBuf, o)
			oldLine++
			newLine++
		case '-':
			if cur == nil {
				startHunk(append([]op(nil), contextBuf...), oldLine, newLine)
			}
			cur.ops = append(cur.ops, o)
			cur.oldLen++
			cur.hasMutation = true
			contextSinceLast = 0
			oldLine++
		case '+':
			if cur == nil {
				startHunk(append([]op(nil), contextBuf...), oldLine, newLine)
			}
			cur.ops = append(cur.ops, o)
			cur.newLen++
			cur.hasMutation = true
			contextSinceLast = 0
			newLine++
		}
	}
	flush()
	return hunks
}
