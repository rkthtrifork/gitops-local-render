package render

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func pathInside(root, requested string) (string, error) {
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("resolve source root %q: %w", root, err)
	}
	if !filepath.IsAbs(requested) {
		requested = filepath.Join(root, requested)
	}
	requested = filepath.Clean(requested)

	resolved := requested
	if _, err := os.Lstat(requested); err == nil {
		resolved, err = filepath.EvalSymlinks(requested)
		if err != nil {
			return "", fmt.Errorf("resolve path %q: %w", requested, err)
		}
	} else if !os.IsNotExist(err) {
		return "", err
	}

	relative, err := filepath.Rel(root, resolved)
	if err != nil {
		return "", err
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return "", fmt.Errorf("path %q escapes local source root %q", requested, root)
	}
	return resolved, nil
}
