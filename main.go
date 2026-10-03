package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const usage = "usage: dios sync <source> <destination>\n       dios check <source> <destination>"

var aliasFile = filepath.Join(".config", "aliases")

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintf(stderr, "Error: missing command\n%s\n", usage)
		return 2
	}
	switch args[0] {
	case "sync":
		return runPaths(args[1:], false, stdout, stderr)
	case "check":
		return runPaths(args[1:], true, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "Error: unknown command: %s\n%s\n", args[0], usage)
		return 2
	}
}

func runPaths(args []string, checkOnly bool, stdout, stderr io.Writer) int {
	source, destination, err := parsePaths(args)
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

	changes, err := syncDirs(source, destination, checkOnly)
	if err != nil {
		if !checkOnly && len(changes) > 0 {
			fmt.Fprintln(stdout, "Changes made before the error:")
			printChanges(stdout, changes)
		}
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}

	if checkOnly {
		printCheckReport(stdout, source, destination, changes)
	} else {
		printSyncReport(stdout, changes)
	}
	return 0
}

func parsePaths(args []string) (source, destination string, err error) {
	var paths []string
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			return "", "", fmt.Errorf("unknown option: %s", arg)
		}
		paths = append(paths, arg)
	}
	if len(paths) != 2 {
		return "", "", fmt.Errorf("expected <source> and <destination>, got %d path(s)", len(paths))
	}
	return paths[0], paths[1], nil
}

func printSyncReport(w io.Writer, changes []change) {
	if len(changes) == 0 {
		fmt.Fprintln(w, "Already synchronized.")
		return
	}
	fmt.Fprintln(w, "Syncing:")
	printChanges(w, changes)
	fmt.Fprint(w, "\nSync completed.\n")
}

func printChanges(w io.Writer, changes []change) {
	for _, c := range changes {
		fmt.Fprintf(w, "  %s\n", c)
	}
}

func printCheckReport(w io.Writer, source, destination string, changes []change) {
	fmt.Fprintf(w, "Checking %s -> %s\n\n", source, destination)
	if len(changes) == 0 {
		fmt.Fprintln(w, "Already synchronized.")
		return
	}

	width := 0
	for _, c := range changes {
		if n := utf8.RuneCountInString(c.label()); n > width {
			width = n
		}
	}
	for _, c := range changes {
		fmt.Fprintf(w, "  %c %-*s  %s\n", c.kind, width, c.label(), c.detail())
	}
	fmt.Fprintf(w, "\nSummary: %s.\nNo changes were made.\n", summarize(changes))
}

func summarize(changes []change) string {
	var create, update, remove, skipped int
	for _, c := range changes {
		switch c.kind {
		case '+':
			create++
		case '~':
			update++
		case '-':
			remove++
		case '!':
			skipped++
		}
	}

	var parts []string
	if create > 0 {
		parts = append(parts, fmt.Sprintf("%d to create", create))
	}
	if update > 0 {
		parts = append(parts, fmt.Sprintf("%d to update", update))
	}
	if remove > 0 {
		parts = append(parts, fmt.Sprintf("%d to remove", remove))
	}
	if skipped > 0 {
		parts = append(parts, fmt.Sprintf("%d skipped", skipped))
	}
	return strings.Join(parts, ", ")
}
