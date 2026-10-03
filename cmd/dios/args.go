package main

import (
	"fmt"
	"strings"

	"github.com/ssajaia/dios/internal/syncer"
)

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
		p, err := syncer.NormalizeSkip(value)
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
