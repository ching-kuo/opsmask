package cli

import (
	"github.com/ching-kuo/opsmask/internal/mcpsrv"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
)

func newMcp(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Model Context Protocol server commands",
	}
	cmd.AddCommand(newMcpServe(opts))
	return cmd
}

func newMcpServe(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the MCP server on stdio",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			rt, err := buildRuntime(opts)
			if err != nil {
				return CodeError{Code: 1, Err: err}
			}
			defer rt.Close()
			audit, err := mcpsrv.OpenAuditWriter()
			if err != nil {
				return CodeError{Code: 1, Err: err}
			}
			defer audit.Close()
			srv := mcpsrv.NewServer(rt, audit)
			return srv.Run(cmd.Context(), &mcp.StdioTransport{})
		},
	}
	return cmd
}
