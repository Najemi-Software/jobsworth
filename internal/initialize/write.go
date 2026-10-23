package initialize

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ErrExists is returned when the target exists and force is false.
var ErrExists = errors.New("file exists")

// WriteFile writes content atomically (temp file in the same directory,
// fsync, rename). Without force an existing target is ErrExists; parent
// directories are created.
func WriteFile(path string, content []byte, force bool) error {
	if _, err := os.Stat(path); err == nil && !force {
		return fmt.Errorf("%s: %w", path, ErrExists)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	fail := func(err error) error {
		_ = tmp.Close()
		_ = os.Remove(name)
		return err
	}
	if _, err := tmp.Write(content); err != nil {
		return fail(err)
	}
	if err := tmp.Sync(); err != nil {
		return fail(err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := os.Chmod(name, 0o644); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		_ = os.Remove(name)
		return err
	}
	return nil
}

// TargetRoot resolves where the config goes (§2): the git repository root
// containing dir, or dir itself when not in a repository. dir must be an
// existing directory.
func TargetRoot(dir string, gitRoot func(string) (string, bool)) (string, error) {
	st, err := os.Stat(dir)
	if err != nil {
		return "", err
	}
	if !st.IsDir() {
		return "", fmt.Errorf("%s is not a directory", dir)
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	if gitRoot != nil {
		if root, ok := gitRoot(abs); ok {
			return root, nil
		}
	}
	return abs, nil
}
