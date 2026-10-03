# Dios

Dios is a small command-line tool that synchronizes one directory into another, one way: `source → destination`. After a sync the destination matches the source. The source is never modified.

## Build

```
go build -ldflags "-X main.version=v1.0.0" -o dios ./cmd/dios
```

`main.version` is initialized to `dev` in the CLI and can be overridden at build time with linker flags. On Windows the output is `dios.exe`. You can also run it without building: `go run ./cmd/dios check src dst`.

## Installation

Install the latest version with Go:

```
go install github.com/ssajaia/dios/cmd/dios@latest
```

Tagged releases publish standalone binaries on the [GitHub Releases page](https://github.com/ssajaia/dios/releases): Linux amd64, Windows amd64 (`.exe`), and macOS amd64 and arm64. Each binary reports the version from its Git tag.

## Usage

```
dios init
dios sync [options] <source> <destination>
dios check [options] <source> <destination>
dios help [command]
dios --help
dios --version
```

`init` creates the alias file (see [Aliases](#aliases)). `sync` makes the destination match the source. `check` shows what `sync` would do and changes nothing, with the reason for each difference. `help` prints brief command usage, and `--version` prints the current build version.

Use the current-directory alias file at `.config/aliases` and include `-h` or `--help` to see command-specific options.

```
$ dios check ./source ./destination
Checking ./source -> ./destination

  + config.txt         missing in destination
  ~ documents/a.txt    contents differ
  + documents/b.txt    missing in destination
  - old.txt            not in source

Summary: 2 to create, 1 to update, 1 to remove.
No changes were made.
```

```
$ dios sync ./source ./destination
Syncing:
  + config.txt
  ~ documents/a.txt
  + documents/b.txt
  - old.txt

Sync completed.
```

| Symbol | Meaning |
|--------|---------|
| `+`    | created or copied |
| `~`    | updated |
| `-`    | removed |
| `!`    | skipped (symlinks and other special files in the source) |

Directories are shown with a trailing `/`. If nothing needs to change, both commands print `Already synchronized.`. Run `check` before `sync` to see what would be deleted.

## Options

Options can be placed anywhere among the arguments.

| Option | Commands | Effect |
|--------|----------|--------|
| `-s <name>`, `--skip <name>` | sync, check | Leave matching files and directories alone. Repeatable. |
| `--pd`, `--prevent-delete` | sync, check | Create and update, but never delete anything from the destination. |
| `--ns`, `--not-sure` | sync | Ask before every change. `y` or `Y` applies it, anything else skips it. |

**`--skip`** takes exactly one value per use, so repeat it for several names: `dios sync src dst -s main.py -s readme.md`. A name without `/` matches at any depth (`main.py` matches `docs/main.py` too). A name with `/` is a path relative to the source root (`docs/main.py`). `*`, `?` and `[...]` patterns work (`*.log`). Skipped entries are not copied, updated or deleted, and a skipped directory is not entered. They are not listed in the output.

**`--prevent-delete`** keeps files that exist only in the destination and lists them as `! name (kept: not in source)`. A change that would require deleting an existing entry, such as replacing a file with a directory, is skipped and listed with `!`.

**`--not-sure`** shows each change with its reason and waits for an answer:

```
$ dios sync --ns src dst
+ config.txt  (missing in destination)  apply? [y/N] y
~ documents/a.txt  (contents differ)  apply? [y/N] n
- old.txt  (not in source)  apply? [y/N] y

Syncing:
  + config.txt
  ! documents/a.txt (skipped by user)
  - old.txt

Sync completed.
```

Declining a new directory skips everything inside it. Closing the input (Ctrl+D, or Ctrl+Z then Enter on Windows) counts as "no" for all remaining changes. `check` never changes anything, so it does not accept `--ns`.

Behavior:

- Files are compared by content and replaced only if they differ.
- Extra files and directories in the destination are removed.
- Permissions and modification times of copied files are preserved.
- Files are written through a temporary file and renamed, so an interrupted copy does not leave a half-written file.
- Symlinks are never followed. A symlink in the destination is removed as a link; a symlink in the source is skipped.
- Source and destination must be different, and neither may be inside the other, including when nesting is hidden through symlinked paths.
- The destination is created if it does not exist.
- Exit codes: `0` when a check finds no differences or a sync succeeds, `1` when a check finds differences or a runtime error occurs, `2` on invalid usage.

## Aliases

Run `dios init` in the directory you work from. It creates `.config/aliases` with a commented template and prints `Created .config/aliases`. If the file already exists, `init` leaves it untouched and exits with an error.

Add one alias per line as `name = "path"`:

```
# .config/aliases
workspace1 = "C:/users/someone/projects"
backup = "/mnt/backup"
```

Then use the names instead of paths:

```
dios check workspace1 backup
dios sync workspace1 backup
```

- Use `/` in paths, or wrap the path in backticks to keep backslashes: ``work = `C:\users\someone` ``.
- Only arguments without `/` or `\` are looked up. `./workspace1` always means a real path.
- Aliases are read from `.config/aliases` in the current directory. A missing file is fine; a malformed one is an error that names the file and line.
- `.config/` is listed in `.gitignore` because it holds personal paths.

## Project layout

```
cmd/dios/            command-line interface: argument parsing, output, prompts
internal/syncer/     directory comparison and synchronization
internal/alias/      alias file parsing, lookup and creation
internal/testutil/   filesystem helpers shared by the tests
.github/workflows/   CI
```

## Development

```
gofmt -l .          # lists unformatted files
go vet ./...
go test ./...
go build ./...
```

Tests use temporary directories and check the resulting filesystem state, including nested directories, replaced and removed entries, options, prompts, aliases and error cases.
