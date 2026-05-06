package cli

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ching-kuo/opsmask/internal/corpus"
)

// fakeRepo creates a minimal Go module rooted at t.TempDir with a
// testdata/corpus directory and (optionally) a fresh git repo so
// `git status --porcelain` produces deterministic output.
func fakeRepo(t *testing.T, gitInit bool) (root, corpusDir string) {
	t.Helper()
	root = t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module fake\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	corpusDir = filepath.Join(root, "testdata", "corpus")
	if err := os.MkdirAll(corpusDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if gitInit {
		runGit(t, root, "init", "-q")
		runGit(t, root, "config", "user.email", "test@example.com")
		runGit(t, root, "config", "user.name", "test")
		runGit(t, root, "add", ".")
		runGit(t, root, "commit", "-q", "-m", "initial")
	}
	return root, corpusDir
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func chdir(t *testing.T, dir string) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })
}

func TestRewriteArgsCorpusUnchanged(t *testing.T) {
	cases := [][]string{
		{"corpus", "list"},
		{"corpus", "add", "./fixture.txt", "--scenario", "x"},
		{"corpus", "accept", "--all"},
	}
	for _, in := range cases {
		got := RewriteArgs(append([]string(nil), in...))
		if !equalSlices(got, in) {
			t.Errorf("RewriteArgs(%v) = %v, want unchanged", in, got)
		}
	}
}

func TestRewriteArgsUnknownStillPrefixed(t *testing.T) {
	got := RewriteArgs([]string{"unknown-command"})
	want := []string{"mask", "unknown-command"}
	if !equalSlices(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func equalSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestCorpusRootFromCLIPackage(t *testing.T) {
	// The CLI test cwd is internal/cli; CorpusRoot must resolve to
	// the same testdata/corpus as when called from internal/corpus.
	got, err := corpus.CorpusRoot()
	if err != nil {
		t.Fatalf("CorpusRoot: %v", err)
	}
	if !strings.HasSuffix(filepath.ToSlash(got), "/testdata/corpus") {
		t.Fatalf("unexpected: %q", got)
	}
}

// runCmd invokes the corpus root command with the given args and stdin.
func runCmd(t *testing.T, stdin io.Reader, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	root := newCorpus()
	root.SilenceUsage = true
	root.SilenceErrors = true
	var outBuf, errBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	if stdin != nil {
		root.SetIn(stdin)
	}
	root.SetArgs(args)
	root.SetContext(context.Background())
	err = root.Execute()
	return outBuf.String(), errBuf.String(), err
}

func TestCorpusListEmpty(t *testing.T) {
	root, _ := fakeRepo(t, false)
	chdir(t, root)
	out, _, err := runCmd(t, nil, "list")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(out, "no scenarios") {
		t.Fatalf("expected 'no scenarios', got %q", out)
	}
}

func TestCorpusAddRejectsBadScenarioName(t *testing.T) {
	root, _ := fakeRepo(t, false)
	chdir(t, root)
	fixture := filepath.Join(root, "fixture.txt")
	if err := os.WriteFile(fixture, []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bad := []string{"../etc/passwd", "FOO", "foo bar"}
	for _, name := range bad {
		_, _, err := runCmd(t, nil, "add", fixture, "--scenario", name, "--yes")
		if err == nil {
			t.Errorf("expected error for %q, got nil", name)
		}
	}
}

func TestCorpusAddMissingScenarioFlag(t *testing.T) {
	root, _ := fakeRepo(t, false)
	chdir(t, root)
	fixture := filepath.Join(root, "fixture.txt")
	if err := os.WriteFile(fixture, []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err := runCmd(t, nil, "add", fixture, "--yes")
	if err == nil {
		t.Fatal("expected error when --scenario missing")
	}
}

func TestCorpusAddYesWritesScenario(t *testing.T) {
	root, corpusDir := fakeRepo(t, false)
	chdir(t, root)
	fixture := filepath.Join(root, "fixture.txt")
	if err := os.WriteFile(fixture, []byte("ip 10.0.0.1 user alice@example.com\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err := runCmd(t, nil, "add", fixture, "--scenario", "kubectl-test", "--note", "from issue X", "--yes")
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	dir := filepath.Join(corpusDir, "kubectl-test")
	for _, f := range []string{"input.txt", "expected.txt", "README.md"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("missing %s: %v", f, err)
		}
	}
	expected, err := os.ReadFile(filepath.Join(dir, "expected.txt"))
	if err != nil {
		t.Fatal(err)
	}
	// Expected should contain canonicalized tokens, never the raw email.
	if bytes.Contains(expected, []byte("alice@example.com")) {
		t.Fatalf("raw email leaked into expected.txt: %s", expected)
	}
	if !bytes.Contains(expected, []byte("opsmask:")) || !bytes.Contains(expected, []byte(":*")) {
		t.Fatalf("expected canonicalized tokens, got: %s", expected)
	}
}

func TestCorpusAddRefusesExistingScenario(t *testing.T) {
	root, corpusDir := fakeRepo(t, false)
	chdir(t, root)
	if err := os.MkdirAll(filepath.Join(corpusDir, "already-there"), 0o755); err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(root, "fixture.txt")
	_ = os.WriteFile(fixture, []byte("hi\n"), 0o644)
	_, _, err := runCmd(t, nil, "add", fixture, "--scenario", "already-there", "--yes")
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected 'already exists' error, got %v", err)
	}
}

func TestCorpusAddNonTTYWithoutYesRefuses(t *testing.T) {
	root, _ := fakeRepo(t, false)
	chdir(t, root)
	fixture := filepath.Join(root, "fixture.txt")
	_ = os.WriteFile(fixture, []byte("hi\n"), 0o644)
	// pipe input is not a TTY; without --yes it must refuse rather than hang.
	_, _, err := runCmd(t, strings.NewReader(""), "add", fixture, "--scenario", "ok-name")
	if err == nil || !strings.Contains(err.Error(), "non-TTY") {
		t.Fatalf("expected non-TTY refusal, got %v", err)
	}
}

func TestCorpusAcceptMutuallyExclusive(t *testing.T) {
	root, _ := fakeRepo(t, false)
	chdir(t, root)
	_, _, err := runCmd(t, nil, "accept", "foo-bar", "--all")
	if err == nil || !strings.Contains(err.Error(), "cannot pass --all") {
		t.Fatalf("expected mutually exclusive error, got %v", err)
	}
}

func TestCorpusAcceptHappyPathClean(t *testing.T) {
	root, corpusDir := fakeRepo(t, true)
	// Create a scenario, then commit it so the tree is clean.
	scenarioDir := filepath.Join(corpusDir, "alpha-one")
	if err := os.MkdirAll(scenarioDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scenarioDir, "input.txt"), []byte("user alice@example.com\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scenarioDir, "expected.txt"), []byte("stale-content\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-q", "-m", "add scenario")
	chdir(t, root)
	_, _, err := runCmd(t, nil, "accept", "alpha-one")
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(scenarioDir, "expected.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(got, []byte("stale-content\n")) {
		t.Fatal("expected.txt was not regenerated")
	}
	if bytes.Contains(got, []byte("alice@example.com")) {
		t.Fatalf("raw email leaked: %s", got)
	}
}

func TestCorpusAcceptDirtyRefuses(t *testing.T) {
	root, corpusDir := fakeRepo(t, true)
	scenarioDir := filepath.Join(corpusDir, "dirty-one")
	if err := os.MkdirAll(scenarioDir, 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(scenarioDir, "input.txt"), []byte("hi\n"), 0o644)
	_ = os.WriteFile(filepath.Join(scenarioDir, "expected.txt"), []byte("hi\n"), 0o644)
	// Leave uncommitted -> dirty.
	chdir(t, root)
	_, _, err := runCmd(t, nil, "accept", "dirty-one")
	if err == nil || !strings.Contains(err.Error(), "uncommitted") {
		t.Fatalf("expected uncommitted error, got %v", err)
	}
	// Verify file unchanged.
	got, _ := os.ReadFile(filepath.Join(scenarioDir, "expected.txt"))
	if string(got) != "hi\n" {
		t.Fatalf("expected.txt mutated despite refusal: %q", got)
	}
}

func TestCorpusAcceptForceOverridesDirty(t *testing.T) {
	root, corpusDir := fakeRepo(t, true)
	scenarioDir := filepath.Join(corpusDir, "force-one")
	if err := os.MkdirAll(scenarioDir, 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(scenarioDir, "input.txt"), []byte("user alice@example.com\n"), 0o644)
	_ = os.WriteFile(filepath.Join(scenarioDir, "expected.txt"), []byte("stale\n"), 0o644)
	chdir(t, root)
	_, _, err := runCmd(t, nil, "accept", "force-one", "--force")
	if err != nil {
		t.Fatalf("accept --force: %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(scenarioDir, "expected.txt"))
	if string(got) == "stale\n" {
		t.Fatal("expected.txt not regenerated under --force")
	}
}

// TestCorpusAcceptAllTwoPhase verifies that when one scenario is dirty
// and --force is not passed, NO scenario's expected.txt is rewritten.
func TestCorpusAcceptAllTwoPhase(t *testing.T) {
	root, corpusDir := fakeRepo(t, true)
	mk := func(name, expected string) {
		dir := filepath.Join(corpusDir, name)
		_ = os.MkdirAll(dir, 0o755)
		_ = os.WriteFile(filepath.Join(dir, "input.txt"), []byte("hi\n"), 0o644)
		_ = os.WriteFile(filepath.Join(dir, "expected.txt"), []byte(expected), 0o644)
	}
	mk("clean-a", "clean-a-stale\n")
	mk("clean-b", "clean-b-stale\n")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-q", "-m", "two clean")
	mk("dirty-c", "dirty-c-stale\n")
	chdir(t, root)
	_, _, err := runCmd(t, nil, "accept", "--all")
	if err == nil {
		t.Fatal("expected accept --all to fail with dirty scenario")
	}
	for _, name := range []string{"clean-a", "clean-b"} {
		got, _ := os.ReadFile(filepath.Join(corpusDir, name, "expected.txt"))
		if string(got) != name+"-stale\n" {
			t.Errorf("scenario %q was mutated despite phase-1 abort: %q", name, got)
		}
	}
}

func TestCorpusListCommitted(t *testing.T) {
	root, corpusDir := fakeRepo(t, true)
	dir := filepath.Join(corpusDir, "alpha-list")
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, "input.txt"), []byte("hello\n"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "expected.txt"), []byte("hello\n"), 0o644)
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-q", "-m", "alpha-list")
	chdir(t, root)
	out, _, err := runCmd(t, nil, "list")
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(strings.TrimSpace(out), "\t")
	if len(parts) < 3 {
		t.Fatalf("expected 3 tab-separated columns, got %q", out)
	}
	if parts[0] != "alpha-list" {
		t.Fatalf("unexpected name column: %q", parts[0])
	}
	if parts[2] == "(no git history)" {
		t.Fatalf("expected committed scenario to have a date, got %q", parts[2])
	}
}

func TestAtomicWriteCleansUpOnSuccess(t *testing.T) {
	dir := t.TempDir()
	if err := atomicWrite(dir, "out.txt", []byte("hi\n")); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".tmp-") {
			t.Fatalf("temp file leaked: %s", e.Name())
		}
	}
	got, _ := os.ReadFile(filepath.Join(dir, "out.txt"))
	if string(got) != "hi\n" {
		t.Fatalf("got %q", got)
	}
}
