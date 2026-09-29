package main

import (
	"context"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
)

func restoreToFile(ctx context.Context, store objectStore, key, destination string, requireClosed bool) (manifest, error) {
	directory, name := filepath.Dir(destination), filepath.Base(destination)
	root, err := os.OpenRoot(directory)
	if err != nil {
		return manifest{}, err
	}
	defer root.Close()
	if _, err := root.Lstat(name); err == nil {
		return manifest{}, os.ErrExist
	} else if !os.IsNotExist(err) {
		return manifest{}, err
	}
	temporary := ".newapi-restore-" + rand.Text()
	f, err := root.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return manifest{}, err
	}
	defer root.Remove(temporary)
	m, restoreErr := restoreArchive(ctx, store, key, f, requireClosed)
	if restoreErr == nil {
		restoreErr = f.Sync()
	}
	closeErr := f.Close()
	if err := errors.Join(restoreErr, closeErr); err != nil {
		return manifest{}, err
	}
	// Link publishes the verified inode atomically and fails if another writer
	// created the destination meanwhile. Rename would silently overwrite it.
	if err := root.Link(temporary, name); err != nil {
		return manifest{}, err
	}
	dir, err := root.Open(".")
	if err != nil {
		return manifest{}, err
	}
	defer dir.Close()
	if err := dir.Sync(); err != nil {
		return manifest{}, err
	}
	return m, nil
}
