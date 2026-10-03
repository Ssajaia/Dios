package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestInitAliases(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(t *testing.T, root string)
		want    string
		wantErr string
	}{
		{
			name:  "creates directory and file",
			setup: func(t *testing.T, root string) {},
			want:  aliasTemplate,
		},
		{
			name: "directory already exists",
			setup: func(t *testing.T, root string) {
				mkdir(t, root, ".config")
			},
			want: aliasTemplate,
		},
		{
			name: "existing file is not overwritten",
			setup: func(t *testing.T, root string) {
				writeFile(t, filepath.Join(root, ".config", "aliases"), "mine = \"/x\"\n")
			},
			want:    "mine = \"/x\"\n",
			wantErr: "already initialized",
		},
		{
			name: "existing empty file is not overwritten",
			setup: func(t *testing.T, root string) {
				writeFile(t, filepath.Join(root, ".config", "aliases"), "")
			},
			want:    "",
			wantErr: "already initialized",
		},
		{
			name: "config path is a file",
			setup: func(t *testing.T, root string) {
				writeFile(t, filepath.Join(root, ".config"), "not a directory")
			},
			wantErr: "cannot create directory",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			tt.setup(t, root)
			file := filepath.Join(root, ".config", "aliases")

			checkErr(t, initAliases(file), tt.wantErr)

			if tt.wantErr == "cannot create directory" {
				return
			}
			got, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tt.want {
				t.Errorf("file content = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestInitTemplateIsValidAliasFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), ".config", "aliases")
	if err := initAliases(file); err != nil {
		t.Fatal(err)
	}

	aliases, err := loadAliases(file)
	if err != nil {
		t.Fatalf("template does not load: %v", err)
	}
	if len(aliases) != 0 {
		t.Errorf("template defines aliases: %v", aliases)
	}
}

func TestRunInit(t *testing.T) {
	root := t.TempDir()
	chdir(t, root)

	var stdout, stderr bytes.Buffer
	if code := run([]string{"init"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit code = %d, stderr: %q", code, stderr.String())
	}
	if want := "Created " + aliasFile + "\n"; stdout.String() != want {
		t.Errorf("stdout = %q, want %q", stdout.String(), want)
	}
	got, err := os.ReadFile(aliasFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != aliasTemplate {
		t.Errorf("file content = %q, want template", got)
	}

	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"init"}, &stdout, &stderr); code != 1 {
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
	chdir(t, root)

	var stdout, stderr bytes.Buffer
	if code := run([]string{"init", "extra"}, &stdout, &stderr); code != 2 {
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
	chdir(t, root)
	src := mkdir(t, root, "real-src")
	dst := filepath.Join(root, "real-dst")
	writeFile(t, filepath.Join(src, "a.txt"), "a")

	var stdout, stderr bytes.Buffer
	if code := run([]string{"init"}, &stdout, &stderr); code != 0 {
		t.Fatalf("init: exit code %d, stderr: %q", code, stderr.String())
	}

	content := aliasTemplate + fmt.Sprintf("one = %q\ntwo = %q\n", src, dst)
	if err := os.WriteFile(aliasFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	if code := run([]string{"sync", "one", "two"}, &stdout, &stderr); code != 0 {
		t.Fatalf("sync: exit code %d, stderr: %q", code, stderr.String())
	}
	assertTree(t, "destination", snapshot(t, dst), map[string]string{"a.txt": "a"})
}
