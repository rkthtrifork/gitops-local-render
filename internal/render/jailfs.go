package render

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"sigs.k8s.io/kustomize/kyaml/filesys"
)

// jailFS permits Kustomize's normal cross-directory bases while preventing a
// resource from reading or writing outside its declared local source root.
type jailFS struct {
	root string
	disk filesys.FileSystem
}

func newJailFS(root string) (*jailFS, error) {
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	return &jailFS{root: resolved, disk: filesys.MakeFsOnDisk()}, nil
}

func (f *jailFS) Create(path string) (filesys.File, error) {
	path, err := f.resolveForWrite(path)
	if err != nil {
		return nil, err
	}
	return f.disk.Create(path)
}

func (f *jailFS) Mkdir(path string) error {
	path, err := f.resolveForWrite(path)
	if err != nil {
		return err
	}
	return f.disk.Mkdir(path)
}

func (f *jailFS) MkdirAll(path string) error {
	path, err := f.resolveForWrite(path)
	if err != nil {
		return err
	}
	return f.disk.MkdirAll(path)
}

func (f *jailFS) RemoveAll(path string) error {
	path, err := f.resolveForWrite(path)
	if err != nil {
		return err
	}
	if path == f.root {
		return fmt.Errorf("refusing to remove source root %q", f.root)
	}
	return f.disk.RemoveAll(path)
}

func (f *jailFS) Open(path string) (filesys.File, error) {
	path, err := f.resolveExisting(path)
	if err != nil {
		return nil, err
	}
	return f.disk.Open(path)
}

func (f *jailFS) IsDir(path string) bool {
	path, err := f.resolveExisting(path)
	return err == nil && f.disk.IsDir(path)
}

func (f *jailFS) ReadDir(path string) ([]string, error) {
	path, err := f.resolveExisting(path)
	if err != nil {
		return nil, err
	}
	return f.disk.ReadDir(path)
}

func (f *jailFS) CleanedAbs(path string) (filesys.ConfirmedDir, string, error) {
	path, err := f.resolveExisting(path)
	if err != nil {
		return "", "", err
	}
	return f.disk.CleanedAbs(path)
}

func (f *jailFS) Exists(path string) bool {
	path, err := f.resolveExisting(path)
	return err == nil && f.disk.Exists(path)
}

func (f *jailFS) Glob(pattern string) ([]string, error) {
	prefix := pattern
	if index := strings.IndexAny(prefix, "*[?"); index >= 0 {
		prefix = prefix[:index]
	}
	if prefix == "" {
		prefix = "."
	}
	if _, err := f.resolveForWrite(prefix); err != nil {
		return nil, err
	}
	if !filepath.IsAbs(pattern) {
		pattern = filepath.Join(f.root, pattern)
	}
	matches, err := f.disk.Glob(pattern)
	if err != nil {
		return nil, err
	}
	for _, match := range matches {
		if _, err := f.resolveExisting(match); err != nil {
			return nil, err
		}
	}
	return matches, nil
}

func (f *jailFS) ReadFile(path string) ([]byte, error) {
	path, err := f.resolveExisting(path)
	if err != nil {
		return nil, err
	}
	return f.disk.ReadFile(path)
}

func (f *jailFS) WriteFile(path string, data []byte) error {
	path, err := f.resolveForWrite(path)
	if err != nil {
		return err
	}
	return f.disk.WriteFile(path, data)
}

func (f *jailFS) Walk(path string, walkFn filepath.WalkFunc) error {
	path, err := f.resolveExisting(path)
	if err != nil {
		return err
	}
	return f.disk.Walk(path, walkFn)
}

func (f *jailFS) resolveExisting(path string) (string, error) {
	if !filepath.IsAbs(path) {
		path = filepath.Join(f.root, path)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	return f.ensureInside(resolved)
}

func (f *jailFS) resolveForWrite(path string) (string, error) {
	if !filepath.IsAbs(path) {
		path = filepath.Join(f.root, path)
	}
	path = filepath.Clean(path)
	if _, err := os.Lstat(path); err == nil {
		return f.resolveExisting(path)
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return "", err
	}
	parent, err = f.ensureInside(parent)
	if err != nil {
		return "", err
	}
	return filepath.Join(parent, filepath.Base(path)), nil
}

func (f *jailFS) ensureInside(path string) (string, error) {
	relative, err := filepath.Rel(f.root, path)
	if err != nil {
		return "", err
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return "", fmt.Errorf("path %q escapes local source root %q", path, f.root)
	}
	return path, nil
}
