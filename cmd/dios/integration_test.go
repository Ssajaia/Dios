package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestBinarySyncAndCheck(t *testing.T) {
	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate integration test source")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(testFile), "..", ".."))
	binaryName := "dios"
	if runtime.GOOS == "windows" {
		binaryName += ".exe"
	}
	binaryPath := filepath.Join(t.TempDir(), binaryName)

	build := exec.Command("go", "build", "-o", binaryPath, "./cmd/dios")
	build.Dir = repoRoot
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build dios binary: %v\n%s", err, output)
	}

	root := t.TempDir()
	source := filepath.Join(root, "source")
	destination := filepath.Join(root, "destination")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(destination, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "initial.txt"), []byte("initial"), 0o644); err != nil {
		t.Fatal(err)
	}

	code, stdout, stderr := runBinary(t, binaryPath, root, "sync", source, destination)
	if code != 0 {
		t.Fatalf("sync exit code = %d, want 0; stdout: %q; stderr: %q", code, stdout, stderr)
	}
	if got, err := os.ReadFile(filepath.Join(destination, "initial.txt")); err != nil || string(got) != "initial" {
		t.Fatalf("synced file = %q, error = %v; want %q", got, err, "initial")
	}

	code, stdout, stderr = runBinary(t, binaryPath, root, "check", source, destination)
	if code != 0 {
		t.Fatalf("check of synchronized trees exit code = %d, want 0; stdout: %q; stderr: %q", code, stdout, stderr)
	}

	if err := os.WriteFile(filepath.Join(source, "later.txt"), []byte("later"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr = runBinary(t, binaryPath, root, "check", source, destination)
	if code != 1 {
		t.Fatalf("check with differences exit code = %d, want 1; stdout: %q; stderr: %q", code, stdout, stderr)
	}
	if _, err := os.Stat(filepath.Join(destination, "later.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("check changed destination; later.txt stat error = %v", err)
	}
}

func runBinary(t *testing.T, binaryPath, workingDirectory string, args ...string) (int, string, string) {
	t.Helper()
	cmd := exec.Command(binaryPath, args...)
	cmd.Dir = workingDirectory
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		return 0, stdout.String(), stderr.String()
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), stdout.String(), stderr.String()
	}
	t.Fatalf("run dios binary: %v", err)
	return -1, stdout.String(), stderr.String()
}
