// Package mockbin mocks any executable binary during tests by injecting a
// mock script into PATH.
//
// Mock creates a temporary directory containing a mock script named after the
// target binary and prepends that directory to PATH. Any command executed via
// os/exec for that binary name then resolves to the mock script instead of the
// real binary.
//
// # Basic usage
//
//	cleanup, err := mockbin.Mock("gh", "bash", `echo "mocked output"`)
//	if err != nil {
//		t.Fatal(err)
//	}
//	defer cleanup()
//
//	// Any call to "gh" now returns "mocked output".
//	out, _ := exec.Command("gh", "pr", "list").Output()
//
// # Conditional mocking
//
// By default every invocation of the binary is mocked. Use [WithPattern] to
// mock only commands whose full command line matches a regular expression,
// letting everything else fall through to the real binary:
//
//	cleanup, err := mockbin.Mock("git", "bash", `echo "mocked status"`,
//		mockbin.WithPattern(`^git status`))
//
// For more flexible logic, your mock script can delegate to the real binary by
// invoking the automatically-created "mock-a-bin-run-original" helper, which
// reproduces the original command (arguments, environment, and exit code):
//
//	cleanup, err := mockbin.Mock("git", "bash", `
//		if [ "$1" = "status" ]; then
//			echo "Everything is clean!"
//		else
//			mock-a-bin-run-original "$@"
//		fi
//	`)
package mockbin

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// Option configures a binary mock.
type Option func(*options)

type options struct {
	pattern string
}

// WithPattern restricts the mock to only commands whose full command line
// (the binary name followed by its arguments) matches pattern. Commands that
// do not match run the real binary. Use this for simple, static matching such
// as "mock only 'gh pr list'".
//
// Matching is done against a space-joined reconstruction of the command line,
// so quoting is not preserved. If you need to inspect arguments or environment
// variables before deciding whether to mock, write that logic in your script
// and delegate with the "mock-a-bin-run-original" helper instead.
func WithPattern(pattern string) Option {
	return func(o *options) { o.pattern = pattern }
}

// Mock creates a mock executable that shadows binName and returns a cleanup
// function that undoes it.
//
// shebang is the interpreter that runs code, e.g. "bash", "node", or "python".
// It may be given with or without the leading "#!"; Mock normalizes it to
// "#!/usr/bin/env <shebang>". code is executed by that interpreter for every
// invocation that is mocked.
//
// The returned cleanup restores PATH to its value before Mock was called and
// removes the temporary directory that held the mock scripts. It is safe to
// call more than once; the PATH is always restored even if removing the
// temporary directory fails. Use it with defer:
//
//	cleanup, err := mockbin.Mock("gh", "bash", `echo "mocked output"`)
//	defer cleanup()
func Mock(binName, shebang, code string, opts ...Option) (func() error, error) {
	o := options{}
	for _, opt := range opts {
		opt(&o)
	}

	// Validate the pattern up front so an invalid one is a returned error
	// rather than a panic deep inside script generation.
	var pattern *regexp.Regexp
	if o.pattern != "" {
		var err error
		pattern, err = regexp.Compile(o.pattern)
		if err != nil {
			return nil, fmt.Errorf("mockbin: invalid pattern %q: %w", o.pattern, err)
		}
	}

	originalPath, originalPathSet := os.LookupEnv("PATH")

	// Resolve the real binary before PATH is modified, so exec.LookPath does
	// not inadvertently resolve to our own mock.
	var realBinPath string
	var realBinErr error
	if pattern != nil {
		realBinPath, realBinErr = exec.LookPath(binName)
	}

	tempDir, err := os.MkdirTemp("", "mock-bin-")
	if err != nil {
		return nil, fmt.Errorf("mockbin: create temp dir: %w", err)
	}
	cleanup := func() error {
		if originalPathSet {
			os.Setenv("PATH", originalPath)
		} else {
			os.Unsetenv("PATH")
		}
		return os.RemoveAll(tempDir)
	}

	// mock-a-bin-run-original resolves the real binary against the original
	// PATH and executes it, preserving arguments, environment, and exit code.
	// It is how a mock script delegates back to the real command.
	runOriginalScript := fmt.Sprintf(`#!/bin/bash
# Find and execute the original binary, bypassing this mock directory.
export PATH=%s
ORIGINAL_BIN=$(command -v %s 2>/dev/null)
if [ -n "$ORIGINAL_BIN" ]; then
  exec "$ORIGINAL_BIN" "$@"
else
  echo "Error: Original '%s' command not found in PATH" >&2
  exit 127
fi
`, shellSingleQuote(originalPath), shellSingleQuote(binName), binName)
	if err := writeExecutable(filepath.Join(tempDir, "mock-a-bin-run-original"), runOriginalScript); err != nil {
		cleanup()
		return nil, fmt.Errorf("mockbin: write run-original helper: %w", err)
	}

	normalizedShebang := shebang
	if !strings.HasPrefix(shebang, "#!") {
		normalizedShebang = "#!/usr/bin/env " + shebang
	}

	var userScript string
	if pattern != nil {
		fallback := fmt.Sprintf(`exec %s "$@"`, shellSingleQuote(realBinPath))
		if realBinErr != nil {
			fallback = fmt.Sprintf(`echo "Error: Real binary '%s' not found in PATH" >&2
  exit 127`, binName)
		}
		userScript = fmt.Sprintf(`%s
FULL_COMMAND="%s $*"
if echo "$FULL_COMMAND" | grep -qE %s; then
%s
else
  %s
fi
`, normalizedShebang, binName, shellSingleQuote(o.pattern), code, fallback)
	} else {
		userScript = normalizedShebang + "\n" + code + "\n"
	}

	if err := writeExecutable(filepath.Join(tempDir, binName), userScript); err != nil {
		cleanup()
		return nil, fmt.Errorf("mockbin: write mock script: %w", err)
	}

	// Prepend the temporary directory so our mock shadows the real binary.
	newPath := tempDir
	if originalPathSet {
		newPath = tempDir + string(os.PathListSeparator) + originalPath
	}
	os.Setenv("PATH", newPath)

	return cleanup, nil
}

// writeExecutable writes content to path and marks it executable. The
// explicit Chmod makes the mode stick even if the file already existed.
func writeExecutable(path, content string) error {
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		return err
	}
	return os.Chmod(path, 0o755)
}

// shellSingleQuote wraps s in single quotes, escaping any embedded single
// quotes, so s can be embedded safely in a generated bash script.
func shellSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
