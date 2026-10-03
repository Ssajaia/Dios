# Dios

Dios is a small command-line tool that synchronizes one directory into another, one way: `source → destination`. After a sync the destination matches the source. The source is never modified.

## Build

```
go build -o dios .
```

## Usage

```
dios init
dios sync <source> <destination>
dios check <source> <destination>
```

`init` creates the alias file (see [Aliases](#aliases)). `sync` makes the destination match the source. `check` shows what `sync` would do and changes nothing, with the reason for each difference.

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

Behavior:

- Files are compared by content and replaced only if they differ.
- Extra files and directories in the destination are removed.
- Permissions and modification times of copied files are preserved.
- Files are written through a temporary file and renamed, so an interrupted copy does not leave a half-written file.
- Symlinks are never followed. A symlink in the destination is removed as a link; a symlink in the source is skipped.
- Source and destination must be different, and neither may be inside the other.
- The destination is created if it does not exist.
- Exit codes: `0` on success, `1` on a runtime error, `2` on invalid usage.

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

## Development

```
gofmt -l .          # lists unformatted files
go vet ./...
go test ./...
go build ./...
```