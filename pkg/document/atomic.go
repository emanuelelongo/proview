package document

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func WriteFileAtomic(path string, data []byte) (err error) {
	if path == "" {
		return errors.New("document: empty output path")
	}
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".proview-*.tmp")
	if err != nil {
		return fmt.Errorf("document: create temporary output: %w", err)
	}
	temporaryName := temporary.Name()
	defer func() {
		if temporary != nil {
			err = errors.Join(err, temporary.Close())
		}
		if err != nil {
			_ = os.Remove(temporaryName)
		}
	}()

	if info, statErr := os.Stat(path); statErr == nil {
		if chmodErr := temporary.Chmod(info.Mode().Perm()); chmodErr != nil {
			return fmt.Errorf("document: preserve output permissions: %w", chmodErr)
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return fmt.Errorf("document: inspect output: %w", statErr)
	}
	if _, err := temporary.Write(data); err != nil {
		return fmt.Errorf("document: write temporary output: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("document: sync temporary output: %w", err)
	}
	if err := temporary.Close(); err != nil {
		temporary = nil
		return fmt.Errorf("document: close temporary output: %w", err)
	}
	temporary = nil
	if err := os.Rename(temporaryName, path); err != nil {
		return fmt.Errorf("document: replace output atomically: %w", err)
	}
	return nil
}
