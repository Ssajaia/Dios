package alias

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

const Template = `# Dios aliases
# One alias per line: name = "path"
# Use / in paths, or wrap the path in backticks to keep backslashes.
#
# workspace1 = "C:/users/someone/projects"
# backup = "D:/backup"
`

// Init creates file, and its directory, with the alias template.
// It fails if file already exists.
func Init(file string) error {
	dir := filepath.Dir(file)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("cannot create directory %s: %w", dir, err)
	}

	f, err := os.OpenFile(file, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("already initialized: %s exists", file)
	}
	if err != nil {
		return fmt.Errorf("cannot create %s: %w", file, err)
	}

	if _, err := f.WriteString(Template); err != nil {
		f.Close()
		os.Remove(file)
		return fmt.Errorf("cannot write %s: %w", file, err)
	}
	if err := f.Close(); err != nil {
		os.Remove(file)
		return fmt.Errorf("cannot write %s: %w", file, err)
	}
	return nil
}
