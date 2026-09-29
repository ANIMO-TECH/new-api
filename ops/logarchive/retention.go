package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"syscall"
	"time"
)

var appLogName = regexp.MustCompile(`^oneapi-[0-9]{14}(?:\.[0-9]{9}-[0-9]{3})?\.log$`)

// deleteVerifiedFile has no size-based override. An old file can be removed only
// after current-source and independently restored remote content match, and the
// caller has rechecked that no container process is writing it.
func deleteVerifiedFile(ctx context.Context, store objectStore, root *os.Root, fileName string, state archiveState, now time.Time, enabled bool, isActive func() (bool, error)) (bool, error) {
	if !enabled || state.Manifest.Kind != "app" || !state.Manifest.Complete || state.ManifestKey == "" {
		return false, nil
	}
	if fileName != filepath.Base(fileName) || !appLogName.MatchString(fileName) {
		return false, errors.New("refusing retention outside application log naming contract")
	}
	active, err := isActive()
	if err != nil || active {
		return false, err
	}
	before, err := root.Lstat(fileName)
	if err != nil {
		return false, err
	}
	if !before.Mode().IsRegular() {
		return false, errors.New("refusing non-regular log")
	}
	if now.Before(before.ModTime().Add(72 * time.Hour)) {
		return false, nil
	}
	if before.Size() != state.Manifest.Size || !before.ModTime().Equal(state.Manifest.ModifiedAt) {
		return false, errors.New("source metadata changed since verification")
	}
	f, err := root.OpenFile(fileName, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return false, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return false, err
	}
	if !os.SameFile(before, opened) {
		return false, errors.New("source identity changed")
	}
	identity, err := fileID(state.Manifest.ContainerID, state.Manifest.Kind, f)
	if err != nil || identity != state.Manifest.SourceID {
		return false, errors.New("source generation does not match verified archive")
	}
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return false, err
	}
	if hex.EncodeToString(h.Sum(nil)) != state.Manifest.SHA256 {
		return false, errors.New("source checksum changed; retaining file")
	}
	restored, err := restoreManifest(ctx, store, state.ManifestKey, io.Discard)
	if err != nil {
		return false, err
	}
	if restored.SourceID != state.Manifest.SourceID || restored.Size != before.Size() || restored.SHA256 != state.Manifest.SHA256 {
		return false, errors.New("restored archive does not match source")
	}
	active, err = isActive()
	if err != nil || active {
		return false, err
	}
	final, err := root.Lstat(fileName)
	if err != nil {
		return false, err
	}
	if !os.SameFile(before, final) || final.Size() != before.Size() || !final.ModTime().Equal(before.ModTime()) {
		return false, errors.New("source changed before retention; retaining file")
	}
	if err := root.Remove(fileName); err != nil {
		return false, err
	}
	return true, nil
}
