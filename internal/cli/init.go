package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

const projectConfigBody = "# opsmask project config (run `opsmask config trust` after edits)\n" +
	"# Example literal rule:\n" +
	"#   - name: customer\n" +
	"#     type: customer\n" +
	"#     value: Example Corp\n" +
	"#     policy: pseudonymize\n" +
	"literals: []\n" +
	"regex_rules: []\n" +
	"deny_list: []\n" +
	"\n" +
	"# Follow-up command execution is disabled by default. To enable it,\n" +
	"# set enabled: true, then run `opsmask config trust` again.\n" +
	"exec:\n" +
	"  enabled: false\n" +
	"  scope: read-only\n" +
	"  default_timeout: 30s\n" +
	"  allow: []\n" +
	"  env_allow: []\n" +
	"  env_deny: []\n"

func initProjectFiles() ([]string, error) {
	dir := ".opsmask"
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	_ = os.Chmod(dir, 0o700)
	created := []string{}
	gitignore := filepath.Join(dir, ".gitignore")
	if _, err := os.Stat(gitignore); err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		if err := os.WriteFile(gitignore, []byte("*\n"), 0o600); err != nil {
			return nil, err
		}
		created = append(created, gitignore)
	}
	cfg := filepath.Join(dir, "config.yaml")
	if _, err := os.Stat(cfg); err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		if err := os.WriteFile(cfg, []byte(projectConfigBody), 0o600); err != nil {
			return nil, err
		}
		created = append(created, cfg)
	} else if repaired, err := repairCommentOnlyProjectConfig(cfg); err != nil {
		return nil, err
	} else if repaired {
		created = append(created, cfg)
	}
	return created, nil
}

func repairCommentOnlyProjectConfig(path string) (bool, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	if hasActiveProjectConfig(body) {
		return false, nil
	}
	if err := os.WriteFile(path, []byte(projectConfigBody), 0o600); err != nil {
		return false, err
	}
	return true, nil
}

func hasActiveProjectConfig(body []byte) bool {
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		return true
	}
	return false
}

func printInitSummary(cmd *cobra.Command, created []string) {
	if len(created) == 0 {
		fmt.Fprintln(cmd.ErrOrStderr(), ".opsmask already initialized")
	} else {
		fmt.Fprintf(cmd.ErrOrStderr(), "created %s\n", created)
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "active project mapping: %s\n", filepath.Join(".opsmask", "mapping.sqlite"))
}

func newInit() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Initialize .opsmask in the current directory",
		RunE: func(cmd *cobra.Command, args []string) error {
			created, err := initProjectFiles()
			if err != nil {
				return err
			}
			printInitSummary(cmd, created)
			return nil
		},
	}
}
