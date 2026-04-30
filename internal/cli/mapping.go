package cli

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"time"

	"github.com/ching-kuo/opsmask/internal/tty"
	"github.com/spf13/cobra"
)

func newMapping(opts *Options) *cobra.Command {
	cmd := &cobra.Command{Use: "mapping", Short: "Inspect or prune mapping rows"}
	cmd.AddCommand(newMappingList(opts), newMappingPrune(opts))
	return cmd
}

func newMappingList(opts *Options) *cobra.Command {
	var typ string
	var limit int
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List pseudonyms without plaintext values",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !tty.IsTerminal(int(os.Stdout.Fd())) {
				return userErr("mapping list refuses non-TTY stdout")
			}
			rt, err := buildRuntime(opts)
			if err != nil {
				return err
			}
			defer rt.Close()
			rows, err := rt.Store.List(context.Background(), typ, limit)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "PSEUDONYM\tTYPE\tREAL_HMAC8\tFIRST_SEEN")
			for _, m := range rows {
				preview := hex.EncodeToString(m.HMACFull)
				if len(preview) > 8 {
					preview = preview[:8]
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%s\n", m.Index, m.Type, preview, m.FirstSeenAt.Format(time.RFC3339))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&typ, "type", "", "filter by type")
	cmd.Flags().IntVar(&limit, "limit", 100, "maximum rows")
	return cmd
}

func newMappingPrune(opts *Options) *cobra.Command {
	var typ, older string
	cmd := &cobra.Command{
		Use:   "prune",
		Short: "Delete old mapping rows",
		RunE: func(cmd *cobra.Command, args []string) error {
			if older == "" {
				return userErr("mapping prune requires --older-than (use e.g. 720h) to avoid accidental wipe")
			}
			d, err := time.ParseDuration(older)
			if err != nil {
				return userErr("invalid --older-than %q", older)
			}
			if d <= 0 {
				return userErr("--older-than must be positive")
			}
			rt, err := buildRuntime(opts)
			if err != nil {
				return err
			}
			defer rt.Close()
			n, err := rt.Store.Prune(context.Background(), typ, d)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "deleted=%d\n", n)
			return nil
		},
	}
	cmd.Flags().StringVar(&typ, "type", "", "filter by type")
	cmd.Flags().StringVar(&older, "older-than", "", "duration such as 720h")
	return cmd
}
