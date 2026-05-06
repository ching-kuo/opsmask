package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ching-kuo/opsmask/internal/corpus"
	"github.com/ching-kuo/opsmask/internal/tty"
	"github.com/spf13/cobra"
)

func newCorpus() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "corpus",
		Short: "Manage the detection-regression test corpus",
		Long: `Manage scenarios under testdata/corpus/. Subcommands write
only inside that directory and never touch the project's persistent
mapping store.`,
	}
	cmd.AddCommand(newCorpusAdd(), newCorpusAccept(), newCorpusList())
	return cmd
}

// ---------- add ----------

func newCorpusAdd() *cobra.Command {
	var scenario, note string
	var assumeYes bool
	cmd := &cobra.Command{
		Use:   "add <input-file>",
		Short: "Add a new corpus scenario from an input file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if scenario == "" {
				return userErr("--scenario is required")
			}
			if err := corpus.ValidateScenarioName(scenario); err != nil {
				return userErr("%s", err.Error())
			}
			root, err := corpus.CorpusRoot()
			if err != nil {
				return err
			}
			scenarioDir, err := corpus.ScenarioPath(root, scenario)
			if err != nil {
				return userErr("%s", err.Error())
			}
			if _, err := os.Stat(scenarioDir); err == nil {
				return fmt.Errorf("scenario %q already exists; use `opsmask corpus accept %s` to update its golden", scenario, scenario)
			}
			input, err := os.ReadFile(args[0])
			if err != nil {
				return fmt.Errorf("read input: %w", err)
			}
			masked, err := corpus.RunMask(cmd.Context(), input)
			if err != nil {
				return err
			}
			canon := corpus.Canonicalize(masked)

			expected, err := promptAcceptOrEdit(cmd.OutOrStdout(), cmd.ErrOrStderr(), cmd.InOrStdin(), canon, assumeYes)
			if err != nil {
				return err
			}
			if expected == nil {
				fmt.Fprintln(cmd.ErrOrStderr(), "not written")
				return nil
			}
			if err := os.MkdirAll(scenarioDir, 0o755); err != nil {
				return fmt.Errorf("mkdir scenario: %w", err)
			}
			// Roll back the entire scenario directory on any write
			// failure so a partially-populated scenario does not break
			// `go test ./...` via Discover's missing-file hard-fail.
			rollback := func() { _ = os.RemoveAll(scenarioDir) }
			if err := atomicWrite(scenarioDir, "input.txt", input); err != nil {
				rollback()
				return err
			}
			if err := atomicWrite(scenarioDir, "expected.txt", expected); err != nil {
				rollback()
				return err
			}
			if note != "" {
				readme := []byte("# " + scenario + "\n\n" + note + "\n")
				if err := atomicWrite(scenarioDir, "README.md", readme); err != nil {
					rollback()
					return err
				}
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "wrote: %s\n", scenarioDir)
			return nil
		},
	}
	cmd.Flags().StringVar(&scenario, "scenario", "", "scenario name (kebab-case, length >= 3)")
	cmd.Flags().StringVar(&note, "note", "", "optional note written to README.md")
	cmd.Flags().BoolVar(&assumeYes, "yes", false, "skip the prompt; accept the proposed expected.txt")
	return cmd
}

// promptAcceptOrEdit prints the proposed canonicalized expected.txt to err
// and prompts y/n/e on in. Returns:
//   - non-nil bytes on accept (after possible $EDITOR pass)
//   - nil bytes, nil error on decline
//   - non-nil error on failure (including non-TTY input without --yes).
func promptAcceptOrEdit(out, errOut io.Writer, in io.Reader, proposed []byte, assumeYes bool) ([]byte, error) {
	if assumeYes {
		return proposed, nil
	}
	stdinFile, _ := in.(*os.File)
	if stdinFile == nil || !tty.IsTerminal(int(stdinFile.Fd())) {
		return nil, fmt.Errorf("non-TTY stdin: pass --yes to accept the proposed expected.txt non-interactively")
	}
	current := proposed
	reader := bufio.NewReader(in)
	for {
		fmt.Fprintln(errOut, "----- proposed expected.txt -----")
		errOut.Write(current)
		if len(current) == 0 || current[len(current)-1] != '\n' {
			fmt.Fprintln(errOut)
		}
		fmt.Fprint(errOut, "----- accept? [y]es / [n]o / [e]dit: ")
		line, err := reader.ReadString('\n')
		if err != nil && err != io.EOF {
			return nil, fmt.Errorf("read prompt: %w", err)
		}
		switch strings.TrimSpace(strings.ToLower(line)) {
		case "y", "yes":
			return current, nil
		case "n", "no", "":
			return nil, nil
		case "e", "edit":
			edited, err := openInEditor(current)
			if err != nil {
				fmt.Fprintf(errOut, "editor failed: %v\n", err)
				continue
			}
			current = edited
		default:
			fmt.Fprintln(errOut, `please answer "y", "n", or "e"`)
		}
	}
}

func openInEditor(content []byte) ([]byte, error) {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}
	// Split on whitespace so values like `code --wait` or `emacs -nw`
	// resolve to argv[0]=binary, argv[1..]=flags. Quoting/escaping is
	// not supported - common $EDITOR values do not need it.
	fields := strings.Fields(editor)
	if len(fields) == 0 {
		return nil, fmt.Errorf("EDITOR is empty after splitting")
	}
	tmp, err := os.CreateTemp("", "opsmask-corpus-edit-*.txt")
	if err != nil {
		return nil, err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		return nil, err
	}
	cmd := exec.Command(fields[0], append(fields[1:], tmpPath)...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("editor %q: %w", editor, err)
	}
	return os.ReadFile(tmpPath)
}

// ---------- accept ----------

func newCorpusAccept() *cobra.Command {
	var all, force bool
	cmd := &cobra.Command{
		Use:   "accept [scenario]",
		Short: "Regenerate expected.txt for one scenario or all scenarios",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if all && len(args) > 0 {
				return userErr("cannot pass --all and a scenario name together")
			}
			if !all && len(args) == 0 {
				return userErr("specify a scenario name or pass --all")
			}
			root, err := corpus.CorpusRoot()
			if err != nil {
				return err
			}
			targets, err := acceptTargets(root, all, args)
			if err != nil {
				return err
			}
			if len(targets) == 0 {
				fmt.Fprintln(cmd.ErrOrStderr(), "no scenarios to accept")
				return nil
			}
			if !force {
				dirty, err := dirtyTargets(targets)
				if err != nil {
					return err
				}
				if len(dirty) > 0 {
					return fmt.Errorf("uncommitted changes in %s; commit or pass --force", strings.Join(dirty, ", "))
				}
			}
			for _, sc := range targets {
				input, err := os.ReadFile(sc.InputPath)
				if err != nil {
					return fmt.Errorf("read %s/input.txt: %w", sc.Name, err)
				}
				masked, err := corpus.RunMask(cmd.Context(), input)
				if err != nil {
					return fmt.Errorf("scenario %s: %w", sc.Name, err)
				}
				canon := corpus.Canonicalize(masked)
				if err := atomicWrite(sc.Dir, "expected.txt", canon); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "wrote: %s\n", sc.ExpectedPath)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "regenerate every scenario's expected.txt")
	cmd.Flags().BoolVar(&force, "force", false, "skip the uncommitted-changes guard")
	return cmd
}

func acceptTargets(root string, all bool, args []string) ([]corpus.Scenario, error) {
	if all {
		return corpus.Discover(root)
	}
	name := args[0]
	if err := corpus.ValidateScenarioName(name); err != nil {
		return nil, userErr("%s", err.Error())
	}
	dir, err := corpus.ScenarioPath(root, name)
	if err != nil {
		return nil, userErr("%s", err.Error())
	}
	input := filepath.Join(dir, "input.txt")
	expected := filepath.Join(dir, "expected.txt")
	if _, err := os.Stat(input); err != nil {
		return nil, fmt.Errorf("scenario %q missing input.txt: %w", name, err)
	}
	return []corpus.Scenario{{Name: name, Dir: dir, InputPath: input, ExpectedPath: expected}}, nil
}

// dirtyTargets shells `git status --porcelain` for each scenario directory
// and returns the names whose tree is dirty. A `git` failure (e.g., not on
// PATH) is reported as a clear error so the caller can decide whether to
// pass --force.
func dirtyTargets(targets []corpus.Scenario) ([]string, error) {
	var dirty []string
	for _, sc := range targets {
		// Run from inside the scenario dir so git resolves the
		// containing repo regardless of the caller's cwd.
		gs := exec.Command("git", "status", "--porcelain", sc.Dir)
		gs.Dir = sc.Dir
		out, err := gs.Output()
		if err != nil {
			return nil, fmt.Errorf("git status %s: %w (pass --force to skip the dirty-tree check)", sc.Name, err)
		}
		if len(strings.TrimSpace(string(out))) > 0 {
			dirty = append(dirty, sc.Name)
		}
	}
	sort.Strings(dirty)
	return dirty, nil
}

// ---------- list ----------

func newCorpusList() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List corpus scenarios with size and last-accept date",
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := corpus.CorpusRoot()
			if err != nil {
				return err
			}
			scenarios, err := corpus.Discover(root)
			if err != nil {
				return err
			}
			if len(scenarios) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "no scenarios")
				return nil
			}
			for _, sc := range scenarios {
				size := int64(0)
				if fi, err := os.Stat(sc.InputPath); err == nil {
					size = fi.Size()
				}
				date := lastAcceptDate(sc.ExpectedPath)
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%d\t%s\n", sc.Name, size, date)
			}
			return nil
		},
	}
}

func lastAcceptDate(path string) string {
	cmd := exec.Command("git", "log", "-1", "--format=%cs", "--", path)
	cmd.Dir = filepath.Dir(path)
	out, err := cmd.Output()
	if err != nil {
		return "(no git history)"
	}
	s := strings.TrimSpace(string(out))
	if s == "" {
		return "(no git history)"
	}
	return s
}

// ---------- helpers ----------

// atomicWrite writes data to <dir>/<name> via temp file + rename. An
// interrupted run leaves at most a `.tmp-*` file in dir, never a partial
// destination file.
func atomicWrite(dir, name string, data []byte) error {
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Rename(tmpPath, filepath.Join(dir, name)); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

