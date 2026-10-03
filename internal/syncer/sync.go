// Package syncer makes one directory match another, one way.
package syncer

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
)

var errSamePath = errors.New("source and destination must be different paths")

var (
	tmpMu   sync.Mutex
	tracked = map[string]struct{}{}
)

// Change describes one difference between source and destination.
// Kind is '+' (create), '~' (update), '-' (remove) or '!' (left alone).
type Change struct {
	Kind   byte
	Path   string
	IsDir  bool
	Reason string
	Note   string
}

func (c Change) Label() string {
	if c.IsDir {
		return c.Path + "/"
	}
	return c.Path
}

func (c Change) Detail() string {
	if c.Note != "" {
		return c.Note
	}
	return c.Reason
}

func (c Change) String() string {
	s := fmt.Sprintf("%c %s", c.Kind, c.Label())
	if c.Note != "" {
		s += " (" + c.Note + ")"
	}
	return s
}

const needsDeleteNote = "skipped: needs delete, --prevent-delete is set"

// Options controls what Sync does.
type Options struct {
	DryRun        bool
	PreventDelete bool
	Skip          []string
	Confirm       func(Change) bool
}

type session struct {
	opts    Options
	changes []Change
}

// Sync makes destination match source and returns the changes it made, or
// would make when opts.DryRun is set.
func Sync(source, destination string, opts Options) ([]Change, error) {
	if err := validatePaths(source, destination); err != nil {
		return nil, err
	}
	if err := ensureCaseSafe(source, destination); err != nil {
		return nil, err
	}

	s := &session{opts: opts}

	_, err := os.Stat(destination)
	dstExists := err == nil
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("cannot access destination %s: %w", destination, err)
	}
	if !dstExists {
		c := Change{Kind: '+', Path: ".", IsDir: true, Reason: "destination does not exist"}
		if !s.approve(c) {
			return s.changes, nil
		}
		if !opts.DryRun {
			if err := os.MkdirAll(destination, 0o755); err != nil {
				return nil, fmt.Errorf("cannot create destination %s: %w", destination, err)
			}
			if err := cleanupStaleTempDir(destination); err != nil {
				return nil, err
			}
		}
		s.add(c)
	} else if !opts.DryRun {
		if err := cleanupStaleTempDir(destination); err != nil {
			return nil, err
		}
	}

	err = s.syncDir(source, destination, "", dstExists)
	return s.changes, err
}

func (s *session) add(c Change) {
	s.changes = append(s.changes, c)
}

func (s *session) approve(c Change) bool {
	if s.opts.Confirm == nil || s.opts.DryRun {
		return true
	}
	if s.opts.Confirm(c) {
		return true
	}
	s.add(Change{Kind: '!', Path: c.Path, IsDir: c.IsDir, Note: "skipped by user"})
	return false
}

func (s *session) skipped(rel string) bool {
	return matchesSkip(s.opts.Skip, rel)
}

func (s *session) syncDir(srcDir, dstDir, rel string, dstExists bool) error {
	srcEntries, err := os.ReadDir(srcDir)
	if err != nil {
		return fmt.Errorf("cannot read source directory %s: %w", srcDir, err)
	}

	var dstEntries []fs.DirEntry
	if dstExists {
		dstEntries, err = os.ReadDir(dstDir)
		if err != nil {
			return fmt.Errorf("cannot read destination directory %s: %w", dstDir, err)
		}
	}

	dstByName := make(map[string]fs.DirEntry, len(dstEntries))
	for _, e := range dstEntries {
		dstByName[e.Name()] = e
	}

	inSource := make(map[string]bool, len(srcEntries))
	for _, srcEntry := range srcEntries {
		name := srcEntry.Name()
		inSource[name] = true
		relPath := path.Join(rel, name)
		if s.skipped(relPath) {
			continue
		}
		err := s.syncEntry(srcEntry, dstByName[name], filepath.Join(srcDir, name), filepath.Join(dstDir, name), relPath)
		if err != nil {
			return err
		}
	}

	for _, dstEntry := range dstEntries {
		name := dstEntry.Name()
		if inSource[name] {
			continue
		}
		relPath := path.Join(rel, name)
		if s.skipped(relPath) {
			continue
		}
		if s.opts.PreventDelete {
			s.add(Change{Kind: '!', Path: relPath, IsDir: dstEntry.IsDir(), Note: "kept: not in source"})
			continue
		}
		c := Change{Kind: '-', Path: relPath, IsDir: dstEntry.IsDir(), Reason: "not in source"}
		if !s.approve(c) {
			continue
		}
		if err := s.removeDst(filepath.Join(dstDir, name), relPath); err != nil {
			return err
		}
		s.add(c)
	}
	return nil
}

func (s *session) syncEntry(srcEntry, dstEntry fs.DirEntry, srcPath, dstPath, rel string) error {
	typ := srcEntry.Type()
	switch {
	case typ.IsDir():
		return s.syncSubdir(srcEntry, dstEntry, srcPath, dstPath, rel)
	case typ.IsRegular():
		return s.syncFile(srcEntry, dstEntry, srcPath, dstPath, rel)
	default:
		note := "skipped: unsupported file type"
		if typ&fs.ModeSymlink != 0 {
			note = "skipped: symlink"
		}
		s.add(Change{Kind: '!', Path: rel, Note: note})
		return nil
	}
}

func (s *session) syncSubdir(srcEntry, dstEntry fs.DirEntry, srcPath, dstPath, rel string) error {
	dstExists := dstEntry != nil
	replace := dstExists && !dstEntry.IsDir()
	c := Change{Kind: '+', Path: rel, IsDir: true, Reason: "missing in destination"}

	if replace {
		if s.opts.PreventDelete {
			s.add(Change{Kind: '!', Path: rel, IsDir: true, Note: needsDeleteNote})
			return nil
		}
		c.Kind = '~'
		c.Reason = "destination is not a directory, source is a directory"
		if dstEntry.Type().IsRegular() {
			c.Reason = "destination is a file, source is a directory"
		}
		dstExists = false
	}

	if !dstExists {
		if !s.approve(c) {
			return nil
		}
		if replace {
			if err := s.removeDst(dstPath, rel); err != nil {
				return err
			}
		}
		if !s.opts.DryRun {
			info, err := srcEntry.Info()
			if err != nil {
				return fmt.Errorf("cannot stat source directory %s: %w", srcPath, err)
			}
			if err := os.Mkdir(dstPath, info.Mode().Perm()|0o700); err != nil {
				return fmt.Errorf("cannot create directory %s: %w", rel, err)
			}
		}
		s.add(c)
	}

	return s.syncDir(srcPath, dstPath, rel, dstExists)
}

func (s *session) syncFile(srcEntry, dstEntry fs.DirEntry, srcPath, dstPath, rel string) error {
	srcInfo, err := srcEntry.Info()
	if err != nil {
		return fmt.Errorf("cannot stat source file %s: %w", srcPath, err)
	}

	c := Change{Kind: '+', Path: rel, Reason: "missing in destination"}
	replace := false
	if dstEntry != nil {
		c.Kind = '~'
		if dstEntry.Type().IsRegular() {
			dstInfo, err := dstEntry.Info()
			if err != nil {
				return fmt.Errorf("cannot stat destination file %s: %w", dstPath, err)
			}
			same, err := sameContent(srcPath, dstPath, srcInfo, dstInfo)
			if err != nil {
				return fmt.Errorf("cannot compare %s: %w", rel, err)
			}
			if same {
				return nil
			}
			c.Reason = "contents differ"
		} else {
			if s.opts.PreventDelete {
				s.add(Change{Kind: '!', Path: rel, Note: needsDeleteNote})
				return nil
			}
			replace = true
			c.Reason = "destination is not a regular file, source is a file"
			if dstEntry.IsDir() {
				c.Reason = "destination is a directory, source is a file"
			}
		}
	}

	if !s.approve(c) {
		return nil
	}
	if replace {
		if err := s.removeDst(dstPath, rel); err != nil {
			return err
		}
	}
	if !s.opts.DryRun {
		if err := copyFile(srcPath, dstPath, srcInfo); err != nil {
			return fmt.Errorf("cannot copy %s: %w", rel, err)
		}
	}
	s.add(c)
	return nil
}

func (s *session) removeDst(dstPath, rel string) error {
	if s.opts.DryRun {
		return nil
	}
	if err := os.RemoveAll(dstPath); err != nil {
		return fmt.Errorf("cannot remove %s: %w", rel, err)
	}
	return nil
}

func NormalizeSkip(pattern string) (string, error) {
	p := filepath.ToSlash(pattern)
	p = strings.TrimPrefix(p, "./")
	p = strings.TrimSuffix(p, "/")
	if p == "" {
		return "", errors.New("skip pattern is empty")
	}
	if _, err := path.Match(p, ""); err != nil {
		return "", fmt.Errorf("invalid skip pattern %q: %w", pattern, err)
	}
	return p, nil
}

func matchesSkip(patterns []string, rel string) bool {
	for _, p := range patterns {
		target := path.Base(rel)
		if strings.Contains(p, "/") {
			target = rel
		}
		if ok, _ := path.Match(p, target); ok {
			return true
		}
	}
	return false
}

func sameContent(a, b string, aInfo, bInfo fs.FileInfo) (bool, error) {
	if aInfo.Size() != bInfo.Size() {
		return false, nil
	}

	fa, err := os.Open(a)
	if err != nil {
		return false, err
	}
	defer fa.Close()
	fb, err := os.Open(b)
	if err != nil {
		return false, err
	}
	defer fb.Close()

	bufA := make([]byte, 32*1024)
	bufB := make([]byte, 32*1024)
	for {
		nA, errA := io.ReadFull(fa, bufA)
		nB, errB := io.ReadFull(fb, bufB)
		if nA != nB || !bytes.Equal(bufA[:nA], bufB[:nB]) {
			return false, nil
		}
		if errA == nil && errB == nil {
			continue
		}
		if isEOF(errA) && isEOF(errB) {
			return true, nil
		}
		if errA != nil && !isEOF(errA) {
			return false, errA
		}
		if errB != nil && !isEOF(errB) {
			return false, errB
		}
		return false, nil
	}
}

func isEOF(err error) bool {
	return errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF)
}

func copyFile(srcPath, dstPath string, info fs.FileInfo) (err error) {
	in, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer in.Close()

	tmp, err := os.CreateTemp(filepath.Dir(dstPath), ".dios-tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	registerTempFile(tmpName)
	defer func() {
		unregisterTempFile(tmpName)
		if err != nil {
			_ = os.Remove(tmpName)
		}
	}()

	if _, err = io.Copy(tmp, in); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = os.Chmod(tmpName, info.Mode().Perm()); err != nil {
		return err
	}
	if err = os.Chtimes(tmpName, info.ModTime(), info.ModTime()); err != nil {
		return err
	}
	if err = os.Rename(tmpName, dstPath); err != nil {
		return err
	}
	return nil
}

func registerTempFile(name string) {
	tmpMu.Lock()
	defer tmpMu.Unlock()
	tracked[name] = struct{}{}
}

func unregisterTempFile(name string) {
	tmpMu.Lock()
	defer tmpMu.Unlock()
	delete(tracked, name)
}

func cleanupTrackedTempFiles() {
	tmpMu.Lock()
	paths := make([]string, 0, len(tracked))
	for name := range tracked {
		paths = append(paths, name)
	}
	tmpMu.Unlock()
	for _, path := range paths {
		_ = os.Remove(path)
	}
	tmpMu.Lock()
	tracked = map[string]struct{}{}
	tmpMu.Unlock()
}

func cleanupStaleTempDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("cannot clean temporary files in %s: %w", dir, err)
	}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), ".dios-tmp-") {
			continue
		}
		if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("cannot remove temporary file %s: %w", entry.Name(), err)
		}
	}
	return nil
}

func validatePaths(source, destination string) error {
	srcInfo, err := os.Stat(source)
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("source does not exist: %s", source)
	}
	if err != nil {
		return fmt.Errorf("cannot access source %s: %w", source, err)
	}
	if !srcInfo.IsDir() {
		return fmt.Errorf("source is not a directory: %s", source)
	}

	dstInfo, err := os.Stat(destination)
	dstExists := err == nil
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("cannot access destination %s: %w", destination, err)
	}
	if dstExists && !dstInfo.IsDir() {
		return fmt.Errorf("destination is not a directory: %s", destination)
	}
	if dstExists && os.SameFile(srcInfo, dstInfo) {
		return errSamePath
	}

	srcAbs, err := filepath.Abs(source)
	if err != nil {
		return fmt.Errorf("cannot resolve source %s: %w", source, err)
	}
	dstAbs, err := filepath.Abs(destination)
	if err != nil {
		return fmt.Errorf("cannot resolve destination %s: %w", destination, err)
	}
	switch {
	case samePath(srcAbs, dstAbs):
		return errSamePath
	case isInside(srcAbs, dstAbs):
		return errors.New("destination must not be inside source")
	case isInside(dstAbs, srcAbs):
		return errors.New("source must not be inside destination")
	}
	return nil
}

func samePath(a, b string) bool {
	if a == b {
		return true
	}
	if isCaseInsensitiveFS(filepath.Dir(a)) {
		return strings.EqualFold(a, b)
	}
	return false
}

func isInside(parent, child string) bool {
	if samePath(parent, child) {
		return false
	}
	if isCaseInsensitiveFS(parent) {
		parent = strings.ToLower(parent)
		child = strings.ToLower(child)
	}
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func ensureCaseSafe(source, destination string) error {
	if !isCaseInsensitiveFS(source) && !isCaseInsensitiveFS(destination) {
		return nil
	}
	for _, root := range []string{source, destination} {
		if _, err := os.Stat(root); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return fmt.Errorf("cannot inspect %s: %w", root, err)
		}
		if err := checkDirectoryCaseSafety(root); err != nil {
			return err
		}
	}
	return nil
}

func checkDirectoryCaseSafety(root string) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	seen := make(map[string]string, len(entries))
	for _, entry := range entries {
		key := strings.ToLower(entry.Name())
		if prev, ok := seen[key]; ok {
			return fmt.Errorf("case-insensitive name collision: %s and %s", prev, entry.Name())
		}
		seen[key] = entry.Name()
		if entry.IsDir() {
			if err := checkDirectoryCaseSafety(filepath.Join(root, entry.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}

func isCaseInsensitiveFS(dir string) bool {
	base, err := os.MkdirTemp(dir, "case-check-")
	if err != nil {
		return false
	}
	defer os.RemoveAll(base)
	lower := filepath.Join(base, "a.txt")
	upper := filepath.Join(base, "A.txt")
	if err := os.WriteFile(lower, []byte("x"), 0o600); err != nil {
		return false
	}
	_, err = os.Stat(upper)
	return err == nil
}
