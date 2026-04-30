package cli

import (
	"bytes"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	"github.com/ching-kuo/opsmask/internal/config"
	"github.com/ching-kuo/opsmask/internal/engine"
	"github.com/spf13/cobra"
)

func newMask(opts *Options) *cobra.Command {
	var summary, ascii bool
	var maxLine string
	cmd := &cobra.Command{
		Use:   "mask [file|-]",
		Short: "Mask sensitive log text",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			limit, err := parseSize(maxLine)
			if err != nil {
				return err
			}
			rt, err := buildRuntime(opts)
			if err != nil {
				return err
			}
			defer rt.Close()
			in, closeIn, err := openInput(args)
			if err != nil {
				return err
			}
			defer closeIn()
			deny := newDenyWriter(cmd.OutOrStdout(), rt.Loaded.DenyList)
			stats, err := engine.Process(cmd.Context(), in, deny, rt.Rules, rt.Alloc, engine.Options{
				ASCIITokens: ascii, MaxLine: limit,
				Warn: func(s string) { fmt.Fprintln(cmd.ErrOrStderr(), s) },
			})
			if err != nil {
				return err
			}
			if hit := deny.Hit(); hit != "" {
				return fmt.Errorf("deny-list canary hit: %s (matched text redacted)", hit)
			}
			if summary {
				fmt.Fprintf(cmd.ErrOrStderr(), "masked=%d destroyed=%d", stats.Masked, stats.Destroyed)
				for k, v := range stats.ByType {
					fmt.Fprintf(cmd.ErrOrStderr(), " %s=%d", k, v)
				}
				fmt.Fprintln(cmd.ErrOrStderr())
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&summary, "summary", false, "write per-class counts to stderr")
	cmd.Flags().BoolVar(&ascii, "ascii-tokens", false, "force ASCII sentinel tokens")
	cmd.Flags().StringVar(&maxLine, "max-line", "16MiB", "maximum line size")
	return cmd
}

func parseSize(s string) (int, error) {
	if s == "" {
		return 16 << 20, nil
	}
	lower := strings.ToLower(strings.TrimSpace(s))
	// Ordered longest-first to avoid "kb" matching before "kib".
	suffixes := []struct {
		name string
		mul  int
	}{
		{"mib", 1 << 20},
		{"kib", 1 << 10},
		{"mb", 1000 * 1000},
		{"kb", 1000},
	}
	mul := 1
	for _, s := range suffixes {
		if strings.HasSuffix(lower, s.name) {
			mul = s.mul
			lower = strings.TrimSuffix(lower, s.name)
			break
		}
	}
	n, err := strconv.Atoi(lower)
	if err != nil || n <= 0 {
		return 0, userErr("invalid --max-line %q", s)
	}
	return n * mul, nil
}

// denyEntry is the runtime form of config.DenyEntry: the regex is compiled
// once at construction so denyHit avoids a Compile call per Write. A regex
// that fails to compile is treated as a no-op (matching the previous silent-
// skip behavior) — config validation is the right place to surface bad
// patterns to the user.
type denyEntry struct {
	name    string
	literal []byte
	pattern *regexp.Regexp
}

func denyHit(b []byte, deny []denyEntry) string {
	for _, d := range deny {
		if d.literal != nil && bytes.Contains(b, d.literal) {
			return d.name
		}
		if d.pattern != nil && d.pattern.Match(b) {
			return d.name
		}
	}
	return ""
}

type denyWriter struct {
	w       io.Writer
	deny    []denyEntry
	hit     string
	tail    []byte
	scratch []byte
	limit   int
}

func newDenyWriter(w io.Writer, src []config.DenyEntry) *denyWriter {
	deny := make([]denyEntry, 0, len(src))
	for _, e := range src {
		entry := denyEntry{name: e.Name}
		if e.Literal != "" {
			entry.literal = []byte(e.Literal)
		}
		if e.Pattern != "" {
			if re, err := regexp.Compile(e.Pattern); err == nil {
				entry.pattern = re
			}
		}
		deny = append(deny, entry)
	}
	return &denyWriter{w: w, deny: deny, limit: 8192}
}

func (d *denyWriter) Write(p []byte) (int, error) {
	if d.hit == "" {
		d.scratch = append(d.scratch[:0], d.tail...)
		d.scratch = append(d.scratch, p...)
		d.hit = denyHit(d.scratch, d.deny)
		if len(d.scratch) > d.limit {
			d.tail = append(d.tail[:0], d.scratch[len(d.scratch)-d.limit:]...)
		} else {
			d.tail = append(d.tail[:0], d.scratch...)
		}
	}
	return d.w.Write(p)
}

func (d *denyWriter) Hit() string { return d.hit }
