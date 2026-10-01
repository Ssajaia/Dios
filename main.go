package main

import (
	"fmt"
	"io"
	"os"
	"strings"
)

const usage = "usage: dios sync [--dry-run] <source> <destination>"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintf(stderr, "Error: missing command\n%s\n", usage)
		return 2
	}

	if args[0] != "sync" {
		fmt.Fprintf(stderr, "Error: unknown command: %s\n%s\n", args[0], usage)
		return 2
	}

	return runSync(args[1:], stdout, stderr)
}

func runSync(args []string, stdout, stderr io.Writer) int {
	dryRun, source, destination, err := parseSyncArgs(args)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n%s\n", err, usage)
		return 2
	}

	if err := syncDirs(source, destination, dryRun, stdout); err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}

	return 0
}

func parseSyncArgs(args []string) (dryRun bool, source, destination string, err error) {
	var paths []string

	for _, arg := range args {
		switch {
		case arg == "--dry-run":
			dryRun = true
		case strings.HasPrefix(arg, "-"):
			return false, "", "", fmt.Errorf("unknown option: %s", arg)
		default:
			paths = append(paths, arg)
		}
	}

	if len(paths) != 2 {
		return false, "", "", fmt.Errorf(
			"expected <source> and <destination>, got %d path(s)",
			len(paths),
		)
	}

	return dryRun, paths[0], paths[1], nil
}