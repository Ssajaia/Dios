package main

import (
	"bufio"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/ssajaia/dios/internal/syncer"
)

func newConfirm(in io.Reader, out io.Writer) func(syncer.Change) bool {
	if in == nil {
		in = strings.NewReader("")
	}
	reader := bufio.NewReader(in)
	return func(c syncer.Change) bool {
		fmt.Fprintf(out, "%c %s  (%s)  apply? [y/N] ", c.Kind, c.Label(), c.Reason)
		line, err := reader.ReadString('\n')
		if err != nil && line == "" {
			fmt.Fprintln(out)
			return false
		}
		answer := strings.TrimSpace(line)
		return answer == "y" || answer == "Y"
	}
}

func printSyncReport(w io.Writer, changes []syncer.Change) {
	if len(changes) == 0 {
		fmt.Fprintln(w, "Already synchronized.")
		return
	}
	fmt.Fprintln(w, "Syncing:")
	printChanges(w, changes)
	fmt.Fprint(w, "\nSync completed.\n")
}

func printChanges(w io.Writer, changes []syncer.Change) {
	for _, c := range changes {
		fmt.Fprintf(w, "  %s\n", c)
	}
}

func printCheckReport(w io.Writer, source, destination string, changes []syncer.Change) {
	fmt.Fprintf(w, "Checking %s -> %s\n\n", source, destination)
	if len(changes) == 0 {
		fmt.Fprintln(w, "Already synchronized.")
		return
	}

	width := 0
	for _, c := range changes {
		if n := utf8.RuneCountInString(c.Label()); n > width {
			width = n
		}
	}
	for _, c := range changes {
		fmt.Fprintf(w, "  %c %-*s  %s\n", c.Kind, width, c.Label(), c.Detail())
	}
	fmt.Fprintf(w, "\nSummary: %s.\nNo changes were made.\n", summarize(changes))
}

func summarize(changes []syncer.Change) string {
	var create, update, remove, skipped int
	for _, c := range changes {
		switch c.Kind {
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
