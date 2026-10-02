package main

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strconv"
	"strings"
)

func loadAliases(file string) (map[string]string, error) {
	f, err := os.Open(file)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("cannot read alias file %s: %w", file, err)
	}
	defer f.Close()

	aliases := make(map[string]string)
	scanner := bufio.NewScanner(f)
	for lineNo := 1; scanner.Scan(); lineNo++ {
		text := scanner.Text()
		if lineNo == 1 {
			text = strings.TrimPrefix(text, "\ufeff")
		}
		line := strings.TrimSpace(text)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, value, err := parseAliasLine(line)
		if err != nil {
			return nil, fmt.Errorf("%s:%d: %w", file, lineNo, err)
		}
		if _, exists := aliases[name]; exists {
			return nil, fmt.Errorf("%s:%d: duplicate alias: %s", file, lineNo, name)
		}
		aliases[name] = value
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("cannot read alias file %s: %w", file, err)
	}
	return aliases, nil
}

func parseAliasLine(line string) (string, string, error) {
	rawName, rawValue, ok := strings.Cut(line, "=")
	if !ok {
		return "", "", errors.New(`invalid alias definition, expected: name = "path"`)
	}

	name := strings.TrimSpace(rawName)
	if name == "" || strings.HasPrefix(name, "-") || strings.ContainsAny(name, " \t/\\") {
		return "", "", fmt.Errorf("invalid alias name: %q", name)
	}

	value, err := strconv.Unquote(strings.TrimSpace(rawValue))
	if err != nil {
		return "", "", errors.New("alias path must be quoted, use / in paths or wrap the path in backticks")
	}
	if value == "" {
		return "", "", fmt.Errorf("alias path is empty: %s", name)
	}
	return name, value, nil
}

func resolvePath(arg string, aliases map[string]string) string {
	if strings.ContainsAny(arg, `/\`) {
		return arg
	}
	if target, ok := aliases[arg]; ok {
		return target
	}
	return arg
}
