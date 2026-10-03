package syncer

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ssajaia/dios/internal/testutil"
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
			testutil.Build(t, src, tt.src)
			testutil.Build(t, dst, tt.dst)
			srcBefore := testutil.Snapshot(t, src)
			dstBefore := testutil.Snapshot(t, dst)

			changes, err := Sync(src, dst, Options{DryRun: true})
			if err != nil {
				t.Fatalf("dry run: %v", err)
			}
			assertChanges(t, "dry run", changes, tt.want)
			testutil.AssertTree(t, "source after dry run", testutil.Snapshot(t, src), srcBefore)
			testutil.AssertTree(t, "destination after dry run", testutil.Snapshot(t, dst), dstBefore)

			changes, err = Sync(src, dst, Options{})
			if err != nil {
				t.Fatalf("sync: %v", err)
			}
			assertChanges(t, "sync", changes, tt.want)
			testutil.AssertTree(t, "source after sync", testutil.Snapshot(t, src), srcBefore)
			testutil.AssertTree(t, "destination after sync", testutil.Snapshot(t, dst), srcBefore)

			changes, err = Sync(src, dst, Options{})
			if err != nil {
				t.Fatalf("second sync: %v", err)
			}
			assertChanges(t, "second sync", changes, nil)
		})
	}
}

func TestSyncRemovesStaleTempFiles(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	testutil.Build(t, src, map[string]string{"a.txt": "new"})
	testutil.Build(t, dst, map[string]string{"a.txt": "old"})

	stale := filepath.Join(dst, ".dios-tmp-stale")
	if err := os.WriteFile(stale, []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := Sync(src, dst, Options{}); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if _, err := os.Stat(stale); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("stale temp file remained at %s: %v", stale, err)
	}
}

func TestCleanupTrackedTempFiles(t *testing.T) {
	dir := t.TempDir()
	tmp := filepath.Join(dir, ".dios-tmp-123")
	if err := os.WriteFile(tmp, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	registerTempFile(tmp)
	cleanupTrackedTempFiles()
	if _, err := os.Stat(tmp); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("tracked temp file still exists: %v", err)
	}
}

func TestCheckDirectoryCaseSafetyRejectsCaseCollisions(t *testing.T) {
	root := t.TempDir()
	if isCaseInsensitiveFS(root) {
		t.Skip("case-insensitive filesystem; collision cannot be represented")
	}

	src := filepath.Join(root, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"A.txt", "a.txt"} {
		if err := os.WriteFile(filepath.Join(src, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := checkDirectoryCaseSafety(src); err == nil {
		t.Fatal("expected case-insensitive collision detection")
	}
}

func TestIsCaseInsensitiveFSWithMissingPath(t *testing.T) {
	root := t.TempDir()
	want := isCaseInsensitiveFS(root)
	got := isCaseInsensitiveFS(filepath.Join(root, "not-created", "nested"))
	if got != want {
		t.Fatalf("case-insensitivity for missing path = %v, want %v", got, want)
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
				testutil.WriteFile(t, src, "x")
				return src, filepath.Join(root, "dst")
			},
			wantErr: "source is not a directory",
		},
		{
			name: "destination is a file",
			setup: func(t *testing.T, root string) (string, string) {
				dst := filepath.Join(root, "dst.txt")
				testutil.WriteFile(t, dst, "x")
				return testutil.Mkdir(t, root, "src"), dst
			},
			wantErr: "destination is not a directory",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src, dst := tt.setup(t, t.TempDir())
			changes, err := Sync(src, dst, Options{})
			testutil.CheckErr(t, err, tt.wantErr)
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
	testutil.Build(t, src, map[string]string{"a.txt": "data"})

	mtime := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	file := filepath.Join(src, "a.txt")
	if err := os.Chmod(file, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(file, mtime, mtime); err != nil {
		t.Fatal(err)
	}

	if _, err := Sync(src, dst, Options{}); err != nil {
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
	testutil.Build(t, src, map[string]string{"a.txt": "new", "d/b.txt": "b"})
	testutil.Build(t, dst, map[string]string{"a.txt": "old"})

	if _, err := Sync(src, dst, Options{}); err != nil {
		t.Fatal(err)
	}
	for name := range testutil.Snapshot(t, dst) {
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
	testutil.WriteFile(t, outside, "outside")
	testutil.Build(t, src, map[string]string{"a.txt": "a"})
	if err := os.Symlink(outside, filepath.Join(src, "link")); err != nil {
		t.Skipf("symlinks not supported: %v", err)
	}

	changes, err := Sync(src, dst, Options{})
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
	src := testutil.Mkdir(t, root, "src")
	dst := testutil.Mkdir(t, root, "dst")
	outside := testutil.Mkdir(t, root, "outside")
	keep := filepath.Join(outside, "keep.txt")
	testutil.WriteFile(t, keep, "keep")
	if err := os.Symlink(outside, filepath.Join(dst, "link")); err != nil {
		t.Skipf("symlinks not supported: %v", err)
	}

	changes, err := Sync(src, dst, Options{})
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

func TestSyncRejectsNestingThroughSymlinks(t *testing.T) {
	t.Run("destination nested through symlinked parent", func(t *testing.T) {
		root := t.TempDir()
		src := filepath.Join(root, "src")
		if err := os.MkdirAll(src, 0o755); err != nil {
			t.Fatal(err)
		}
		sourceFile := filepath.Join(src, "keep.txt")
		if err := os.WriteFile(sourceFile, []byte("keep"), 0o644); err != nil {
			t.Fatal(err)
		}
		sourceAlias := filepath.Join(root, "source-alias")
		if err := os.Symlink(src, sourceAlias); err != nil {
			t.Skipf("directory symlinks not supported: %v", err)
		}
		dst := filepath.Join(sourceAlias, "nested-destination")

		_, err := Sync(src, dst, Options{})
		testutil.CheckErr(t, err, "destination must not be inside source")
		if _, err := os.Lstat(dst); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("destination was created: %v", err)
		}
		if data, err := os.ReadFile(sourceFile); err != nil || string(data) != "keep" {
			t.Fatalf("source file changed: %q, %v", data, err)
		}
	})

	t.Run("source nested through symlinked parent", func(t *testing.T) {
		root := t.TempDir()
		dst := filepath.Join(root, "dst")
		if err := os.MkdirAll(dst, 0o755); err != nil {
			t.Fatal(err)
		}
		keep := filepath.Join(dst, "keep.txt")
		if err := os.WriteFile(keep, []byte("keep"), 0o644); err != nil {
			t.Fatal(err)
		}
		dstAlias := filepath.Join(root, "destination-alias")
		if err := os.Symlink(dst, dstAlias); err != nil {
			t.Skipf("directory symlinks not supported: %v", err)
		}
		src := filepath.Join(dstAlias, "nested-source")
		if err := os.MkdirAll(src, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(src, "new.txt"), []byte("new"), 0o644); err != nil {
			t.Fatal(err)
		}

		_, err := Sync(src, dst, Options{})
		testutil.CheckErr(t, err, "source must not be inside destination")
		if data, err := os.ReadFile(keep); err != nil || string(data) != "keep" {
			t.Fatalf("destination file changed: %q, %v", data, err)
		}
		if _, err := os.Stat(filepath.Join(dst, "new.txt")); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("destination was synchronized despite nesting: %v", err)
		}
	})
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
				testutil.WriteFile(t, src, "x")
				return src, filepath.Join(root, "dst")
			},
			wantErr: "source is not a directory",
		},
		{
			name: "same path",
			setup: func(t *testing.T, root string) (string, string) {
				src := testutil.Mkdir(t, root, "src")
				return src, src
			},
			wantErr: "must be different paths",
		},
		{
			name: "same path with different spelling",
			setup: func(t *testing.T, root string) (string, string) {
				src := testutil.Mkdir(t, root, "src")
				return src, src + sep + "."
			},
			wantErr: "must be different paths",
		},
		{
			name: "destination inside source",
			setup: func(t *testing.T, root string) (string, string) {
				src := testutil.Mkdir(t, root, "src")
				return src, filepath.Join(src, "dst")
			},
			wantErr: "destination must not be inside source",
		},
		{
			name: "source inside destination",
			setup: func(t *testing.T, root string) (string, string) {
				dst := testutil.Mkdir(t, root, "dst")
				src := testutil.Mkdir(t, dst, "src")
				return src, dst
			},
			wantErr: "source must not be inside destination",
		},
		{
			name: "destination is a file",
			setup: func(t *testing.T, root string) (string, string) {
				src := testutil.Mkdir(t, root, "src")
				dst := filepath.Join(root, "dst.txt")
				testutil.WriteFile(t, dst, "x")
				return src, dst
			},
			wantErr: "destination is not a directory",
		},
		{
			name: "missing destination",
			setup: func(t *testing.T, root string) (string, string) {
				return testutil.Mkdir(t, root, "src"), filepath.Join(root, "dst")
			},
		},
		{
			name: "existing destination",
			setup: func(t *testing.T, root string) (string, string) {
				return testutil.Mkdir(t, root, "src"), testutil.Mkdir(t, root, "dst")
			},
		},
		{
			name: "sibling directories with common prefix",
			setup: func(t *testing.T, root string) (string, string) {
				return testutil.Mkdir(t, root, "data"), testutil.Mkdir(t, root, "data-backup")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src, dst := tt.setup(t, t.TempDir())
			testutil.CheckErr(t, validatePaths(src, dst), tt.wantErr)
		})
	}
}

func TestValidatePathsSymlinkToSource(t *testing.T) {
	root := t.TempDir()
	src := testutil.Mkdir(t, root, "src")
	link := filepath.Join(root, "link")
	if err := os.Symlink(src, link); err != nil {
		t.Skipf("symlinks not supported: %v", err)
	}
	testutil.CheckErr(t, validatePaths(src, link), "must be different paths")
}

func TestCheckReasons(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	testutil.Build(t, src, map[string]string{
		"chg.txt":     "1",
		"dirx/f.txt":  "1",
		"new.txt":     "n",
		"t":           "file",
		"same.txt":    "s",
		"sub/inner/z": "z",
	})
	testutil.Build(t, dst, map[string]string{
		"chg.txt":     "2",
		"dirx":        "file",
		"t/x":         "x",
		"old.txt":     "o",
		"same.txt":    "s",
		"sub/inner/z": "z",
		"sub/gone/":   "",
	})

	changes, err := Sync(src, dst, Options{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}

	var got []string
	for _, c := range changes {
		got = append(got, fmt.Sprintf("%c %s: %s", c.Kind, c.Label(), c.Detail()))
	}
	want := []string{
		"~ chg.txt: contents differ",
		"~ dirx/: destination is a file, source is a directory",
		"+ dirx/f.txt: missing in destination",
		"+ new.txt: missing in destination",
		"- sub/gone/: not in source",
		"~ t: destination is a directory, source is a file",
		"- old.txt: not in source",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("reasons:\n got  %q\n want %q", got, want)
	}
}

func assertChanges(t *testing.T, what string, got []Change, want []string) {
	t.Helper()
	var gotStrings []string
	for _, c := range got {
		gotStrings = append(gotStrings, c.String())
	}
	if !reflect.DeepEqual(gotStrings, want) {
		t.Errorf("%s changes = %v, want %v", what, gotStrings, want)
	}
}
