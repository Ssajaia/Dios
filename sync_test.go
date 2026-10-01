package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseSyncArgs(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantDryRun bool
		wantPaths  []string
		wantErr    string
	}{
		{
			name:      "paths only",
			args:      []string{"a", "b"},
			wantPaths: []string{"a", "b"},
		},
		{
			name:       "flag first",
			args:       []string{"--dry-run", "a", "b"},
			wantDryRun: true,
			wantPaths:  []string{"a", "b"},
		},
		{
			name:       "flag between",
			args:       []string{"a", "--dry-run", "b"},
			wantDryRun: true,
			wantPaths:  []string{"a", "b"},
		},
		{
			name:       "flag last",
			args:       []string{"a", "b", "--dry-run"},
			wantDryRun: true,
			wantPaths:  []string{"a", "b"},
		},
		{
			name:    "no arguments",
			args:    nil,
			wantErr: "expected <source> and <destination>",
		},
		{
			name:    "one path",
			args:    []string{"a"},
			wantErr: "expected <source> and <destination>",
		},
		{
			name:    "three paths",
			args:    []string{"a", "b", "c"},
			wantErr: "expected <source> and <destination>",
		},
		{
			name:    "unknown option",
			args:    []string{"--force", "a", "b"},
			wantErr: "unknown option: --force",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dryRun, src, dst, err := parseSyncArgs(tt.args)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}

				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %q, want it to contain %q", err.Error(), tt.wantErr)
				}

				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if dryRun != tt.wantDryRun {
				t.Errorf("dryRun = %v, want %v", dryRun, tt.wantDryRun)
			}

			if src != tt.wantPaths[0] || dst != tt.wantPaths[1] {
				t.Errorf("paths = [%s %s], want %v", src, dst, tt.wantPaths)
			}
		})
	}
}

func TestSyncDirs(t *testing.T) {
	root := t.TempDir()

	source := filepath.Join(root, "source")
	destination := filepath.Join(root, "destination")

	if err := os.MkdirAll(filepath.Join(source, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}

	writeFile(t, filepath.Join(source, "hello.txt"), "hello")
	writeFile(t, filepath.Join(source, "nested", "test.txt"), "test")

	var output bytes.Buffer

	if err := syncDirs(source, destination, false, &output); err != nil {
		t.Fatal(err)
	}

	assertFileContent(t, filepath.Join(destination, "hello.txt"), "hello")
	assertFileContent(t, filepath.Join(destination, "nested", "test.txt"), "test")
}

func TestSyncUpdatesChangedFile(t *testing.T) {
	root := t.TempDir()

	source := filepath.Join(root, "source")
	destination := filepath.Join(root, "destination")

	mustMkdir(t, source)
	mustMkdir(t, destination)

	writeFile(t, filepath.Join(source, "file.txt"), "new")
	writeFile(t, filepath.Join(destination, "file.txt"), "old")

	var output bytes.Buffer

	if err := syncDirs(source, destination, false, &output); err != nil {
		t.Fatal(err)
	}

	assertFileContent(t, filepath.Join(destination, "file.txt"), "new")

	if !strings.Contains(output.String(), "~") {
		t.Errorf("expected update output, got %q", output.String())
	}
}

func TestSyncRemovesExtraFiles(t *testing.T) {
	root := t.TempDir()

	source := filepath.Join(root, "source")
	destination := filepath.Join(root, "destination")

	mustMkdir(t, source)
	mustMkdir(t, destination)

	writeFile(t, filepath.Join(source, "keep.txt"), "keep")
	writeFile(t, filepath.Join(destination, "keep.txt"), "keep")
	writeFile(t, filepath.Join(destination, "extra.txt"), "remove")

	var output bytes.Buffer

	if err := syncDirs(source, destination, false, &output); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(destination, "extra.txt")); !os.IsNotExist(err) {
		t.Fatalf("extra file still exists")
	}

	if !strings.Contains(output.String(), "-") {
		t.Errorf("expected removal output, got %q", output.String())
	}
}

func TestDryRunDoesNotModifyDestination(t *testing.T) {
	root := t.TempDir()

	source := filepath.Join(root, "source")
	destination := filepath.Join(root, "destination")

	mustMkdir(t, source)
	mustMkdir(t, destination)

	writeFile(t, filepath.Join(source, "new.txt"), "new")
	writeFile(t, filepath.Join(destination, "old.txt"), "old")

	var output bytes.Buffer

	if err := syncDirs(source, destination, true, &output); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(destination, "new.txt")); !os.IsNotExist(err) {
		t.Fatalf("dry-run created new file")
	}

	assertFileContent(t, filepath.Join(destination, "old.txt"), "old")

	if !strings.Contains(output.String(), "+") {
		t.Errorf("expected dry-run output, got %q", output.String())
	}
}

func TestValidatePaths(t *testing.T) {
	root := t.TempDir()

	source := filepath.Join(root, "source")
	destination := filepath.Join(root, "destination")

	mustMkdir(t, source)

	if err := validatePaths(source, destination); err != nil {
		t.Fatalf("missing destination should be valid: %v", err)
	}

	if err := validatePaths(source, source); err == nil {
		t.Fatal("expected same-path error")
	}

	file := filepath.Join(root, "file.txt")
	writeFile(t, file, "test")

	if err := validatePaths(file, destination); err == nil {
		t.Fatal("expected source-not-directory error")
	}

	if err := validatePaths(filepath.Join(root, "missing"), destination); err == nil {
		t.Fatal("expected missing-source error")
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()

	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func assertFileContent(t *testing.T, path, want string) {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	if string(data) != want {
		t.Errorf("%s = %q, want %q", path, string(data), want)
	}
}
