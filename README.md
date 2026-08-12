# mock-a-bin-go

> Mock any executable binary (Go port of [mock-a-bin](https://github.com/levibostian/mock-a-bin))

Have a Go program or test that shells out to binary commands? Want to test it by
mocking those commands instead of the real tools? This library lets you mock any
executable binary by injecting a mock script into your PATH.

> [!IMPORTANT]  
> I have not tested the code myself, just rely on automated tests for validation. Just had it written to use in the future. Since I have not tried it in a project, the API might not be ideal and breaking changes may be introduced in future versions. 

## Install

```sh
go get github.com/levibostian/mock-a-bin-go
```

## Usage

### Mock all commands

```go
import "github.com/levibostian/mock-a-bin-go"

cleanup, err := mockbin.Mock("gh", "bash", `echo "mocked output"`)
if err != nil {
    return err
}
defer cleanup()

// Now any call to "gh" returns "mocked output".
out, _ := exec.Command("gh", "pr", "list").Output()
fmt.Println(string(out)) // "mocked output"
```

`shebang` is the interpreter that runs your code — `bash`, `node`, `python`,
etc. It may be given with or without the leading `#!`.

## Conditional mocking

Sometimes you want to mock only specific commands or subcommands while letting
others run normally. Two approaches.

### Option 1: Pattern-based mocking

Use a regular expression to mock only matching commands:

```go
// Mock only git status.
cleanup, _ := mockbin.Mock("git", "bash", `echo "modified: file.txt"`,
    mockbin.WithPattern(`^git status`))

// Mock multiple subcommands.
cleanup, _ := mockbin.Mock("docker", "bash", `echo "Docker operation mocked"`,
    mockbin.WithPattern(`^docker (build|push)`))

// Mock commit with any message.
cleanup, _ := mockbin.Mock("git", "bash", `echo "[main abc123] Commit"`,
    mockbin.WithPattern(`^git commit -m`))
```

Commands that do not match the pattern run the real binary.

### Option 2: Script-based with `mock-a-bin-run-original`

When you create a mock, a helper binary `mock-a-bin-run-original` is created
alongside it. Your mock script can call it to run the original command under the
original PATH — arguments, environment, and exit code all pass through.

```go
// Mock only "git status", pass everything else to real git.
cleanup, _ := mockbin.Mock("git", "bash", `
if [ "$1" = "status" ]; then
    echo "Everything is clean!"
else
    mock-a-bin-run-original "$@"
fi
`)
```

Works with any interpreter:

```go
// Node
cleanup, _ := mockbin.Mock("git", "node", `
const { spawnSync } = require('child_process')
if (process.argv[2] === 'status') {
    console.log('Mocked status')
} else {
    const r = spawnSync('mock-a-bin-run-original', process.argv.slice(2), { stdio: 'inherit' })
    process.exit(r.status || 0)
}
`)

// Python
cleanup, _ := mockbin.Mock("git", "python", `
import sys, subprocess
if len(sys.argv) > 1 and sys.argv[1] == 'status':
    print('Mocked status')
else:
    sys.exit(subprocess.call(['mock-a-bin-run-original'] + sys.argv[1:]))
`)
```

### Which approach?

- **Pattern-based** for simple, static matching ("mock all `gh pr` commands").
- **Script-based with `mock-a-bin-run-original`** when you need to inspect
  arguments or environment before deciding, or want human-readable delegation.

## How it works

`Mock` creates a temporary directory, writes a mock script named after the
binary (plus the `mock-a-bin-run-original` helper), chmods it executable, and
prepends the directory to PATH. Because the temp dir is first in PATH, it
shadows the real binary. The returned cleanup restores the original PATH and
removes the temp dir; it is safe to call more than once.

## Limitations

- Unix-only. The generated mocks are shell scripts; Windows shims are not
  implemented.
- Pattern matching rebuilds the command line with space-joined arguments, so
  argument quoting is not preserved. Use script-based conditional mocking when
  that matters.
- `mock-a-bin-run-original` is bash; script-based mocks delegating to it need
  `bash` on PATH.
