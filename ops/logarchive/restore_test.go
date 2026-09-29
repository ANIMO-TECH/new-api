package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRestorePublishesVerifiedFileWithoutOverwriting(t *testing.T) {
	s, f, m, now := archiveFixture(t, 4096, 96*time.Hour)
	state, err := archiveSnapshot(context.Background(), s, "logs/test", f, m, true, now)
	require.NoError(t, err)
	destination := filepath.Join(t.TempDir(), "restored.log")
	restored, err := restoreToFile(context.Background(), s, state.ManifestKey, destination, true)
	require.NoError(t, err)
	data, err := os.ReadFile(destination)
	require.NoError(t, err)
	assert.Equal(t, restored.SHA256, digest(data))
	require.NoError(t, os.WriteFile(destination, []byte("unrelated important data"), 0600))
	_, err = restoreToFile(context.Background(), s, state.ManifestKey, destination, true)
	assert.ErrorIs(t, err, os.ErrExist)
	data, err = os.ReadFile(destination)
	require.NoError(t, err)
	assert.Equal(t, "unrelated important data", string(data))
}

func TestFailedRestoreDoesNotPublishPartialFile(t *testing.T) {
	s, f, m, now := archiveFixture(t, 4096, 96*time.Hour)
	state, err := archiveSnapshot(context.Background(), s, "logs/test", f, m, true, now)
	require.NoError(t, err)
	s.corrupt = true
	dir := t.TempDir()
	destination := filepath.Join(dir, "restored.log")
	_, err = restoreToFile(context.Background(), s, state.ManifestKey, destination, true)
	require.Error(t, err)
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Empty(t, entries)
}
