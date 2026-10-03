package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ssajaia/dios/internal/alias"
	"github.com/ssajaia/dios/internal/testutil"
)

func TestRun(t *testing.T) {
	root := t.TempDir()
	testutil.Chdir(t, root)
	src := testutil.Mkdir(t, root, "src")
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
		{name: "sync no longer accepts dry-run", args: []string{"sync", "--dry-run", "a", "b"}, wantCode: 2, wantStderr: "Error: unknown option: --dry-run"},
		{name: "check wrong argument count", args: []string{"check", "a"}, wantCode: 2, wantStderr: "Error: expected"},
		{name: "check missing source", args: []string{"check", missing, dst}, wantCode: 1, wantStderr: "Error: source does not exist: " + missing},
		{name: "missing source", args: []string{"sync", missing, dst}, wantCode: 1, wantStderr: "Error: source does not exist: " + missing},
		{name: "valid", args: []string{"sync", src, dst}, wantCode: 0},
		{name: "valid check", args: []string{"check", src, dst}, wantCode: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(tt.args, nil, &stdout, &stderr)
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
	testutil.Chdir(t, root)
	src := testutil.Mkdir(t, root, "src")
	dst := testutil.Mkdir(t, root, "dst")
	testutil.WriteFile(t, filepath.Join(src, "a.txt"), "a")
	testutil.WriteFile(t, filepath.Join(dst, "old.txt"), "old")

	header := "Checking " + src + " -> " + dst + "\n\n"
	checkWant := header +
		"  + a.txt    missing in destination\n" +
		"  - old.txt  not in source\n" +
		"\nSummary: 1 to create, 1 to remove.\n" +
		"No changes were made.\n"

	steps := []struct {
		name     string
		args     []string
		want     string
		wantCode int
	}{
		{name: "check", args: []string{"check", src, dst}, want: checkWant, wantCode: 1},
		{name: "sync", args: []string{"sync", src, dst}, want: "Syncing:\n  + a.txt\n  - old.txt\n\nSync completed.\n"},
		{name: "already synchronized", args: []string{"sync", src, dst}, want: "Already synchronized.\n"},
		{name: "check when synchronized", args: []string{"check", src, dst}, want: header + "Already synchronized.\n"},
	}

	for _, step := range steps {
		var stdout, stderr bytes.Buffer
		if code := run(step.args, nil, &stdout, &stderr); code != step.wantCode {
			t.Fatalf("%s: exit code %d, want %d (stderr: %q)", step.name, code, step.wantCode, stderr.String())
		}
		if stdout.String() != step.want {
			t.Errorf("%s: stdout = %q, want %q", step.name, stdout.String(), step.want)
		}
		if step.name == "check" {
			testutil.AssertTree(t, step.name+": destination", testutil.Snapshot(t, dst), map[string]string{"old.txt": "old"})
		}
	}

	testutil.AssertTree(t, "destination", testutil.Snapshot(t, dst), map[string]string{"a.txt": "a"})
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
			testutil.Chdir(t, root)
			src := filepath.Join(root, "src")
			dst := filepath.Join(root, "dst")
			testutil.Build(t, src, map[string]string{"a.txt": "a", "b.txt": "b", "c.txt": "c", "d.txt": "d", "e.txt": "e"})
			testutil.Build(t, dst, map[string]string{})

			args := append([]string(nil), tt.args...)
			for i, arg := range args {
				switch arg {
				case "SRC":
					args[i] = src
				case "DST":
					args[i] = dst
				}
			}
			var stdout, stderr bytes.Buffer
			if code := run(args, strings.NewReader(tt.input), &stdout, &stderr); code != 0 {
				t.Fatalf("exit code %d, stderr: %q", code, stderr.String())
			}

			testutil.AssertTree(t, "destination", testutil.Snapshot(t, dst), tt.wantFiles)
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
	testutil.Chdir(t, root)
	src := testutil.Mkdir(t, root, "src")
	dst := testutil.Mkdir(t, root, "dst")

	var stdout, stderr bytes.Buffer
	code := run([]string{"check", "--ns", src, dst}, nil, &stdout, &stderr)
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	if want := "Error: --not-sure cannot be used with check"; !strings.Contains(stderr.String(), want) {
		t.Errorf("stderr = %q, want it to contain %q", stderr.String(), want)
	}
}

func TestRunCheckExitCodes(t *testing.T) {
	root := t.TempDir()
	testutil.Chdir(t, root)
	src := testutil.Mkdir(t, root, "src")
	dst := filepath.Join(root, "dst")
	testutil.WriteFile(t, filepath.Join(src, "a.txt"), "a")

	var stdout, stderr bytes.Buffer
	if code := run([]string{"check", src, dst}, nil, &stdout, &stderr); code != 1 {
		t.Fatalf("check on missing destination: exit code = %d, want 1", code)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	if !strings.Contains(stdout.String(), "Summary: 2 to create") {
		t.Fatalf("stdout = %q, want summary to contain create", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"sync", src, dst}, nil, &stdout, &stderr); code != 0 {
		t.Fatalf("sync: exit code = %d, stderr: %q", code, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"check", src, dst}, nil, &stdout, &stderr); code != 0 {
		t.Fatalf("check on synchronized tree: exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout.String(), "Already synchronized.") {
		t.Fatalf("stdout = %q, want synchronized message", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"check", "a"}, nil, &stdout, &stderr); code != 2 {
		t.Fatalf("check usage error: exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "Error: expected <source> and <destination>") {
		t.Fatalf("stderr = %q, want usage error", stderr.String())
	}
}

func TestRunSkipAndPreventDelete(t *testing.T) {
	root := t.TempDir()
	testutil.Chdir(t, root)
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	testutil.Build(t, src, map[string]string{"main.py": "m", "readme.md": "r", "x.txt": "x"})
	testutil.Build(t, dst, map[string]string{"old.txt": "o"})
	header := "Checking " + src + " -> " + dst + "\n\n"

	var stdout, stderr bytes.Buffer
	args := []string{"check", src, dst, "-s", "main.py", "--skip=readme.md", "--pd"}
	if code := run(args, nil, &stdout, &stderr); code != 1 {
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
	testutil.AssertTree(t, "destination after check", testutil.Snapshot(t, dst), map[string]string{"old.txt": "o"})

	stdout.Reset()
	args = []string{"sync", "-s", "main.py", "-s", "readme.md", "--prevent-delete", src, dst}
	if code := run(args, nil, &stdout, &stderr); code != 0 {
		t.Fatalf("sync: exit code %d, stderr: %q", code, stderr.String())
	}
	testutil.AssertTree(t, "destination after sync", testutil.Snapshot(t, dst), map[string]string{"old.txt": "o", "x.txt": "x"})
}

func TestRunSkipTakesOneValue(t *testing.T) {
	root := t.TempDir()
	testutil.Chdir(t, root)

	var stdout, stderr bytes.Buffer
	code := run([]string{"sync", "-s", "main.py", "readme.md", "src", "dst"}, nil, &stdout, &stderr)
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	if want := "got 3 path(s)"; !strings.Contains(stderr.String(), want) {
		t.Errorf("stderr = %q, want it to contain %q", stderr.String(), want)
	}
}

func TestRunHelpAndVersion(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantCode int
		wantText string
	}{
		{name: "general help", args: []string{"--help"}, wantCode: 0, wantText: "Dios synchronizes one directory into another"},
		{name: "short help", args: []string{"-h"}, wantCode: 0, wantText: "Usage:"},
		{name: "help command", args: []string{"help", "sync"}, wantCode: 0, wantText: "usage: dios sync"},
		{name: "sync help", args: []string{"sync", "--help"}, wantCode: 0, wantText: "usage: dios sync"},
		{name: "check help", args: []string{"check", "--help"}, wantCode: 0, wantText: "usage: dios check"},
		{name: "init help", args: []string{"init", "--help"}, wantCode: 0, wantText: ".config/aliases"},
		{name: "version flag", args: []string{"--version"}, wantCode: 0, wantText: version + "\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(tt.args, nil, &stdout, &stderr)
			if code != tt.wantCode {
				t.Fatalf("exit code = %d, want %d; stderr = %q", code, tt.wantCode, stderr.String())
			}
			if !strings.Contains(stdout.String(), tt.wantText) {
				t.Fatalf("stdout = %q, want it to contain %q", stdout.String(), tt.wantText)
			}
			if stderr.Len() != 0 {
				t.Fatalf("stderr = %q, want empty", stderr.String())
			}
		})
	}
}

func TestRunWithAliases(t *testing.T) {
	root := t.TempDir()
	testutil.Chdir(t, root)
	src := testutil.Mkdir(t, root, "real-src")
	dst := filepath.Join(root, "real-dst")
	testutil.WriteFile(t, filepath.Join(src, "a.txt"), "a")
	testutil.WriteFile(t, filepath.Join(root, ".config", "aliases"),
		fmt.Sprintf("# aliases\n\none = %q\ntwo = %q\n", src, dst))

	var stdout, stderr bytes.Buffer

	if code := run([]string{"check", "one", "two"}, nil, &stdout, &stderr); code != 1 {
		t.Fatalf("check: exit code %d, stderr: %q", code, stderr.String())
	}
	testutil.AssertTree(t, "destination after check", testutil.Snapshot(t, dst), nil)

	stdout.Reset()
	if code := run([]string{"sync", "one", "two"}, nil, &stdout, &stderr); code != 0 {
		t.Fatalf("sync: exit code %d, stderr: %q", code, stderr.String())
	}
	testutil.AssertTree(t, "destination after sync", testutil.Snapshot(t, dst), map[string]string{"a.txt": "a"})
}

func TestRunAliasWithSeparatorIsNotResolved(t *testing.T) {
	root := t.TempDir()
	testutil.Chdir(t, root)
	testutil.WriteFile(t, filepath.Join(root, ".config", "aliases"), fmt.Sprintf("one = %q\n", testutil.Mkdir(t, root, "real")))

	var stdout, stderr bytes.Buffer
	code := run([]string{"sync", "./one", "dst"}, nil, &stdout, &stderr)
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if want := "Error: source does not exist: ./one"; !bytes.Contains(stderr.Bytes(), []byte(want)) {
		t.Errorf("stderr = %q, want it to contain %q", stderr.String(), want)
	}
}

func TestRunWithInvalidAliasFile(t *testing.T) {
	root := t.TempDir()
	testutil.Chdir(t, root)
	testutil.WriteFile(t, filepath.Join(root, ".config", "aliases"), "broken line\n")

	var stdout, stderr bytes.Buffer
	code := run([]string{"sync", "a", "b"}, nil, &stdout, &stderr)
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if want := "aliases:1: invalid alias definition"; !bytes.Contains(stderr.Bytes(), []byte(want)) {
		t.Errorf("stderr = %q, want it to contain %q", stderr.String(), want)
	}
}

func TestRunInit(t *testing.T) {
	root := t.TempDir()
	testutil.Chdir(t, root)

	var stdout, stderr bytes.Buffer
	if code := run([]string{"init"}, nil, &stdout, &stderr); code != 0 {
		t.Fatalf("exit code = %d, stderr: %q", code, stderr.String())
	}
	if want := "Created " + aliasFile + "\n"; stdout.String() != want {
		t.Errorf("stdout = %q, want %q", stdout.String(), want)
	}
	got, err := os.ReadFile(aliasFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != alias.Template {
		t.Errorf("file content = %q, want template", got)
	}

	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"init"}, nil, &stdout, &stderr); code != 1 {
		t.Errorf("second init: exit code = %d, want 1", code)
	}
	if want := "Error: already initialized: " + aliasFile + " exists"; !bytes.Contains(stderr.Bytes(), []byte(want)) {
		t.Errorf("second init: stderr = %q, want it to contain %q", stderr.String(), want)
	}
	if stdout.Len() != 0 {
		t.Errorf("second init: unexpected stdout %q", stdout.String())
	}
}

func TestRunInitWithArguments(t *testing.T) {
	root := t.TempDir()
	testutil.Chdir(t, root)

	var stdout, stderr bytes.Buffer
	if code := run([]string{"init", "extra"}, nil, &stdout, &stderr); code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	if want := "Error: init takes no arguments"; !bytes.Contains(stderr.Bytes(), []byte(want)) {
		t.Errorf("stderr = %q, want it to contain %q", stderr.String(), want)
	}
	if _, err := os.Stat(aliasFile); err == nil {
		t.Error("alias file was created despite invalid arguments")
	}
}

func TestInitThenUseAlias(t *testing.T) {
	root := t.TempDir()
	testutil.Chdir(t, root)
	src := testutil.Mkdir(t, root, "real-src")
	dst := filepath.Join(root, "real-dst")
	testutil.WriteFile(t, filepath.Join(src, "a.txt"), "a")

	var stdout, stderr bytes.Buffer
	if code := run([]string{"init"}, nil, &stdout, &stderr); code != 0 {
		t.Fatalf("init: exit code %d, stderr: %q", code, stderr.String())
	}

	content := alias.Template + fmt.Sprintf("one = %q\ntwo = %q\n", src, dst)
	if err := os.WriteFile(aliasFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	if code := run([]string{"sync", "one", "two"}, nil, &stdout, &stderr); code != 0 {
		t.Fatalf("sync: exit code %d, stderr: %q", code, stderr.String())
	}
	testutil.AssertTree(t, "destination", testutil.Snapshot(t, dst), map[string]string{"a.txt": "a"})
}
