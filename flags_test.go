package main

import (
	"bytes"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseSyncArgs(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		want    syncArgs
		wantErr string
	}{
		{
			name: "paths only",
			args: []string{"a", "b"},
			want: syncArgs{source: "a", destination: "b"},
		},
		{
			name: "long flags in any position",
			args: []string{"--not-sure", "a", "--prevent-delete", "b"},
			want: syncArgs{source: "a", destination: "b", notSure: true, preventDelete: true},
		},
		{
			name: "short flag names",
			args: []string{"a", "b", "--ns", "--pd"},
			want: syncArgs{source: "a", destination: "b", notSure: true, preventDelete: true},
		},
		{
			name: "skip is repeatable",
			args: []string{"-s", "main.py", "a", "--skip", "readme.md", "b"},
			want: syncArgs{source: "a", destination: "b", skip: []string{"main.py", "readme.md"}},
		},
		{
			name: "skip with equals sign",
			args: []string{"a", "b", "--skip=main.py"},
			want: syncArgs{source: "a", destination: "b", skip: []string{"main.py"}},
		},
		{
			name: "skip pattern is normalized",
			args: []string{"a", "b", "-s", "./docs/", "-s", "*.log"},
			want: syncArgs{source: "a", destination: "b", skip: []string{"docs", "*.log"}},
		},
		{name: "skip without value", args: []string{"a", "b", "-s"}, wantErr: "-s requires a value"},
		{name: "skip followed by flag", args: []string{"a", "b", "--skip", "--pd"}, wantErr: "--skip requires a value"},
		{name: "empty skip pattern", args: []string{"a", "b", "--skip="}, wantErr: "skip pattern is empty"},
		{name: "malformed skip pattern", args: []string{"a", "b", "-s", "[abc"}, wantErr: "invalid skip pattern"},
		{name: "unknown option", args: []string{"--force", "a", "b"}, wantErr: "unknown option: --force"},
		{name: "dry-run is gone", args: []string{"--dry-run", "a", "b"}, wantErr: "unknown option: --dry-run"},
		{name: "no arguments", args: nil, wantErr: "expected <source> and <destination>"},
		{name: "one path", args: []string{"a"}, wantErr: "expected <source> and <destination>"},
		{name: "three paths", args: []string{"a", "b", "c"}, wantErr: "expected <source> and <destination>"},
		{name: "skip takes exactly one value", args: []string{"-s", "x.txt", "y.txt", "a", "b"}, wantErr: "got 3 path(s)"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseSyncArgs(tt.args)
			checkErr(t, err, tt.wantErr)
			if tt.wantErr != "" {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parsed = %+v, want %+v", got, tt.want)
			}
		})
	}
}

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
		opts    options
		want    []string
		wantDst map[string]string
	}{
		{
			name:    "skip by name at any depth",
			src:     map[string]string{"main.py": "1", "docs/main.py": "1", "keep.txt": "k"},
			dst:     map[string]string{},
			opts:    options{skip: []string{"main.py"}},
			want:    []string{"+ docs/", "+ keep.txt"},
			wantDst: map[string]string{"docs/": "", "keep.txt": "k"},
		},
		{
			name:    "skip by relative path",
			src:     map[string]string{"main.py": "1", "docs/main.py": "1", "keep.txt": "k"},
			dst:     map[string]string{},
			opts:    options{skip: []string{"docs/main.py"}},
			want:    []string{"+ docs/", "+ keep.txt", "+ main.py"},
			wantDst: map[string]string{"docs/": "", "keep.txt": "k", "main.py": "1"},
		},
		{
			name:    "skip by glob",
			src:     map[string]string{"a.log": "new", "b.txt": "b"},
			dst:     map[string]string{"a.log": "old"},
			opts:    options{skip: []string{"*.log"}},
			want:    []string{"+ b.txt"},
			wantDst: map[string]string{"a.log": "old", "b.txt": "b"},
		},
		{
			name:    "skipped file is not removed from destination",
			src:     map[string]string{},
			dst:     map[string]string{"secret.txt": "s", "other.txt": "o"},
			opts:    options{skip: []string{"secret.txt"}},
			want:    []string{"- other.txt"},
			wantDst: map[string]string{"secret.txt": "s"},
		},
		{
			name:    "skipped directory is left untouched",
			src:     map[string]string{"node_modules/a.js": "new", "x.txt": "x"},
			dst:     map[string]string{"node_modules/a.js": "old", "node_modules/extra.js": "e"},
			opts:    options{skip: []string{"node_modules"}},
			want:    []string{"+ x.txt"},
			wantDst: map[string]string{"node_modules/": "", "node_modules/a.js": "old", "node_modules/extra.js": "e", "x.txt": "x"},
		},
		{
			name:    "prevent delete keeps extra entries but still creates and updates",
			src:     map[string]string{"a.txt": "new", "b.txt": "b"},
			dst:     map[string]string{"a.txt": "old", "extra.txt": "e", "dir/f.txt": "f"},
			opts:    options{preventDelete: true},
			want:    []string{"~ a.txt", "+ b.txt", "! dir/ (kept: not in source)", "! extra.txt (kept: not in source)"},
			wantDst: map[string]string{"a.txt": "new", "b.txt": "b", "extra.txt": "e", "dir/": "", "dir/f.txt": "f"},
		},
		{
			name:    "prevent delete refuses to replace a directory with a file",
			src:     map[string]string{"x": "file"},
			dst:     map[string]string{"x/inner.txt": "i"},
			opts:    options{preventDelete: true},
			want:    []string{"! x (" + needsDeleteNote + ")"},
			wantDst: map[string]string{"x/": "", "x/inner.txt": "i"},
		},
		{
			name:    "prevent delete refuses to replace a file with a directory",
			src:     map[string]string{"x/f.txt": "1"},
			dst:     map[string]string{"x": "file"},
			opts:    options{preventDelete: true},
			want:    []string{"! x/ (" + needsDeleteNote + ")"},
			wantDst: map[string]string{"x": "file"},
		},
		{
			name:    "prevent delete and skip together",
			src:     map[string]string{"a.txt": "a"},
			dst:     map[string]string{"keep.log": "k", "extra.txt": "e"},
			opts:    options{preventDelete: true, skip: []string{"*.log"}},
			want:    []string{"+ a.txt", "! extra.txt (kept: not in source)"},
			wantDst: map[string]string{"a.txt": "a", "keep.log": "k", "extra.txt": "e"},
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

			dry := tt.opts
			dry.dryRun = true
			changes, err := syncDirs(src, dst, dry)
			if err != nil {
				t.Fatalf("dry run: %v", err)
			}
			assertChanges(t, "dry run", changes, tt.want)
			assertTree(t, "destination after dry run", snapshot(t, dst), dstBefore)

			changes, err = syncDirs(src, dst, tt.opts)
			if err != nil {
				t.Fatalf("sync: %v", err)
			}
			assertChanges(t, "sync", changes, tt.want)
			assertTree(t, "source after sync", snapshot(t, src), srcBefore)
			assertTree(t, "destination after sync", snapshot(t, dst), tt.wantDst)
		})
	}
}

func scriptedConfirm(t *testing.T, answers ...bool) (func(change) bool, *[]string) {
	t.Helper()
	var asked []string
	return func(c change) bool {
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
			build(t, src, tt.src)
			build(t, dst, tt.dst)

			confirm, asked := scriptedConfirm(t, tt.answers...)
			changes, err := syncDirs(src, dst, options{confirm: confirm})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(*asked, tt.wantAsked) {
				t.Errorf("prompts = %v, want %v", *asked, tt.wantAsked)
			}
			assertChanges(t, "sync", changes, tt.want)
			assertTree(t, "destination", snapshot(t, dst), tt.wantDst)
		})
	}
}

func TestSyncConfirmIsNotAskedInDryRun(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	build(t, src, map[string]string{"a.txt": "a"})
	build(t, dst, map[string]string{})

	confirm, asked := scriptedConfirm(t)
	changes, err := syncDirs(src, dst, options{dryRun: true, confirm: confirm})
	if err != nil {
		t.Fatal(err)
	}
	assertChanges(t, "dry run", changes, []string{"+ a.txt"})
	if len(*asked) != 0 {
		t.Errorf("prompts in dry run: %v", *asked)
	}
}

func TestRunNotSure(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		input     string
		wantFiles map[string]string
	}{
		{
			name:      "y and Y apply, anything else skips",
			args:      []string{"sync", "--ns", "SRC", "DST"},
			input:     "y\nY\nyes\n\nn\n",
			wantFiles: map[string]string{"a.txt": "a", "b.txt": "b"},
		},
		{
			name:      "long flag name",
			args:      []string{"sync", "--not-sure", "SRC", "DST"},
			input:     "y\ny\ny\ny\ny\n",
			wantFiles: map[string]string{"a.txt": "a", "b.txt": "b", "c.txt": "c", "d.txt": "d", "e.txt": "e"},
		},
		{
			name:      "end of input skips everything",
			args:      []string{"sync", "--ns", "SRC", "DST"},
			input:     "",
			wantFiles: map[string]string{},
		},
		{
			name:      "answer without trailing newline",
			args:      []string{"sync", "SRC", "DST", "--ns"},
			input:     "y",
			wantFiles: map[string]string{"a.txt": "a"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			chdir(t, root)
			src := filepath.Join(root, "src")
			dst := filepath.Join(root, "dst")
			build(t, src, map[string]string{"a.txt": "a", "b.txt": "b", "c.txt": "c", "d.txt": "d", "e.txt": "e"})
			build(t, dst, map[string]string{})

			args := replaceAll(tt.args, "SRC", src, "DST", dst)
			var stdout, stderr bytes.Buffer
			if code := run(args, strings.NewReader(tt.input), &stdout, &stderr); code != 0 {
				t.Fatalf("exit code %d, stderr: %q", code, stderr.String())
			}

			assertTree(t, "destination", snapshot(t, dst), tt.wantFiles)
			if !strings.Contains(stdout.String(), "+ a.txt  (missing in destination)  apply? [y/N] ") {
				t.Errorf("prompt not shown, stdout = %q", stdout.String())
			}
			if !strings.Contains(stdout.String(), "Sync completed.") {
				t.Errorf("report not shown, stdout = %q", stdout.String())
			}
		})
	}
}

func TestRunCheckRejectsNotSure(t *testing.T) {
	root := t.TempDir()
	chdir(t, root)
	src := mkdir(t, root, "src")
	dst := mkdir(t, root, "dst")

	var stdout, stderr bytes.Buffer
	code := run([]string{"check", "--ns", src, dst}, nil, &stdout, &stderr)
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	if want := "Error: --not-sure cannot be used with check"; !strings.Contains(stderr.String(), want) {
		t.Errorf("stderr = %q, want it to contain %q", stderr.String(), want)
	}
}

func TestRunSkipAndPreventDelete(t *testing.T) {
	root := t.TempDir()
	chdir(t, root)
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	build(t, src, map[string]string{"main.py": "m", "readme.md": "r", "x.txt": "x"})
	build(t, dst, map[string]string{"old.txt": "o"})
	header := "Checking " + src + " -> " + dst + "\n\n"

	var stdout, stderr bytes.Buffer
	args := []string{"check", src, dst, "-s", "main.py", "--skip=readme.md", "--pd"}
	if code := run(args, nil, &stdout, &stderr); code != 0 {
		t.Fatalf("check: exit code %d, stderr: %q", code, stderr.String())
	}
	want := header +
		"  + x.txt    missing in destination\n" +
		"  ! old.txt  kept: not in source\n" +
		"\nSummary: 1 to create, 1 skipped.\n" +
		"No changes were made.\n"
	if stdout.String() != want {
		t.Errorf("check output = %q, want %q", stdout.String(), want)
	}
	assertTree(t, "destination after check", snapshot(t, dst), map[string]string{"old.txt": "o"})

	stdout.Reset()
	args = []string{"sync", "-s", "main.py", "-s", "readme.md", "--prevent-delete", src, dst}
	if code := run(args, nil, &stdout, &stderr); code != 0 {
		t.Fatalf("sync: exit code %d, stderr: %q", code, stderr.String())
	}
	assertTree(t, "destination after sync", snapshot(t, dst), map[string]string{"old.txt": "o", "x.txt": "x"})
}

func TestRunSkipTakesOneValue(t *testing.T) {
	root := t.TempDir()
	chdir(t, root)

	var stdout, stderr bytes.Buffer
	code := run([]string{"sync", "-s", "main.py", "readme.md", "src", "dst"}, nil, &stdout, &stderr)
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	if want := "got 3 path(s)"; !strings.Contains(stderr.String(), want) {
		t.Errorf("stderr = %q, want it to contain %q", stderr.String(), want)
	}
}

func replaceAll(args []string, pairs ...string) []string {
	out := make([]string, len(args))
	for i, arg := range args {
		out[i] = arg
		for j := 0; j+1 < len(pairs); j += 2 {
			if arg == pairs[j] {
				out[i] = pairs[j+1]
			}
		}
	}
	return out
}
