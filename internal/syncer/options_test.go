package syncer

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ssajaia/dios/internal/testutil"
)

func TestMatchesSkip(t *testing.T) {
	tests := []struct {
		patterns []string
		rel      string
		want     bool
	}{
		{[]string{"main.py"}, "main.py", true},
		{[]string{"main.py"}, "docs/main.py", true},
		{[]string{"main.py"}, "docs/main.pyc", false},
		{[]string{"docs/main.py"}, "docs/main.py", true},
		{[]string{"docs/main.py"}, "main.py", false},
		{[]string{"docs/main.py"}, "other/docs/main.py", false},
		{[]string{"*.log"}, "a/b/app.log", true},
		{[]string{"*.log"}, "a/b/app.txt", false},
		{[]string{"docs/*.md"}, "docs/a.md", true},
		{[]string{"docs/*.md"}, "docs/sub/a.md", false},
		{[]string{"node_modules"}, "node_modules", true},
		{nil, "anything", false},
	}

	for _, tt := range tests {
		if got := matchesSkip(tt.patterns, tt.rel); got != tt.want {
			t.Errorf("matchesSkip(%v, %q) = %v, want %v", tt.patterns, tt.rel, got, tt.want)
		}
	}
}

func TestSyncOptions(t *testing.T) {
	tests := []struct {
		name    string
		src     map[string]string
		dst     map[string]string
		opts    Options
		want    []string
		wantDst map[string]string
	}{
		{
			name:    "skip by name at any depth",
			src:     map[string]string{"main.py": "1", "docs/main.py": "1", "keep.txt": "k"},
			dst:     map[string]string{},
			opts:    Options{Skip: []string{"main.py"}},
			want:    []string{"+ docs/", "+ keep.txt"},
			wantDst: map[string]string{"docs/": "", "keep.txt": "k"},
		},
		{
			name:    "skip by relative path",
			src:     map[string]string{"main.py": "1", "docs/main.py": "1", "keep.txt": "k"},
			dst:     map[string]string{},
			opts:    Options{Skip: []string{"docs/main.py"}},
			want:    []string{"+ docs/", "+ keep.txt", "+ main.py"},
			wantDst: map[string]string{"docs/": "", "keep.txt": "k", "main.py": "1"},
		},
		{
			name:    "skip by glob",
			src:     map[string]string{"a.log": "new", "b.txt": "b"},
			dst:     map[string]string{"a.log": "old"},
			opts:    Options{Skip: []string{"*.log"}},
			want:    []string{"+ b.txt"},
			wantDst: map[string]string{"a.log": "old", "b.txt": "b"},
		},
		{
			name:    "skipped file is not removed from destination",
			src:     map[string]string{},
			dst:     map[string]string{"secret.txt": "s", "other.txt": "o"},
			opts:    Options{Skip: []string{"secret.txt"}},
			want:    []string{"- other.txt"},
			wantDst: map[string]string{"secret.txt": "s"},
		},
		{
			name:    "skipped directory is left untouched",
			src:     map[string]string{"node_modules/a.js": "new", "x.txt": "x"},
			dst:     map[string]string{"node_modules/a.js": "old", "node_modules/extra.js": "e"},
			opts:    Options{Skip: []string{"node_modules"}},
			want:    []string{"+ x.txt"},
			wantDst: map[string]string{"node_modules/": "", "node_modules/a.js": "old", "node_modules/extra.js": "e", "x.txt": "x"},
		},
		{
			name:    "prevent delete keeps extra entries but still creates and updates",
			src:     map[string]string{"a.txt": "new", "b.txt": "b"},
			dst:     map[string]string{"a.txt": "old", "extra.txt": "e", "dir/f.txt": "f"},
			opts:    Options{PreventDelete: true},
			want:    []string{"~ a.txt", "+ b.txt", "! dir/ (kept: not in source)", "! extra.txt (kept: not in source)"},
			wantDst: map[string]string{"a.txt": "new", "b.txt": "b", "extra.txt": "e", "dir/": "", "dir/f.txt": "f"},
		},
		{
			name:    "prevent delete refuses to replace a directory with a file",
			src:     map[string]string{"x": "file"},
			dst:     map[string]string{"x/inner.txt": "i"},
			opts:    Options{PreventDelete: true},
			want:    []string{"! x (" + needsDeleteNote + ")"},
			wantDst: map[string]string{"x/": "", "x/inner.txt": "i"},
		},
		{
			name:    "prevent delete refuses to replace a file with a directory",
			src:     map[string]string{"x/f.txt": "1"},
			dst:     map[string]string{"x": "file"},
			opts:    Options{PreventDelete: true},
			want:    []string{"! x/ (" + needsDeleteNote + ")"},
			wantDst: map[string]string{"x": "file"},
		},
		{
			name:    "prevent delete and skip together",
			src:     map[string]string{"a.txt": "a"},
			dst:     map[string]string{"keep.log": "k", "extra.txt": "e"},
			opts:    Options{PreventDelete: true, Skip: []string{"*.log"}},
			want:    []string{"+ a.txt", "! extra.txt (kept: not in source)"},
			wantDst: map[string]string{"a.txt": "a", "keep.log": "k", "extra.txt": "e"},
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

			dry := tt.opts
			dry.DryRun = true
			changes, err := Sync(src, dst, dry)
			if err != nil {
				t.Fatalf("dry run: %v", err)
			}
			assertChanges(t, "dry run", changes, tt.want)
			testutil.AssertTree(t, "destination after dry run", testutil.Snapshot(t, dst), dstBefore)

			changes, err = Sync(src, dst, tt.opts)
			if err != nil {
				t.Fatalf("sync: %v", err)
			}
			assertChanges(t, "sync", changes, tt.want)
			testutil.AssertTree(t, "source after sync", testutil.Snapshot(t, src), srcBefore)
			testutil.AssertTree(t, "destination after sync", testutil.Snapshot(t, dst), tt.wantDst)
		})
	}
}

func scriptedConfirm(t *testing.T, answers ...bool) (func(Change) bool, *[]string) {
	t.Helper()
	var asked []string
	return func(c Change) bool {
		if len(asked) >= len(answers) {
			t.Fatalf("unexpected prompt for %s", c)
		}
		answer := answers[len(asked)]
		asked = append(asked, c.String())
		return answer
	}, &asked
}

func TestSyncConfirm(t *testing.T) {
	tests := []struct {
		name      string
		src       map[string]string
		dst       map[string]string
		answers   []bool
		wantAsked []string
		want      []string
		wantDst   map[string]string
	}{
		{
			name:      "each change is confirmed separately",
			src:       map[string]string{"a.txt": "a", "b.txt": "b", "c.txt": "new"},
			dst:       map[string]string{"c.txt": "old", "old.txt": "o"},
			answers:   []bool{true, false, true, false},
			wantAsked: []string{"+ a.txt", "+ b.txt", "~ c.txt", "- old.txt"},
			want:      []string{"+ a.txt", "! b.txt (skipped by user)", "~ c.txt", "! old.txt (skipped by user)"},
			wantDst:   map[string]string{"a.txt": "a", "c.txt": "new", "old.txt": "o"},
		},
		{
			name:      "declining a directory skips everything inside it",
			src:       map[string]string{"d/f.txt": "1", "d/g/h.txt": "2"},
			dst:       map[string]string{},
			answers:   []bool{false},
			wantAsked: []string{"+ d/"},
			want:      []string{"! d/ (skipped by user)"},
			wantDst:   map[string]string{},
		},
		{
			name:      "accepting a directory asks about its contents",
			src:       map[string]string{"d/f.txt": "1"},
			dst:       map[string]string{},
			answers:   []bool{true, false},
			wantAsked: []string{"+ d/", "+ d/f.txt"},
			want:      []string{"+ d/", "! d/f.txt (skipped by user)"},
			wantDst:   map[string]string{"d/": ""},
		},
		{
			name:      "declining a replacement leaves the destination entry",
			src:       map[string]string{"x": "file"},
			dst:       map[string]string{"x/inner.txt": "i"},
			answers:   []bool{false},
			wantAsked: []string{"~ x"},
			want:      []string{"! x (skipped by user)"},
			wantDst:   map[string]string{"x/": "", "x/inner.txt": "i"},
		},
		{
			name:      "declining destination creation stops the sync",
			src:       map[string]string{"a.txt": "a"},
			dst:       nil,
			answers:   []bool{false},
			wantAsked: []string{"+ ./"},
			want:      []string{"! ./ (skipped by user)"},
			wantDst:   nil,
		},
		{
			name:      "nothing to confirm when synchronized",
			src:       map[string]string{"a.txt": "a"},
			dst:       map[string]string{"a.txt": "a"},
			answers:   nil,
			wantAsked: nil,
			want:      nil,
			wantDst:   map[string]string{"a.txt": "a"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			src := filepath.Join(root, "src")
			dst := filepath.Join(root, "dst")
			testutil.Build(t, src, tt.src)
			testutil.Build(t, dst, tt.dst)

			confirm, asked := scriptedConfirm(t, tt.answers...)
			changes, err := Sync(src, dst, Options{Confirm: confirm})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(*asked, tt.wantAsked) {
				t.Errorf("prompts = %v, want %v", *asked, tt.wantAsked)
			}
			assertChanges(t, "sync", changes, tt.want)
			testutil.AssertTree(t, "destination", testutil.Snapshot(t, dst), tt.wantDst)
		})
	}
}

func TestSyncConfirmIsNotAskedInDryRun(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	testutil.Build(t, src, map[string]string{"a.txt": "a"})
	testutil.Build(t, dst, map[string]string{})

	confirm, asked := scriptedConfirm(t)
	changes, err := Sync(src, dst, Options{DryRun: true, Confirm: confirm})
	if err != nil {
		t.Fatal(err)
	}
	assertChanges(t, "dry run", changes, []string{"+ a.txt"})
	if len(*asked) != 0 {
		t.Errorf("prompts in dry run: %v", *asked)
	}
}
