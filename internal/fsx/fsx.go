package fsx

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

type RootDir struct {
	root *os.Root
	dir  string
}

func OpenRoot(dir string) (*RootDir, error) {
	r, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	return &RootDir{root: r, dir: dir}, nil
}

func (r *RootDir) Close() error {
	if r == nil || r.root == nil {
		return nil
	}
	return r.root.Close()
}

func (r *RootDir) Dir() string {
	return r.dir
}

func randomSuffix() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func (r *RootDir) AtomicWrite(rel string, data []byte, defaultPerm os.FileMode) error {
	if r == nil || r.root == nil {
		return errors.New("root directory is closed")
	}
	if rel == "" || filepath.IsAbs(rel) {
		return fmt.Errorf("invalid relative path: %q", rel)
	}
	perm := defaultPerm
	if info, err := r.root.Lstat(rel); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to overwrite symlink: %s", rel)
		}
		if info.IsDir() {
			return fmt.Errorf("refusing to overwrite directory: %s", rel)
		}
		perm = info.Mode().Perm()
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	parent := filepath.Dir(rel)
	base := filepath.Base(rel)
	var tmpRel string
	var f *os.File
	for range 16 {
		suffix, err := randomSuffix()
		if err != nil {
			return err
		}
		candidate := "." + base + "." + suffix
		if parent != "." {
			candidate = filepath.Join(parent, candidate)
		}
		file, err := r.root.OpenFile(candidate, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			tmpRel = candidate
			f = file
			break
		}
		if !errors.Is(err, os.ErrExist) {
			return err
		}
	}
	if f == nil {
		return fmt.Errorf("failed to allocate temp file for %s", rel)
	}

	cleanup := true
	defer func() {
		if cleanup {
			_ = r.root.Remove(tmpRel)
		}
	}()

	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if runtime.GOOS != "windows" {
		if err := f.Chmod(perm); err != nil {
			_ = f.Close()
			return err
		}
	}
	if err := f.Close(); err != nil {
		return err
	}
	if info, err := r.root.Lstat(rel); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing to overwrite symlink: %s", rel)
	}
	if err := r.root.Rename(tmpRel, rel); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func (r *RootDir) Lstat(rel string) (os.FileInfo, error) {
	if r == nil || r.root == nil {
		return nil, errors.New("root directory is closed")
	}
	return r.root.Lstat(rel)
}

func (r *RootDir) ReadFile(rel string) ([]byte, error) {
	if r == nil || r.root == nil {
		return nil, errors.New("root directory is closed")
	}
	info, err := r.root.Lstat(rel)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("unsafe symlink destination: %s", rel)
	}
	return r.root.ReadFile(rel)
}

func (r *RootDir) WriteNewNoClobber(rel string, data []byte, perm os.FileMode) error {
	if r == nil || r.root == nil {
		return errors.New("root directory is closed")
	}
	if rel == "" || filepath.IsAbs(rel) {
		return fmt.Errorf("unsafe attachment destination: %s", rel)
	}
	if info, err := r.root.Lstat(rel); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("unsafe attachment destination: %s", rel)
		}
		return os.ErrExist
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	parent := filepath.Dir(rel)
	var tmpRel string
	var f *os.File
	for range 16 {
		suffix, err := randomSuffix()
		if err != nil {
			return err
		}
		candidate := ".elport-" + suffix
		if parent != "." {
			candidate = filepath.Join(parent, candidate)
		}
		file, err := r.root.OpenFile(candidate, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			tmpRel = candidate
			f = file
			break
		}
		if !errors.Is(err, os.ErrExist) {
			return err
		}
	}
	if f == nil {
		return fmt.Errorf("failed to allocate temp file for %s", rel)
	}
	defer func() { _ = r.root.Remove(tmpRel) }()

	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if runtime.GOOS != "windows" {
		if err := f.Chmod(perm); err != nil {
			_ = f.Close()
			return err
		}
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := r.root.Link(tmpRel, rel); err == nil {
		return nil
	} else if errors.Is(err, os.ErrExist) {
		if info, lerr := r.root.Lstat(rel); lerr == nil && info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("unsafe attachment destination: %s", rel)
		}
		return os.ErrExist
	}

	dest, err := r.root.OpenFile(rel, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		if info, lerr := r.root.Lstat(rel); lerr == nil && info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("unsafe attachment destination: %s", rel)
		}
		return err
	}
	if _, err := dest.Write(data); err != nil {
		_ = dest.Close()
		_ = r.root.Remove(rel)
		return err
	}
	return dest.Close()
}

func writeAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if runtime.GOOS != "windows" {
		if err := tmp.Chmod(perm); err != nil {
			_ = tmp.Close()
			return err
		}
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func AtomicWriteFile(path string, data []byte, defaultPerm os.FileMode) error {
	perm := defaultPerm
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink == 0 {
		perm = info.Mode().Perm()
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return writeAtomic(path, data, perm)
}

func AtomicWriteFileFixedPerm(path string, data []byte, perm os.FileMode) error {
	return writeAtomic(path, data, perm)
}

func HomeDir() (string, error) {
	return os.UserHomeDir()
}

func ConfigDir() (string, error) {
	home, err := HomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "elport"), nil
}
