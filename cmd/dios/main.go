package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/ssajaia/dios/internal/alias"
	"github.com/ssajaia/dios/internal/syncer"
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
	} else {
		printSyncReport(stdout, changes)
	}
	return 0
}
