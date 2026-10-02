# Dios

Dios is a small command-line tool that synchronizes one directory into another, one way: `source → destination`. After a sync, the destination matches the source. The source is never modified.

It is useful for keeping a backup or mirror folder up to date without copying everything again. Files that are already identical are left alone.

## Build

```
go build -o dios .
```

## Usage

```
dios sync [--dry-run] <source> <destination>
```

`--dry-run` can be placed anywhere among the arguments. It prints what a sync would do and changes nothing.

Example:

```
$ dios sync ./source ./destination
Syncing:
  + config.txt
  ~ documents/a.txt
  + documents/b.txt
  - old.txt

Sync completed.
```

Dry run:

```
$ dios sync --dry-run ./source ./destination
Dry run:

  + config.txt
  ~ documents/a.txt
  + documents/b.txt
  - old.txt

No changes were made.
```

If nothing needs to change, Dios prints `Already synchronized.`.

| Symbol | Meaning |
|--------|---------|
| `+`    | created or copied |
| `~`    | updated |
| `-`    | removed |
| `!`    | skipped (symlinks and other special files in the source) |

Directories are shown with a trailing `/`.

## Behavior

- Missing files and directories are created in the destination.
- Files are compared by content and replaced only if they differ.
- Files and directories that exist only in the destination are removed.
- A file replaced by a directory (or the reverse) is reported as `~`.
- Permissions and modification times of copied files are preserved.
- Files are written through a temporary file and renamed, so an interrupted copy does not leave a half-written file.
- Symlinks are never followed. A symlink in the destination is removed as a link, and a symlink in the source is skipped.
- The destination is created if it does not exist.
- The source and destination must be different, and neither may be inside the other.
- Filesystem errors stop the sync and are printed as `Error: ...`. Changes already made are listed first.

Exit codes: `0` on success, `1` on a runtime error, `2` on invalid usage.

## Development

```
gofmt -l .          # lists unformatted files
go vet ./...
go test ./...
go build ./...
```

Tests use temporary directories and check the resulting filesystem state, including dry-run, nested directories, replaced and removed entries, and error cases.
