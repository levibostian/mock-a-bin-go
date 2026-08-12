package mockbin

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// run executes name with args and returns its stdout and exit code. stderr is
// ignored unless the caller wants it; the *Mock tests that need it read it
// directly for the missing-binary case.
func run(t *testing.T, name string, args ...string) (string, int) {
	t.Helper()
	got, _, code := runEnv(t, nil, name, args...)
	return got, code
}

// runEnv is like run but exposes the child's stdout, stderr, and exit code so
// tests can assert on the full capture.
func runEnv(t *testing.T, extraEnv []string, name string, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(name, args...)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	cmd.Env = append(os.Environ(), extraEnv...)
	err := cmd.Run()
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			t.Fatalf("run %s %v: %v", name, args, err)
		}
	}
	return out.String(), errb.String(), code
}

func TestBasicMockAndUnmock(t *testing.T) {
	cleanup := mustMock(t, "git", "bash", `echo "mocking git!"`)
	got, _ := run(t, "git")
	if got != "mocking git!\n" {
		t.Errorf("got %q, want mocked output", got)
	}
	cleanup()

	// After cleanup the real git runs again.
	// (Only asserting it no longer echoes our mock.)
	got2, _ := run(t, "git", "--version")
	if strings.Contains(got2, "mocking git!") {
		t.Errorf("git still mocked after cleanup")
	}
}

func TestExitCode(t *testing.T) {
	cleanup := mustMock(t, "git", "bash", "exit 1")
	_, code := run(t, "git")
	if code != 1 {
		t.Errorf("got exit code %d, want 1", code)
	}
	cleanup()
}

func TestShebangForms(t *testing.T) {
	// Bare interpreter name.
	cleanup := mustMock(t, "git", "bash", `echo "mocked"`)
	if got, _ := run(t, "git"); got != "mocked\n" {
		t.Errorf("bare shebang: got %q", got)
	}
	cleanup()

	// Full path with #!.
	cleanup = mustMock(t, "git", "#!/bin/bash", `echo "mocked"`)
	if got, _ := run(t, "git"); got != "mocked\n" {
		t.Errorf("full path shebang: got %q", got)
	}
	cleanup()

	// env-style shebang.
	cleanup = mustMock(t, "git", "#!/usr/bin/env bash", `echo "mocked"`)
	if got, _ := run(t, "git"); got != "mocked\n" {
		t.Errorf("env shebang: got %q", got)
	}
	cleanup()
}

func TestArgumentsPassthrough(t *testing.T) {
	cleanup := mustMock(t, "gh", "bash", `echo "pr: $1 $2"`)
	got, _ := run(t, "gh", "pr", "list")
	if got != "pr: pr list\n" {
		t.Errorf("got %q, want %q", got, "pr: pr list\n")
	}
	cleanup()
}

func TestPythonAndNodeShebangs(t *testing.T) {
	cleanup := mustMock(t, "testbin", "python", "print('from python')")
	if got, _ := run(t, "testbin"); got != "from python\n" {
		t.Errorf("python shebang: got %q", got)
	}
	cleanup()

	cleanup = mustMock(t, "testbin", "node", `console.log("from node")`)
	if got, _ := run(t, "testbin"); got != "from node\n" {
		t.Errorf("node shebang: got %q", got)
	}
	cleanup()
}

func TestMultipleMocksAtOnce(t *testing.T) {
	cleanupGit := mustMock(t, "git", "bash", `echo "mocked git"`)
	cleanupGh := mustMock(t, "gh", "bash", `echo "mocked gh"`)

	if got, _ := run(t, "git"); got != "mocked git\n" {
		t.Errorf("git: got %q", got)
	}
	if got, _ := run(t, "gh"); got != "mocked gh\n" {
		t.Errorf("gh: got %q", got)
	}

	cleanupGit()
	cleanupGh()
}

func TestCleanupRestoresPATH(t *testing.T) {
	original, _ := os.LookupEnv("PATH")
	cleanup := mustMock(t, "git", "bash", `echo "test"`)
	if got := os.Getenv("PATH"); got == original {
		t.Error("PATH was not modified while mocked")
	}
	cleanup()
	if got := os.Getenv("PATH"); got != original {
		t.Errorf("PATH not restored: got %q, want %q", got, original)
	}
}

func TestCleanupIsIdempotent(t *testing.T) {
	cleanup := mustMock(t, "testbin", "bash", `echo "test"`)
	// Second call must not panic or error.
	cleanup()
	cleanup()
}

func TestCleanupEmptyOriginalPATH(t *testing.T) {
	original, originalSet := os.LookupEnv("PATH")
	t.Cleanup(func() {
		if originalSet {
			os.Setenv("PATH", original)
		} else {
			os.Unsetenv("PATH")
		}
	})

	os.Unsetenv("PATH")
	cleanup := mustMock(t, "testbin", "bash", `echo "test"`)

	if got := os.Getenv("PATH"); got == "" {
		t.Error("PATH should be non-empty while mocked")
	}
	cleanup()
	if _, set := os.LookupEnv("PATH"); set {
		t.Error("PATH should be unset after cleanup when it was unset before")
	}
}

func TestRunOriginalExecutesRealBinary(t *testing.T) {
	cleanup := mustMock(t, "git", "bash", `mock-a-bin-run-original "$@"`)
	got, _, code := runEnv(t, nil, "git", "--version")
	if code != 0 {
		t.Errorf("exit code %d, want 0", code)
	}
	if !strings.Contains(got, "git version") {
		t.Errorf("expected real git output, got %q", got)
	}
	cleanup()
}

func TestRunOriginalMissingBinary(t *testing.T) {
	const missing = "nonexistent-fake-binary-xyz"
	cleanup := mustMock(t, missing, "bash", `mock-a-bin-run-original "$@"`)
	_, errOut, code := runEnv(t, nil, missing)
	if code != 127 {
		t.Errorf("exit code %d, want 127", code)
	}
	if !strings.Contains(errOut, "Original '"+missing+"' command not found") {
		t.Errorf("stderr %q missing expected message", errOut)
	}
	cleanup()
}

func TestScriptBasedConditionalMocking(t *testing.T) {
	cleanup := mustMock(t, "git", "bash", `
if [ "$1" = "status" ]; then
  echo "mocked status output"
else
  mock-a-bin-run-original "$@"
fi
`)

	if got, _ := run(t, "git", "status"); got != "mocked status output\n" {
		t.Errorf("mocked subcommand: got %q", got)
	}
	if got, _, code := runEnv(t, nil, "git", "--version"); code != 0 || !strings.Contains(got, "git version") {
		t.Errorf("pass-through: code=%d out=%q", code, got)
	}
	cleanup()
}

func TestScriptBasedConditionalWithNode(t *testing.T) {
	cleanup := mustMock(t, "git", "node", `
const { spawnSync } = require('child_process')
if (process.argv[2] === 'status') {
  console.log('mocked from node')
} else {
  const result = spawnSync('mock-a-bin-run-original', process.argv.slice(2), { stdio: 'inherit' })
  process.exit(result.status || 0)
}
`)

	if got, _ := run(t, "git", "status"); got != "mocked from node\n" {
		t.Errorf("mocked via node: got %q", got)
	}
	if got, _, code := runEnv(t, nil, "git", "--version"); code != 0 || !strings.Contains(got, "git version") {
		t.Errorf("node pass-through: code=%d out=%q", code, got)
	}
	cleanup()
}

func TestEnvironmentPassesThroughRunOriginal(t *testing.T) {
	cleanup := mustMock(t, "env", "bash", `mock-a-bin-run-original "$@"`)
	got, _, code := runEnv(t, []string{
		"CUSTOM_TEST_VAR=my-custom-value",
		"ANOTHER_VAR=another-value",
	}, "env")
	if code != 0 {
		t.Errorf("exit code %d, want 0", code)
	}
	for _, want := range []string{"CUSTOM_TEST_VAR=my-custom-value", "ANOTHER_VAR=another-value"} {
		if !strings.Contains(got, want) {
			t.Errorf("env output missing %q", want)
		}
	}
	cleanup()
}

func TestPatternMatch(t *testing.T) {
	cleanup := mustMock(t, "git", "bash", `echo "mocked status"`,
		WithPattern(`^git status`))

	if got, _ := run(t, "git", "status"); got != "mocked status\n" {
		t.Errorf("matching command: got %q", got)
	}
	// Non-matching command runs the real binary.
	if got, _, code := runEnv(t, nil, "git", "--version"); code != 0 || !strings.Contains(got, "git version") {
		t.Errorf("non-matching command: code=%d out=%q", code, got)
	}
	cleanup()
}

func TestPatternAlternatives(t *testing.T) {
	cleanup := mustMock(t, "git", "bash", `echo "mocked: $*"`,
		WithPattern(`^git (status|log)`))

	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"status"}, "mocked: status\n"},
		{[]string{"log"}, "mocked: log\n"},
	} {
		if got, _ := run(t, "git", tc.args...); got != tc.want {
			t.Errorf("git %v: got %q, want %q", tc.args, got, tc.want)
		}
	}
	if got, _, code := runEnv(t, nil, "git", "--version"); code != 0 || !strings.Contains(got, "git version") {
		t.Errorf("non-matching command: code=%d out=%q", code, got)
	}
	cleanup()
}

func TestPatternSubcommandArguments(t *testing.T) {
	cleanup := mustMock(t, "git", "bash", `echo "mocked commit"`,
		WithPattern(`^git commit -m`))

	if got, _ := run(t, "git", "commit", "-m", "test"); got != "mocked commit\n" {
		t.Errorf("matching commit: got %q", got)
	}
	cleanup()
}

func TestEmptyPatternMatchesEverything(t *testing.T) {
	cleanup := mustMock(t, "git", "bash", `echo "mocked with empty pattern"`,
		WithPattern(""))
	if got, _ := run(t, "git", "status"); got != "mocked with empty pattern\n" {
		t.Errorf("empty pattern should match all, got %q", got)
	}
	cleanup()
}

func TestInvalidPatternReturnsError(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Mock panicked on invalid pattern: %v", r)
		}
	}()
	_, err := Mock("git", "bash", `echo hi`, WithPattern("([unclosed"))
	if err == nil {
		t.Error("expected error for invalid pattern")
	}
}

// mustMock creates a mock, failing the test on any error, and registers a
// cleanup to avoid leaking temp dirs and PATH mutations on a failed assertion.
func mustMock(t *testing.T, binName, shebang, code string, opts ...Option) func() error {
	t.Helper()
	cleanup, err := Mock(binName, shebang, code, opts...)
	if err != nil {
		t.Fatalf("Mock(%q): %v", binName, err)
	}
	t.Cleanup(func() { _ = cleanup() })
	return cleanup
}
