package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const usage = `usage: dios init
       dios sync [options] <source> <destination>
       dios check [options] <source> <destination>

options:
  -s, --skip <name>   skip files or directories matching name (repeatable)
  --pd, --prevent-delete
                      create and update, but never delete from the destination
  --ns, --not-sure    ask before every change (sync only)`

var aliasFile = filepath.Join(".config", "aliases")

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintf(stderr, "Error: missing command\n%s\n", usage)
		return 2
	}
	switch args[0] {
	case "init":
		return runInit(args[1:], stdout, stderr)
	case "sync":
		return runPaths(args[1:], false, stdin, stdout, stderr)
	case "check":
		return runPaths(args[1:], true, stdin, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "Error: unknown command: %s\n%s\n", args[0], usage)
		return 2
	}
}

func runInit(args []string, stdout, stderr io.Writer) int {
	if len(args) != 0 {
		fmt.Fprintf(stderr, "Error: init takes no arguments\n%s\n", usage)
		return 2
	}
	if err := initAliases(aliasFile); err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "Created %s\n", aliasFile)
	return 0
}

func runPaths(args []string, checkOnly bool, stdin io.Reader, stdout, stderr io.Writer) int {
	cfg, err := parseSyncArgs(args)
	if err == nil && checkOnly && cfg.notSure {
		err = fmt.Errorf("--not-sure cannot be used with check")
	}
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n%s\n", err, usage)
		return 2
	}

	aliases, err := loadAliases(aliasFile)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	source := resolvePath(cfg.source, aliases)
	destination := resolvePath(cfg.destination, aliases)

	opts := options{dryRun: checkOnly, preventDelete: cfg.preventDelete, skip: cfg.skip}
	if cfg.notSure {
		opts.confirm = newConfirm(stdin, stdout)
	}

	changes, err := syncDirs(source, destination, opts)
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

type syncArgs struct {
	source        string
	destination   string
	notSure       bool
	preventDelete bool
	skip          []string
}

func parseSyncArgs(args []string) (syncArgs, error) {
	var cfg syncArgs
	var paths []string

	addSkip := func(value string) error {
		p, err := normalizeSkip(value)
		if err != nil {
			return err
		}
		cfg.skip = append(cfg.skip, p)
		return nil
	}

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--not-sure" || arg == "--ns":
			cfg.notSure = true
		case arg == "--prevent-delete" || arg == "--pd":
			cfg.preventDelete = true
		case arg == "--skip" || arg == "-s":
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
				return syncArgs{}, fmt.Errorf("%s requires a value", arg)
			}
			i++
			if err := addSkip(args[i]); err != nil {
				return syncArgs{}, err
			}
		case strings.HasPrefix(arg, "--skip="):
			if err := addSkip(strings.TrimPrefix(arg, "--skip=")); err != nil {
				return syncArgs{}, err
			}
		case strings.HasPrefix(arg, "-"):
			return syncArgs{}, fmt.Errorf("unknown option: %s", arg)
		default:
			paths = append(paths, arg)
		}
	}

	if len(paths) != 2 {
		return syncArgs{}, fmt.Errorf("expected <source> and <destination>, got %d path(s)", len(paths))
	}
	cfg.source, cfg.destination = paths[0], paths[1]
	return cfg, nil
}

func newConfirm(in io.Reader, out io.Writer) func(change) bool {
	if in == nil {
		in = strings.NewReader("")
	}
	reader := bufio.NewReader(in)
	return func(c change) bool {
		fmt.Fprintf(out, "%c %s  (%s)  apply? [y/N] ", c.kind, c.label(), c.reason)
		line, err := reader.ReadString('\n')
		if err != nil && line == "" {
			fmt.Fprintln(out)
			return false
		}
		answer := strings.TrimSpace(line)
		return answer == "y" || answer == "Y"
	}
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
