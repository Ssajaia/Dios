package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/ssajaia/dios/internal/alias"
	"github.com/ssajaia/dios/internal/syncer"
)

var version = "dev"

const usage = `usage: dios init
       dios sync [options] <source> <destination>
       dios check [options] <source> <destination>
       dios help [command]

options:
  -s, --skip <name>   skip files or directories matching name (repeatable)
  --pd, --prevent-delete
                      create and update, but never delete from the destination
  --ns, --not-sure    ask before every change (sync only)
  -h, --help          show help`

const helpText = `Dios synchronizes one directory into another, one way: source -> destination.

Usage:
  dios <command> [options]

Commands:
  init        create .config/aliases in the current directory
  sync        make the destination match the source
  check       show what sync would do without changing anything
  help        show help

Aliases:
  Aliases are read from .config/aliases in the current working directory.

Options:
  -h, --help   show help
  --version    print the current Dios version

Exit codes:
  0 success or no differences in check
  1 runtime error or differences found in check
  2 invalid usage
`

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
	case "help":
		return runHelp(args[1:], stdout, stderr)
	case "-h", "--help":
		fmt.Fprint(stdout, helpText)
		return 0
	case "--version":
		fmt.Fprintln(stdout, version)
		return 0
	case "version":
		fmt.Fprintln(stdout, version)
		return 0
	case "init":
		if isHelpRequest(args[1:]) {
			printInitHelp(stdout)
			return 0
		}
		return runInit(args[1:], stdout, stderr)
	case "sync":
		if isHelpRequest(args[1:]) {
			printSyncHelp(stdout)
			return 0
		}
		return runPaths(args[1:], false, stdin, stdout, stderr)
	case "check":
		if isHelpRequest(args[1:]) {
			printCheckHelp(stdout)
			return 0
		}
		return runPaths(args[1:], true, stdin, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "Error: unknown command: %s\n%s\n", args[0], usage)
		return 2
	}
}

func isHelpRequest(args []string) bool {
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			return true
		}
	}
	return false
}

func runHelp(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stdout, helpText)
		return 0
	}
	switch args[0] {
	case "sync":
		printSyncHelp(stdout)
		return 0
	case "check":
		printCheckHelp(stdout)
		return 0
	case "init":
		printInitHelp(stdout)
		return 0
	case "help":
		fmt.Fprint(stdout, helpText)
		return 0
	default:
		fmt.Fprintf(stderr, "Error: unknown help topic: %s\n%s\n", args[0], usage)
		return 2
	}
}

func printSyncHelp(stdout io.Writer) {
	fmt.Fprint(stdout, `usage: dios sync [options] <source> <destination>

options:
  -s, --skip <name>   skip files or directories matching name (repeatable)
  --pd, --prevent-delete
                      create and update, but never delete from the destination
  --ns, --not-sure    ask before every change
  -h, --help          show help
`)
}

func printCheckHelp(stdout io.Writer) {
	fmt.Fprint(stdout, `usage: dios check [options] <source> <destination>

options:
  -s, --skip <name>   skip files or directories matching name (repeatable)
  --pd, --prevent-delete
                      create and update, but never delete from the destination
  -h, --help          show help
`)
}

func printInitHelp(stdout io.Writer) {
	fmt.Fprint(stdout, `usage: dios init

Create .config/aliases in the current working directory with a commented template.
`)
}

func runInit(args []string, stdout, stderr io.Writer) int {
	if len(args) != 0 {
		fmt.Fprintf(stderr, "Error: init takes no arguments\n%s\n", usage)
		return 2
	}
	if err := alias.Init(aliasFile); err != nil {
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

	aliases, err := alias.Load(aliasFile)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	source := alias.Resolve(cfg.source, aliases)
	destination := alias.Resolve(cfg.destination, aliases)

	opts := syncer.Options{DryRun: checkOnly, PreventDelete: cfg.preventDelete, Skip: cfg.skip}
	if cfg.notSure {
		opts.Confirm = newConfirm(stdin, stdout)
	}

	changes, err := syncer.Sync(source, destination, opts)
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
		if len(changes) > 0 {
			return 1
		}
		return 0
	}
	printSyncReport(stdout, changes)
	return 0
}
