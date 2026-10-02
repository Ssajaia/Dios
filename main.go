package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const usage = "usage: dios sync [--dry-run] <source> <destination>"

var aliasFile = filepath.Join(".config", "aliases")

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

	aliases, err := loadAliases(aliasFile)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	source = resolvePath(source, aliases)
	destination = resolvePath(destination, aliases)

	changes, err := syncDirs(source, destination, dryRun)
	if err != nil {
		if !dryRun && len(changes) > 0 {
			fmt.Fprintln(stdout, "Changes made before the error:")
			printChanges(stdout, changes)
		}
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	printReport(stdout, changes, dryRun)
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
		return false, "", "", fmt.Errorf("expected <source> and <destination>, got %d path(s)", len(paths))
	}
	return dryRun, paths[0], paths[1], nil
}

func printReport(w io.Writer, changes []change, dryRun bool) {
	if len(changes) == 0 {
		fmt.Fprintln(w, "Already synchronized.")
		return
	}
	if dryRun {
		fmt.Fprint(w, "Dry run:\n\n")
	} else {
		fmt.Fprintln(w, "Syncing:")
	}
	printChanges(w, changes)
	if dryRun {
		fmt.Fprint(w, "\nNo changes were made.\n")
	} else {
		fmt.Fprint(w, "\nSync completed.\n")
	}
}

func printChanges(w io.Writer, changes []change) {
	for _, c := range changes {
		fmt.Fprintf(w, "  %s\n", c)
	}
}
