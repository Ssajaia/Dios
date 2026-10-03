package main

import (
	"bytes"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLoadAliases(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    map[string]string
		wantErr string
	}{
		{
			name:    "single alias",
			content: `work = "C:/users/someone/blabla"` + "\n",
			want:    map[string]string{"work": "C:/users/someone/blabla"},
		},
		{
			name:    "comments, blank lines and spacing",
			content: "# my aliases\n\n  one=\"/a\"  \n\ttwo   =   \"/b\"\n",
			want:    map[string]string{"one": "/a", "two": "/b"},
		},
		{
			name:    "windows line endings and BOM",
			content: "\ufeffone = \"/a\"\r\ntwo = \"/b\"\r\n",
			want:    map[string]string{"one": "/a", "two": "/b"},
		},
		{
			name:    "backtick value keeps backslashes",
			content: "work = `C:\\users\\someone`\n",
			want:    map[string]string{"work": `C:\users\someone`},
		},
		{
			name:    "value containing equals sign",
			content: `work = "/a=b"` + "\n",
			want:    map[string]string{"work": "/a=b"},
		},
		{
			name:    "empty file",
			content: "",
			want:    map[string]string{},
		},
		{
			name:    "missing equals sign",
			content: "# ok\nwork\n",
			wantErr: ":2: invalid alias definition",
		},
		{
			name:    "unquoted value",
			content: "work = /a/b\n",
			wantErr: ":1: alias path must be quoted",
		},
		{
			name:    "backslashes in double quotes",
			content: `work = "C:\users\x"` + "\n",
			wantErr: ":1: alias path must be quoted",
		},
		{
			name:    "empty value",
			content: `work = ""` + "\n",
			wantErr: ":1: alias path is empty: work",
		},
		{
			name:    "empty name",
			content: `= "/a"` + "\n",
			wantErr: ":1: invalid alias name",
		},
		{
			name:    "name with slash",
			content: `a/b = "/a"` + "\n",
			wantErr: ":1: invalid alias name",
		},
		{
			name:    "name starting with dash",
			content: `-x = "/a"` + "\n",
			wantErr: ":1: invalid alias name",
		},
		{
			name:    "name with space",
			content: `my work = "/a"` + "\n",
			wantErr: ":1: invalid alias name",
		},
		{
			name:    "duplicate name",
			content: "one = \"/a\"\ntwo = \"/b\"\none = \"/c\"\n",
			wantErr: ":3: duplicate alias: one",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "aliases")
			writeFile(t, file, tt.content)

			got, err := loadAliases(file)
			checkErr(t, err, tt.wantErr)
			if tt.wantErr != "" {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("aliases = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLoadAliasesMissingFile(t *testing.T) {
	got, err := loadAliases(filepath.Join(t.TempDir(), "nope"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("aliases = %v, want empty", got)
	}
}

func TestLoadAliasesDirectory(t *testing.T) {
	_, err := loadAliases(t.TempDir())
	if err == nil {
		t.Fatal("expected error when alias file is a directory")
	}
}

func TestResolvePath(t *testing.T) {
	aliases := map[string]string{"work": "/data/work", "bak": "/mnt/bak"}

	tests := []struct {
		arg  string
		want string
	}{
		{"work", "/data/work"},
		{"bak", "/mnt/bak"},
		{"unknown", "unknown"},
		{"./work", "./work"},
		{"work/sub", "work/sub"},
		{`work\sub`, `work\sub`},
		{"/work", "/work"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.arg, func(t *testing.T) {
			if got := resolvePath(tt.arg, aliases); got != tt.want {
				t.Errorf("resolvePath(%q) = %q, want %q", tt.arg, got, tt.want)
			}
		})
	}
}

func TestRunWithAliases(t *testing.T) {
	root := t.TempDir()
	chdir(t, root)
	src := mkdir(t, root, "real-src")
	dst := filepath.Join(root, "real-dst")
	writeFile(t, filepath.Join(src, "a.txt"), "a")
	writeFile(t, filepath.Join(root, ".config", "aliases"),
		fmt.Sprintf("# aliases\n\none = %q\ntwo = %q\n", src, dst))

	var stdout, stderr bytes.Buffer

	if code := run([]string{"check", "one", "two"}, nil, &stdout, &stderr); code != 0 {
		t.Fatalf("check: exit code %d, stderr: %q", code, stderr.String())
	}
	assertTree(t, "destination after check", snapshot(t, dst), nil)

	stdout.Reset()
	if code := run([]string{"sync", "one", "two"}, nil, &stdout, &stderr); code != 0 {
		t.Fatalf("sync: exit code %d, stderr: %q", code, stderr.String())
	}
	assertTree(t, "destination after sync", snapshot(t, dst), map[string]string{"a.txt": "a"})
}

func TestRunAliasWithSeparatorIsNotResolved(t *testing.T) {
	root := t.TempDir()
	chdir(t, root)
	writeFile(t, filepath.Join(root, ".config", "aliases"), fmt.Sprintf("one = %q\n", mkdir(t, root, "real")))

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
	chdir(t, root)
	writeFile(t, filepath.Join(root, ".config", "aliases"), "broken line\n")

	var stdout, stderr bytes.Buffer
	code := run([]string{"sync", "a", "b"}, nil, &stdout, &stderr)
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if want := "aliases:1: invalid alias definition"; !bytes.Contains(stderr.Bytes(), []byte(want)) {
		t.Errorf("stderr = %q, want it to contain %q", stderr.String(), want)
	}
}
