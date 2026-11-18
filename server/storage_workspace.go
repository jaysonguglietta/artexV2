package server

import (
	"errors"
	"github.com/Autumn-27/artex/storage"
	"os"
	"path/filepath"
	"strings"
)

// Generic file management cannot modify evidence databases, legal-hold state or
// archive internals. Reject in-root symlink aliases as well as obvious prefixes.
func (s *Server) workspaceMutationAllowed(absolute string) error {
	relative, err := filepath.Rel(s.m.dir, absolute)
	if err != nil || relative == "." || !filepath.IsLocal(relative) {
		return errors.New("invalid workspace mutation path")
	}
	components := strings.Split(filepath.ToSlash(relative), "/")
	if strings.HasPrefix(components[0], "_") {
		return errors.New("storage control/staging paths are not mutable through the workspace API")
	}
	switch components[0] {
	case "traffic", "evidence", "archives", "exports", "transcripts", "private", "config.json", ".delete-staging", "_quota.lock", "_storage-holds.json", "_storage-holds.next":
		return errors.New("managed storage paths are read-only in the workspace; use the storage or archive API")
	}
	root, err := os.OpenRoot(s.m.dir)
	if err != nil {
		return err
	}
	defer root.Close()
	for index := range components {
		prefix := filepath.FromSlash(strings.Join(components[:index+1], "/"))
		info, err := root.Lstat(prefix)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("workspace mutations cannot traverse symlink aliases")
		}
	}
	return nil
}
func workspaceDelete(root *os.Root, relative, absolute string) error {
	if bucket := storage.ForPath(absolute); bucket != nil {
		return bucket.WithUnheld(func() error { return root.RemoveAll(relative) })
	}
	return root.RemoveAll(relative)
}

func canonicalStoragePath(value string) (string, error) {
	absolute, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	var missing []string
	for {
		resolved, err := filepath.EvalSymlinks(absolute)
		if err == nil {
			for index := len(missing) - 1; index >= 0; index-- {
				resolved = filepath.Join(resolved, missing[index])
			}
			return filepath.Clean(resolved), nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(absolute)
		if parent == absolute {
			return "", err
		}
		missing = append(missing, filepath.Base(absolute))
		absolute = parent
	}
}
func (s *Server) checkSkillManagedMutation(absolute string) error {
	if s.m == nil {
		return nil
	}
	dataRoot, err := canonicalStoragePath(s.m.dir)
	if err != nil {
		return err
	}
	skillPath, err := canonicalStoragePath(absolute)
	if err != nil {
		return err
	}
	inside := func(base, candidate string) bool {
		relative, err := filepath.Rel(base, candidate)
		return err == nil && (relative == "." || filepath.IsLocal(relative))
	}
	if inside(dataRoot, skillPath) || inside(skillPath, dataRoot) {
		return errors.New("skills must not overlap managed data roots or their ancestors; source/skill trees require their own reviewed capacity policy")
	}
	return nil
}
