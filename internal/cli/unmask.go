package cli

import (
	"bytes"
	"fmt"
	"io"
	"os"

	"github.com/ching-kuo/opsmask/internal/detect"
	"github.com/ching-kuo/opsmask/internal/tty"
	"github.com/spf13/cobra"
)

func newUnmask(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "unmask [file|-]",
		Short: "Restore opsmask tokens at a human terminal",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !tty.IsTerminal(int(os.Stdout.Fd())) {
				return userErr("unmask refuses to write to non-TTY stdout")
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
			b, err := io.ReadAll(in)
			if err != nil {
				return err
			}
			// KTD-4 non-recursive single-pass contract: substitute live
			// tokens first, then decode inert-escaped bytes. This ordering is
			// load-bearing -- decoding inert forms first and then scanning
			// for tokens would re-animate literal sentinel bytes planted by
			// an attacker in the source text, turning an audit canary into
			// a lookup oracle. InertDecode never re-scans its output.
			var restored, unknown int
			out := detect.TokenRegexp().ReplaceAllFunc(b, func(m []byte) []byte {
				tok, ok := detect.ParseToken(m)
				if !ok {
					return m
				}
				plaintext, found, err := rt.store.Lookup(cmd.Context(), tok.Type, tok.Index)
				if err != nil || !found {
					unknown++
					return m
				}
				restored++
				return plaintext
			})
			out, escaped := detect.InertDecode(out)
			if _, err := io.Copy(cmd.OutOrStdout(), bytes.NewReader(out)); err != nil {
				return err
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "restored=%d unknown=%d escaped=%d\n", restored, unknown, escaped)
			return nil
		},
	}
	return cmd
}
