# mock-a-bin-go — Development Guide

Implementation-facing notes for anyone changing this library. Keep this file in
sync with the code.

## Purpose & layer boundary

`mock-a-bin-go` is a zero-dependency Go library that mocks any executable
binary during tests by injecting a mock script into PATH. It is a port of
[levibostian/mock-a-bin](https://github.com/levibostian/mock-a-bin) (Deno) to Go.

Single public entry point (package `mockbin`):

```go
cleanup, err := mockbin.Mock("gh", "bash", `echo "mocked"`, mockbin.WithPattern(`^gh pr`))
defer cleanup()
```

**Layer boundary:** `Mock` is pure glue. It writes two shell scripts and flips
`PATH`. There is no in-process behavior worth unit testing in isolation — a
"mock" only does anything once a real child process runs.

## Responsibilities of `Mock`

1. Resolve the real binary via `exec.LookPath` **before** mutating PATH (so it
   never resolves to the mock itself).
2. `os.MkdirTemp("", "mock-bin-")`.
3. Write `mock-a-bin-run-original` (bash helper that restores original PATH and
   `exec`s the real binary, preserving args/env/exit code).
4. Write the mock script at `tempDir/<binName>`.
5. Prepend `tempDir` to PATH.
6. Return a cleanup closure: restore PATH (or `Unsetenv` it if it wasn't set),
   then `os.RemoveAll(tempDir)`. Idempotent.

## Design decisions (and why)

### Public API

- `Cleanup` is a returned closure `func() error`, **not** a struct. Idiomatic Go
  (`defer cleanup()`), idempotent, and avoids a name collision (a type named
  `Mock` would collide with the function `Mock`).
- `WithPattern` is a **functional option**, not a `Config` struct. Keeps the
  no-option call one line and stays extensible. Deno used a `string | object`
  union; Go has no such union, so extensibility goes through options.
- Invalid regex is a **returned error**, never a panic. Pattern is validated
  with `regexp.Compile` up front.
- Errors are wrapped with `fmt.Errorf("...: %w", err)` context on every failure
  path; a failed write cleans up the temp dir before returning.

### No wrapper indirection (deliberate deviation from Deno)

Deno writes a bash wrapper at `tempDir/<binName>` that `exec`s a hidden payload
at `tempDir/.<binName>-user-script`. This package writes user code **directly**
to `tempDir/<binName>`, one file instead of two.

This is safe. Deno's original `v1` wrote directly; the wrapper was introduced
alongside conditional mocking and is structural, not functional. The Go port
passes the full behavior matrix (bash/node/python shebangs, pattern matching,
`mock-a-bin-run-original` delegation, env passthrough, exit codes) without it.
See git history in the upstream repo for confirmation.

### Pattern matching stays in shell (`grep -qE`)

Pattern decisions are made inside the generated bash script via `echo ... |
grep -qE`. Not translated to Go `regexp`, so the match semantics are POSIX
ERE. This is a faithful port; a valid-but-different RE2-vs-ERE edge is accepted
and documented in README.

### Windows

Unix-only. Generated mocks are shell scripts. Windows `.cmd`/`.bat` shims are
not implemented. `PATH` separator uses `os.PathListSeparator` regardless, but
the scripts themselves require a POSIX shell.

## Generated files

`tempDir` (e.g. `/tmp/mock-bin-1234/`) contains exactly two executables (0755):

- `<binName>` — the mock. Shebang normalized to `#!/usr/bin/env <shebang>`
  unless it already starts with `#!`. For pattern mode, the script wraps the
  mock code in a `grep -qE` check and falls back to `exec <realBinary> "$@"`.
- `mock-a-bin-run-original` — bash helper resolving the original binary against
  the saved PATH. User mocks call `mock-a-bin-run-original "$@"` to delegate.

`shellSingleQuote` escapes `'` for safe embedding of arbitrary strings into
generated bash.

## Commands

```sh
# build + vet
go build ./...
go vet ./...

# format (must be clean — CI rejects unformatted)
gofmt -l .

# tests with coverage
go test ./...
go test -race -count=1 ./...
go test -coverprofile=/tmp/cov.out ./... && go tool cover -func=/tmp/cov.out
```

There are no lint tools configured beyond `gofmt`/`go vet`. There is no
`go.sum` because the module has zero third-party dependencies; do not add a
dependency for something a few lines of stdlib can do.

## Testing strategy

All tests are **integration tests** — they spawn real child processes
(`bash`, `node`, `python`, `git`, `env`) that execute the generated scripts.
This is intentional: the library is glue, so behavior-level tests are the
meaningful contract and won't churn on refactors that don't change behavior.

Coverage ratio is high (~86% statements). The uncovered lines are exclusively
filesystem error branches (`MkdirTemp`/`writeExecutable` failures) that require
fault injection to reach. They are compile-checked; do not add machinery to
force-test them.

**Critical constraint:** tests mutate the global process `PATH`. Therefore
none may call `t.Parallel()`, and each test restores PATH via `t.Cleanup`.
`mustMock` helper registers cleanup automatically.

Port a Deno test as an integration test too; do not write text assertions on the
generated script.

## Module

- Module path: `github.com/levibostian/mock-a-bin-go` (matches remote
  `git@github.com:levibostian/mock-a-bin-go.git`).
- Root package `mockbin` at repo root, mirroring Deno's single `main.ts`.
- Go toolchain: `go 1.26.1`.

## Maintenance notes

- If you change cleanup semantics, the "cleanup is idempotent" contract is a
  tested guarantee — update `TestCleanupIsIdempotent` accordingly.
- If you make pattern matching Go-side instead of `grep -qE`, update README's
  limitations note about RE2-vs-ERE.
- If you ever genuinely need `$0` to differ inside the mock, that is the one
  reason to re-introduce the Deno wrapper. Don't add it speculatively.