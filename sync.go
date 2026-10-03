package main

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
)

var errSamePath = errors.New("source and destination must be different paths")

type change struct {
	kind   byte
	path   string
	isDir  bool
	reason string
	note   string
}

func (c change) label() string {
	if c.isDir {
		return c.path + "/"
	}
	return c.path
}

func (c change) detail() string {
	if c.note != "" {
		return c.note
	}
	return c.reason
}

func (c change) String() string {
	s := fmt.Sprintf("%c %s", c.kind, c.label())
	if c.note != "" {
		s += " (" + c.note + ")"
	}
	return s
}

type syncer struct {
	dryRun  bool
	changes []change
}

func syncDirs(source, destination string, dryRun bool) ([]change, error) {
	if err := validatePaths(source, destination); err != nil {
		return nil, err
	}

	s := &syncer{dryRun: dryRun}

	_, err := os.Stat(destination)
	dstExists := err == nil
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("cannot access destination %s: %w", destination, err)
	}
	if !dstExists {
		if !dryRun {
			if err := os.MkdirAll(destination, 0o755); err != nil {
				return nil, fmt.Errorf("cannot create destination %s: %w", destination, err)
			}
		}
		s.record('+', ".", true, "destination does not exist")
	}

	err = s.syncDir(source, destination, "", dstExists)
	return s.changes, err
}

func (s *syncer) record(kind byte, rel string, isDir bool, reason string) {
	s.changes = append(s.changes, change{kind: kind, path: rel, isDir: isDir, reason: reason})
}

func (s *syncer) syncDir(srcDir, dstDir, rel string, dstExists bool) error {
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
		err := s.syncEntry(srcEntry, dstByName[name], filepath.Join(srcDir, name), filepath.Join(dstDir, name), path.Join(rel, name))
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
		if err := s.removeDst(filepath.Join(dstDir, name), relPath); err != nil {
			return err
		}
		s.record('-', relPath, dstEntry.IsDir(), "not in source")
	}
	return nil
}

func (s *syncer) syncEntry(srcEntry, dstEntry fs.DirEntry, srcPath, dstPath, rel string) error {
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
		s.changes = append(s.changes, change{kind: '!', path: rel, note: note})
		return nil
	}
}

func (s *syncer) syncSubdir(srcEntry, dstEntry fs.DirEntry, srcPath, dstPath, rel string) error {
	dstExists := dstEntry != nil
	kind := byte('+')
	reason := "missing in destination"

	if dstExists && !dstEntry.IsDir() {
		if err := s.removeDst(dstPath, rel); err != nil {
			return err
		}
		dstExists = false
		kind = '~'
		reason = "destination is not a directory, source is a directory"
		if dstEntry.Type().IsRegular() {
			reason = "destination is a file, source is a directory"
		}
	}

	if !dstExists {
		if !s.dryRun {
			info, err := srcEntry.Info()
			if err != nil {
				return fmt.Errorf("cannot stat source directory %s: %w", srcPath, err)
			}
			if err := os.Mkdir(dstPath, info.Mode().Perm()|0o700); err != nil {
				return fmt.Errorf("cannot create directory %s: %w", rel, err)
			}
		}
		s.record(kind, rel, true, reason)
	}

	return s.syncDir(srcPath, dstPath, rel, dstExists)
}

func (s *syncer) syncFile(srcEntry, dstEntry fs.DirEntry, srcPath, dstPath, rel string) error {
	srcInfo, err := srcEntry.Info()
	if err != nil {
		return fmt.Errorf("cannot stat source file %s: %w", srcPath, err)
	}

	kind := byte('+')
	reason := "missing in destination"
	if dstEntry != nil {
		kind = '~'
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
			reason = "contents differ"
		} else {
			if err := s.removeDst(dstPath, rel); err != nil {
				return err
			}
			reason = "destination is not a regular file, source is a file"
			if dstEntry.IsDir() {
				reason = "destination is a directory, source is a file"
			}
		}
	}

	if !s.dryRun {
		if err := copyFile(srcPath, dstPath, srcInfo); err != nil {
			return fmt.Errorf("cannot copy %s: %w", rel, err)
		}
	}
	s.record(kind, rel, false, reason)
	return nil
}

func (s *syncer) removeDst(dstPath, rel string) error {
	if s.dryRun {
		return nil
	}
	if err := os.RemoveAll(dstPath); err != nil {
		return fmt.Errorf("cannot remove %s: %w", rel, err)
	}
	return nil
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
	defer func() {
		if err != nil {
			os.Remove(tmpName)
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
	return os.Rename(tmpName, dstPath)
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
	case srcAbs == dstAbs:
		return errSamePath
	case isInside(srcAbs, dstAbs):
		return errors.New("destination must not be inside source")
	case isInside(dstAbs, srcAbs):
		return errors.New("source must not be inside destination")
	}
	return nil
}

func isInside(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
