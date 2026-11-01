package config

import (
	"io"
	"os"
	"path/filepath"
)

// writeFileAtomically keeps the last complete file if a read, write or rename
// fails. The temporary file lives beside the destination so rename is atomic.
func writeFileAtomically(path string, reader io.Reader) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".libclash-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if _, err := io.Copy(file, reader); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}
