package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func validatePaths(source, destination string) error {
	sourceInfo, err := os.Stat(source)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("source does not exist: %s", source)
		}
		return fmt.Errorf("cannot access source: %w", err)
	}

	if !sourceInfo.IsDir() {
		return fmt.Errorf("source is not a directory: %s", source)
	}

	sourceAbs, err := filepath.Abs(source)
	if err != nil {
		return fmt.Errorf("cannot resolve source path: %w", err)
	}

	destinationAbs, err := filepath.Abs(destination)
	if err != nil {
		return fmt.Errorf("cannot resolve destination path: %w", err)
	}

	sourceAbs, err = filepath.EvalSymlinks(sourceAbs)
	if err != nil {
		return fmt.Errorf("cannot resolve source path: %w", err)
	}

	if destinationInfo, err := os.Stat(destination); err == nil {
		if !destinationInfo.IsDir() {
			return fmt.Errorf("destination is not a directory: %s", destination)
		}

		destinationAbs, err = filepath.EvalSymlinks(destinationAbs)
		if err != nil {
			return fmt.Errorf("cannot resolve destination path: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("cannot access destination: %w", err)
	}

	if samePath(sourceAbs, destinationAbs) {
		return fmt.Errorf("source and destination must be different paths")
	}

	if isInside(sourceAbs, destinationAbs) {
		return fmt.Errorf("destination must not be inside source")
	}

	if isInside(destinationAbs, sourceAbs) {
		return fmt.Errorf("source must not be inside destination")
	}

	return nil
}

func syncDirs(source, destination string, dryRun bool, stdout io.Writer) error {
	if err := validatePaths(source, destination); err != nil {
		return err
	}

	if !dryRun {
		if err := os.MkdirAll(destination, 0o755); err != nil {
			return fmt.Errorf("create destination: %w", err)
		}
	}

	entries, err := os.ReadDir(source)
	if err != nil {
		return fmt.Errorf("read source: %w", err)
	}

	seen := make(map[string]bool)

	for _, entry := range entries {
		name := entry.Name()
		seen[name] = true

		srcPath := filepath.Join(source, name)
		dstPath := filepath.Join(destination, name)

		if err := syncEntry(srcPath, dstPath, dryRun, stdout); err != nil {
			return err
		}
	}

	return removeExtraEntries(destination, seen, dryRun, stdout)
}

func syncEntry(source, destination string, dryRun bool, stdout io.Writer) error {
	sourceInfo, err := os.Stat(source)
	if err != nil {
		return fmt.Errorf("inspect %s: %w", source, err)
	}

	destinationInfo, err := os.Stat(destination)

	if os.IsNotExist(err) {
		if sourceInfo.IsDir() {
			fmt.Fprintf(stdout, "+ %s\n", destination)

			if !dryRun {
				if err := os.MkdirAll(destination, sourceInfo.Mode()); err != nil {
					return fmt.Errorf("create directory %s: %w", destination, err)
				}
			}

			entries, err := os.ReadDir(source)
			if err != nil {
				return fmt.Errorf("read directory %s: %w", source, err)
			}

			seen := make(map[string]bool)

			for _, entry := range entries {
				seen[entry.Name()] = true

				srcPath := filepath.Join(source, entry.Name())
				dstPath := filepath.Join(destination, entry.Name())

				if err := syncEntry(srcPath, dstPath, dryRun, stdout); err != nil {
					return err
				}
			}

			return removeExtraEntries(destination, seen, dryRun, stdout)
		}

		fmt.Fprintf(stdout, "+ %s\n", destination)

		if !dryRun {
			if err := copyFile(source, destination, sourceInfo.Mode()); err != nil {
				return err
			}
		}

		return nil
	}

	if err != nil {
		return fmt.Errorf("inspect destination %s: %w", destination, err)
	}

	if sourceInfo.IsDir() {
		if !destinationInfo.IsDir() {
			fmt.Fprintf(stdout, "~ %s\n", destination)

			if !dryRun {
				if err := os.RemoveAll(destination); err != nil {
					return fmt.Errorf("remove %s: %w", destination, err)
				}

				if err := os.MkdirAll(destination, sourceInfo.Mode()); err != nil {
					return fmt.Errorf("create directory %s: %w", destination, err)
				}
			}
		}

		entries, err := os.ReadDir(source)
		if err != nil {
			return fmt.Errorf("read directory %s: %w", source, err)
		}

		seen := make(map[string]bool)

		for _, entry := range entries {
			seen[entry.Name()] = true

			srcPath := filepath.Join(source, entry.Name())
			dstPath := filepath.Join(destination, entry.Name())

			if err := syncEntry(srcPath, dstPath, dryRun, stdout); err != nil {
				return err
			}
		}

		return removeExtraEntries(destination, seen, dryRun, stdout)
	}

	if destinationInfo.IsDir() {
		fmt.Fprintf(stdout, "~ %s\n", destination)

		if !dryRun {
			if err := os.RemoveAll(destination); err != nil {
				return fmt.Errorf("remove %s: %w", destination, err)
			}

			if err := copyFile(source, destination, sourceInfo.Mode()); err != nil {
				return err
			}
		}

		return nil
	}

	same, err := filesEqual(source, destination)
	if err != nil {
		return err
	}

	if same {
		return nil
	}

	fmt.Fprintf(stdout, "~ %s\n", destination)

	if !dryRun {
		if err := copyFile(source, destination, sourceInfo.Mode()); err != nil {
			return err
		}
	}

	return nil
}

func removeExtraEntries(destination string, seen map[string]bool, dryRun bool, stdout io.Writer) error {
	entries, err := os.ReadDir(destination)
	if os.IsNotExist(err) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("read destination %s: %w", destination, err)
	}

	for _, entry := range entries {
		if seen[entry.Name()] {
			continue
		}

		path := filepath.Join(destination, entry.Name())

		fmt.Fprintf(stdout, "- %s\n", path)

		if !dryRun {
			if err := os.RemoveAll(path); err != nil {
				return fmt.Errorf("remove %s: %w", path, err)
			}
		}
	}

	return nil
}

func copyFile(source, destination string, mode os.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open source %s: %w", source, err)
	}
	defer input.Close()

	output, err := os.OpenFile(
		destination,
		os.O_CREATE|os.O_WRONLY|os.O_TRUNC,
		mode,
	)
	if err != nil {
		return fmt.Errorf("create destination %s: %w", destination, err)
	}
	defer output.Close()

	if _, err := io.Copy(output, input); err != nil {
		return fmt.Errorf("copy %s to %s: %w", source, destination, err)
	}

	return nil
}

func filesEqual(source, destination string) (bool, error) {
	sourceInfo, err := os.Stat(source)
	if err != nil {
		return false, fmt.Errorf("inspect source: %w", err)
	}

	destinationInfo, err := os.Stat(destination)
	if err != nil {
		return false, fmt.Errorf("inspect destination: %w", err)
	}

	if sourceInfo.Size() != destinationInfo.Size() {
		return false, nil
	}

	sourceFile, err := os.Open(source)
	if err != nil {
		return false, fmt.Errorf("open source: %w", err)
	}
	defer sourceFile.Close()

	destinationFile, err := os.Open(destination)
	if err != nil {
		return false, fmt.Errorf("open destination: %w", err)
	}
	defer destinationFile.Close()

	const bufferSize = 32 * 1024

	sourceBuffer := make([]byte, bufferSize)
	destinationBuffer := make([]byte, bufferSize)

	for {
		sourceN, sourceErr := sourceFile.Read(sourceBuffer)
		destinationN, destinationErr := destinationFile.Read(destinationBuffer)

		if sourceN != destinationN {
			return false, nil
		}

		for i := 0; i < sourceN; i++ {
			if sourceBuffer[i] != destinationBuffer[i] {
				return false, nil
			}
		}

		if sourceErr == io.EOF && destinationErr == io.EOF {
			return true, nil
		}

		if sourceErr != nil {
			return false, fmt.Errorf("read source: %w", sourceErr)
		}

		if destinationErr != nil {
			return false, fmt.Errorf("read destination: %w", destinationErr)
		}
	}
}

func samePath(a, b string) bool {
	return filepath.Clean(a) == filepath.Clean(b)
}

func isInside(parent, child string) bool {
	relative, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}

	if relative == "." || relative == ".." {
		return false
	}

	return !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
