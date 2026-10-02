# Dios

Dios is a small command-line tool that synchronizes one directory into another, one way: `source → destination`. After a sync the destination matches the source. The source is never modified.

## Build

```
go build -o dios .
```

## Usage

```
dios sync [--dry-run] <source> <destination>
```

`--dry-run` can be placed anywhere among the arguments. It prints what a sync would do and changes nothing.

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

Directories are shown with a trailing `/`. If nothing needs to change, Dios prints `Already synchronized.`.

Behavior:

- Files are compared by content and replaced only if they differ.
- Extra files and directories in the destination are removed.
- Permissions and modification times of copied files are preserved.
- Symlinks are never followed. A symlink in the destination is removed as a link; a symlink in the source is skipped.
- Source and destination must be different, and neither may be inside the other.
- The destination is created if it does not exist.

## Aliases

Dios reads aliases from `.config/aliases` in the current directory. Each line is `name = "path"`:

```
# .config/aliases
workspace1 = "C:/users/someone/projects"
backup = "/mnt/backup"
```

```
dios sync workspace1 backup
```

- Use `/` in paths, or wrap the path in backticks to keep backslashes: ``work = `C:\users\someone` ``.
- Only arguments without `/` or `\` are looked up. `./workspace1` always means a real path.
- A missing alias file is fine. A malformed one is an error that names the file and line.

## Development

```
gofmt -l .          # lists unformatted files
go vet ./...
go test ./...
go build ./...
```
