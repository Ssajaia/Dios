package main

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestSync(t *testing.T) {
	bigA := strings.Repeat("a", 70000)
	bigB := strings.Repeat("a", 69999) + "b"

	tests := []struct {
		name string
		src  map[string]string
		dst  map[string]string
		want []string
	}{
		{
			name: "empty source and empty destination",
			src:  map[string]string{},
			dst:  map[string]string{},
		},
		{
			name: "empty source and missing destination",
			src:  map[string]string{},
			dst:  nil,
			want: []string{"+ ./"},
		},
		{
			name: "missing file",
			src:  map[string]string{"a.txt": "a"},
			dst:  map[string]string{},
			want: []string{"+ a.txt"},
		},
		{
			name: "empty directory",
			src:  map[string]string{"empty/": ""},
			dst:  map[string]string{},
			want: []string{"+ empty/"},
		},
		{
			name: "modified file with different size",
			src:  map[string]string{"a.txt": "new content"},
			dst:  map[string]string{"a.txt": "old"},
			want: []string{"~ a.txt"},
		},
		{
			name: "modified file with same size",
			src:  map[string]string{"a.txt": "abc"},
			dst:  map[string]string{"a.txt": "abd"},
			want: []string{"~ a.txt"},
		},
		{
			name: "large file differing in the last byte",
			src:  map[string]string{"big.bin": bigA},
			dst:  map[string]string{"big.bin": bigB},
			want: []string{"~ big.bin"},
		},
		{
			name: "identical large file",
			src:  map[string]string{"big.bin": bigA},
			dst:  map[string]string{"big.bin": bigA},
		},
		{
			name: "extra file",
			src:  map[string]string{},
			dst:  map[string]string{"old.txt": "old"},
			want: []string{"- old.txt"},
		},
		{
			name: "extra directory with contents",
			src:  map[string]string{},
			dst:  map[string]string{"extra/": "", "extra/n/": "", "extra/n/x.txt": "x"},
			want: []string{"- extra/"},
		},
		{
			name: "missing nested directories",
			src:  map[string]string{"a/b/c/f.txt": "1"},
			dst:  map[string]string{},
			want: []string{"+ a/", "+ a/b/", "+ a/b/c/", "+ a/b/c/f.txt"},
		},
		{
			name: "changes at several nested levels",
			src:  map[string]string{"d/e/f.txt": "new"},
			dst:  map[string]string{"d/e/f.txt": "old", "d/e/g.txt": "g", "d/h/": ""},
			want: []string{"~ d/e/f.txt", "- d/e/g.txt", "- d/h/"},
		},
		{
			name: "identical trees",
			src:  map[string]string{"a.txt": "a", "d/b.txt": "b", "d/e/": ""},
			dst:  map[string]string{"a.txt": "a", "d/b.txt": "b", "d/e/": ""},
		},
		{
			name: "file replaced by directory",
			src:  map[string]string{"x/f.txt": "1"},
			dst:  map[string]string{"x": "file"},
			want: []string{"~ x/", "+ x/f.txt"},
		},
		{
			name: "directory replaced by file",
			src:  map[string]string{"x": "data"},
			dst:  map[string]string{"x/inner.txt": "i"},
			want: []string{"~ x"},
		},
		{
			name: "mixed changes",
			src: map[string]string{
				"config.txt":      "c",
				"hello.txt":       "h",
				"documents/a.txt": "new a",
				"documents/b.txt": "b",
			},
			dst: map[string]string{
				"hello.txt":       "h",
				"old.txt":         "o",
				"documents/a.txt": "old a",
			},
			want: []string{"+ config.txt", "~ documents/a.txt", "+ documents/b.txt", "- old.txt"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			src := filepath.Join(root, "src")
			dst := filepath.Join(root, "dst")
			build(t, src, tt.src)
			build(t, dst, tt.dst)
			srcBefore := snapshot(t, src)
			dstBefore := snapshot(t, dst)

			changes, err := syncDirs(src, dst, true)
			if err != nil {
				t.Fatalf("dry run: %v", err)
			}
			assertChanges(t, "dry run", changes, tt.want)
			assertTree(t, "source after dry run", snapshot(t, src), srcBefore)
			assertTree(t, "destination after dry run", snapshot(t, dst), dstBefore)

			changes, err = syncDirs(src, dst, false)
			if err != nil {
				t.Fatalf("sync: %v", err)
			}
			assertChanges(t, "sync", changes, tt.want)
			assertTree(t, "source after sync", snapshot(t, src), srcBefore)
			assertTree(t, "destination after sync", snapshot(t, dst), srcBefore)

			changes, err = syncDirs(src, dst, false)
			if err != nil {
				t.Fatalf("second sync: %v", err)
			}
			assertChanges(t, "second sync", changes, nil)
		})
	}
}

func TestSyncErrors(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(t *testing.T, root string) (src, dst string)
		wantErr string
	}{
		{
			name: "missing source",
			setup: func(t *testing.T, root string) (string, string) {
				return filepath.Join(root, "missing"), filepath.Join(root, "dst")
			},
			wantErr: "source does not exist",
		},
		{
			name: "source is a file",
			setup: func(t *testing.T, root string) (string, string) {
				src := filepath.Join(root, "file.txt")
				writeFile(t, src, "x")
				return src, filepath.Join(root, "dst")
			},
			wantErr: "source is not a directory",
		},
		{
			name: "destination is a file",
			setup: func(t *testing.T, root string) (string, string) {
				dst := filepath.Join(root, "dst.txt")
				writeFile(t, dst, "x")
				return mkdir(t, root, "src"), dst
			},
			wantErr: "destination is not a directory",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src, dst := tt.setup(t, t.TempDir())
			changes, err := syncDirs(src, dst, false)
			checkErr(t, err, tt.wantErr)
			if len(changes) != 0 {
				t.Errorf("changes = %v, want none", changes)
			}
		})
	}
}

func TestSyncPreservesMetadata(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	build(t, src, map[string]string{"a.txt": "data"})

	mtime := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	file := filepath.Join(src, "a.txt")
	if err := os.Chmod(file, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(file, mtime, mtime); err != nil {
		t.Fatal(err)
	}

	if _, err := syncDirs(src, dst, false); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(filepath.Join(dst, "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(mtime) {
		t.Errorf("modtime = %v, want %v", info.ModTime(), mtime)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o640 {
		t.Errorf("mode = %v, want %v", info.Mode().Perm(), fs.FileMode(0o640))
	}
}

func TestSyncLeavesNoTempFiles(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	build(t, src, map[string]string{"a.txt": "new", "d/b.txt": "b"})
	build(t, dst, map[string]string{"a.txt": "old"})

	if _, err := syncDirs(src, dst, false); err != nil {
		t.Fatal(err)
	}
	for name := range snapshot(t, dst) {
		if strings.Contains(name, ".dios-tmp-") {
			t.Errorf("temp file left behind: %s", name)
		}
	}
}

func TestSyncSkipsSourceSymlink(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	outside := filepath.Join(root, "outside.txt")
	writeFile(t, outside, "outside")
	build(t, src, map[string]string{"a.txt": "a"})
	if err := os.Symlink(outside, filepath.Join(src, "link")); err != nil {
		t.Skipf("symlinks not supported: %v", err)
	}

	changes, err := syncDirs(src, dst, false)
	if err != nil {
		t.Fatal(err)
	}
	assertChanges(t, "sync", changes, []string{"+ ./", "+ a.txt", "! link (skipped: symlink)"})
	if _, err := os.Lstat(filepath.Join(dst, "link")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("symlink was copied to destination: %v", err)
	}
}

func TestSyncRemovesDestinationSymlinkWithoutFollowing(t *testing.T) {
	root := t.TempDir()
	src := mkdir(t, root, "src")
	dst := mkdir(t, root, "dst")
	outside := mkdir(t, root, "outside")
	keep := filepath.Join(outside, "keep.txt")
	writeFile(t, keep, "keep")
	if err := os.Symlink(outside, filepath.Join(dst, "link")); err != nil {
		t.Skipf("symlinks not supported: %v", err)
	}

	changes, err := syncDirs(src, dst, false)
	if err != nil {
		t.Fatal(err)
	}
	assertChanges(t, "sync", changes, []string{"- link"})
	if _, err := os.Lstat(filepath.Join(dst, "link")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("symlink still exists: %v", err)
	}
	if data, err := os.ReadFile(keep); err != nil || string(data) != "keep" {
		t.Errorf("file behind symlink was touched: %q, %v", data, err)
	}
}

func TestValidatePaths(t *testing.T) {
	sep := string(filepath.Separator)

	tests := []struct {
		name    string
		setup   func(t *testing.T, root string) (src, dst string)
		wantErr string
	}{
		{
			name: "missing source",
			setup: func(t *testing.T, root string) (string, string) {
				return filepath.Join(root, "missing"), filepath.Join(root, "dst")
			},
			wantErr: "source does not exist",
		},
		{
			name: "source is a file",
			setup: func(t *testing.T, root string) (string, string) {
				src := filepath.Join(root, "file.txt")
				writeFile(t, src, "x")
				return src, filepath.Join(root, "dst")
			},
			wantErr: "source is not a directory",
		},
		{
			name: "same path",
			setup: func(t *testing.T, root string) (string, string) {
				src := mkdir(t, root, "src")
				return src, src
			},
			wantErr: "must be different paths",
		},
		{
			name: "same path with different spelling",
			setup: func(t *testing.T, root string) (string, string) {
				src := mkdir(t, root, "src")
				return src, src + sep + "."
			},
			wantErr: "must be different paths",
		},
		{
			name: "destination inside source",
			setup: func(t *testing.T, root string) (string, string) {
				src := mkdir(t, root, "src")
				return src, filepath.Join(src, "dst")
			},
			wantErr: "destination must not be inside source",
		},
		{
			name: "source inside destination",
			setup: func(t *testing.T, root string) (string, string) {
				dst := mkdir(t, root, "dst")
				src := mkdir(t, dst, "src")
				return src, dst
			},
			wantErr: "source must not be inside destination",
		},
		{
			name: "destination is a file",
			setup: func(t *testing.T, root string) (string, string) {
				src := mkdir(t, root, "src")
				dst := filepath.Join(root, "dst.txt")
				writeFile(t, dst, "x")
				return src, dst
			},
			wantErr: "destination is not a directory",
		},
		{
			name: "missing destination",
			setup: func(t *testing.T, root string) (string, string) {
				return mkdir(t, root, "src"), filepath.Join(root, "dst")
			},
		},
		{
			name: "existing destination",
			setup: func(t *testing.T, root string) (string, string) {
				return mkdir(t, root, "src"), mkdir(t, root, "dst")
			},
		},
		{
			name: "sibling directories with common prefix",
			setup: func(t *testing.T, root string) (string, string) {
				return mkdir(t, root, "data"), mkdir(t, root, "data-backup")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src, dst := tt.setup(t, t.TempDir())
			checkErr(t, validatePaths(src, dst), tt.wantErr)
		})
	}
}

func TestValidatePathsSymlinkToSource(t *testing.T) {
	root := t.TempDir()
	src := mkdir(t, root, "src")
	link := filepath.Join(root, "link")
	if err := os.Symlink(src, link); err != nil {
		t.Skipf("symlinks not supported: %v", err)
	}
	checkErr(t, validatePaths(src, link), "must be different paths")
}

func TestParseSyncArgs(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantDryRun bool
		wantPaths  []string
		wantErr    string
	}{
		{name: "paths only", args: []string{"a", "b"}, wantPaths: []string{"a", "b"}},
		{name: "flag first", args: []string{"--dry-run", "a", "b"}, wantDryRun: true, wantPaths: []string{"a", "b"}},
		{name: "flag between", args: []string{"a", "--dry-run", "b"}, wantDryRun: true, wantPaths: []string{"a", "b"}},
		{name: "flag last", args: []string{"a", "b", "--dry-run"}, wantDryRun: true, wantPaths: []string{"a", "b"}},
		{name: "no arguments", args: nil, wantErr: "expected <source> and <destination>"},
		{name: "one path", args: []string{"a"}, wantErr: "expected <source> and <destination>"},
		{name: "three paths", args: []string{"a", "b", "c"}, wantErr: "expected <source> and <destination>"},
		{name: "unknown option", args: []string{"--force", "a", "b"}, wantErr: "unknown option: --force"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dryRun, src, dst, err := parseSyncArgs(tt.args)
			checkErr(t, err, tt.wantErr)
			if tt.wantErr != "" {
				return
			}
			if dryRun != tt.wantDryRun {
				t.Errorf("dryRun = %v, want %v", dryRun, tt.wantDryRun)
			}
			if got := []string{src, dst}; !reflect.DeepEqual(got, tt.wantPaths) {
				t.Errorf("paths = %v, want %v", got, tt.wantPaths)
			}
		})
	}
}

func TestRun(t *testing.T) {
	root := t.TempDir()
	chdir(t, root)
	src := mkdir(t, root, "src")
	missing := filepath.Join(root, "missing")
	dst := filepath.Join(root, "dst")

	tests := []struct {
		name       string
		args       []string
		wantCode   int
		wantStderr string
	}{
		{name: "no command", args: nil, wantCode: 2, wantStderr: "Error: missing command"},
		{name: "unknown command", args: []string{"copy"}, wantCode: 2, wantStderr: "Error: unknown command: copy"},
		{name: "wrong argument count", args: []string{"sync", "a"}, wantCode: 2, wantStderr: "Error: expected"},
		{name: "unknown option", args: []string{"sync", "--force", "a", "b"}, wantCode: 2, wantStderr: "Error: unknown option"},
		{name: "missing source", args: []string{"sync", missing, dst}, wantCode: 1, wantStderr: "Error: source does not exist: " + missing},
		{name: "valid", args: []string{"sync", src, dst}, wantCode: 0},
		{name: "valid with dry run", args: []string{"sync", src, dst, "--dry-run"}, wantCode: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(tt.args, &stdout, &stderr)
			if code != tt.wantCode {
				t.Errorf("exit code = %d, want %d (stderr: %q)", code, tt.wantCode, stderr.String())
			}
			if !strings.Contains(stderr.String(), tt.wantStderr) {
				t.Errorf("stderr = %q, want it to contain %q", stderr.String(), tt.wantStderr)
			}
			if tt.wantCode == 0 && stderr.Len() != 0 {
				t.Errorf("unexpected stderr: %q", stderr.String())
			}
		})
	}
}

func TestRunOutput(t *testing.T) {
	root := t.TempDir()
	chdir(t, root)
	src := mkdir(t, root, "src")
	dst := mkdir(t, root, "dst")
	writeFile(t, filepath.Join(src, "a.txt"), "a")
	writeFile(t, filepath.Join(dst, "old.txt"), "old")

	steps := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "dry run",
			args: []string{"sync", "--dry-run", src, dst},
			want: "Dry run:\n\n  + a.txt\n  - old.txt\n\nNo changes were made.\n",
		},
		{
			name: "dry run again gives the same result",
			args: []string{"sync", "--dry-run", src, dst},
			want: "Dry run:\n\n  + a.txt\n  - old.txt\n\nNo changes were made.\n",
		},
		{
			name: "sync",
			args: []string{"sync", src, dst},
			want: "Syncing:\n  + a.txt\n  - old.txt\n\nSync completed.\n",
		},
		{
			name: "already synchronized",
			args: []string{"sync", src, dst},
			want: "Already synchronized.\n",
		},
	}

	for _, step := range steps {
		var stdout, stderr bytes.Buffer
		if code := run(step.args, &stdout, &stderr); code != 0 {
			t.Fatalf("%s: exit code %d, stderr: %q", step.name, code, stderr.String())
		}
		if stdout.String() != step.want {
			t.Errorf("%s: stdout = %q, want %q", step.name, stdout.String(), step.want)
		}
	}

	assertTree(t, "destination", snapshot(t, dst), map[string]string{"a.txt": "a"})
}

func build(t *testing.T, root string, tree map[string]string) {
	t.Helper()
	if tree == nil {
		return
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range tree {
		full := filepath.Join(root, filepath.FromSlash(name))
		if strings.HasSuffix(name, "/") {
			if err := os.MkdirAll(full, 0o755); err != nil {
				t.Fatal(err)
			}
			continue
		}
		writeFile(t, full, content)
	}
}

func snapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	if _, err := os.Lstat(root); errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	tree := make(map[string]string)
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			tree[rel+"/"] = ""
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		tree[rel] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

func assertTree(t *testing.T, what string, got, want map[string]string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s:\n got  %v\n want %v", what, summarize(got), summarize(want))
	}
}

func summarize(tree map[string]string) map[string]string {
	if tree == nil {
		return nil
	}
	out := make(map[string]string, len(tree))
	for k, v := range tree {
		if len(v) > 20 {
			v = v[:20] + "..."
		}
		out[k] = v
	}
	return out
}

func assertChanges(t *testing.T, what string, got []change, want []string) {
	t.Helper()
	var gotStrings []string
	for _, c := range got {
		gotStrings = append(gotStrings, c.String())
	}
	if !reflect.DeepEqual(gotStrings, want) {
		t.Errorf("%s changes = %v, want %v", what, gotStrings, want)
	}
}

func mkdir(t *testing.T, parent, name string) string {
	t.Helper()
	p := filepath.Join(parent, name)
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func writeFile(t *testing.T, p, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func chdir(t *testing.T, dir string) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(old); err != nil {
			t.Error(err)
		}
	})
}

func checkErr(t *testing.T, err error, want string) {
	t.Helper()
	if want == "" {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		return
	}
	if err == nil {
		t.Fatalf("expected error containing %q, got nil", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %q, want it to contain %q", err.Error(), want)
	}
}
