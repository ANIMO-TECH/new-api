package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplicationLogHeldByOtherProcessIsProtected(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "log")
	require.NoError(t, os.WriteFile(logPath, []byte("important log"), 0600))
	info, err := os.Stat(logPath)
	require.NoError(t, err)
	proc := filepath.Join(dir, "proc")
	require.NoError(t, os.MkdirAll(filepath.Join(proc, "100", "fd"), 0700))
	require.NoError(t, os.Symlink(logPath, filepath.Join(proc, "100", "fd", "4")))
	active, err := hostFileActiveAt(info, proc, 200)
	require.NoError(t, err)
	assert.True(t, active, "collector/other-container readers must prevent deletion")
	active, err = hostFileActiveAt(info, proc, 100)
	require.NoError(t, err)
	assert.False(t, active, "the archive worker's own verified read is excluded")
}

func TestUnknownDescriptorAccessDoesNotMeanUnused(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "log")
	require.NoError(t, os.WriteFile(logPath, []byte("important log"), 0600))
	info, err := os.Stat(logPath)
	require.NoError(t, err)
	proc := filepath.Join(dir, "proc")
	fdDir := filepath.Join(proc, "100", "fd")
	require.NoError(t, os.MkdirAll(fdDir, 0700))
	require.NoError(t, os.Symlink("4", filepath.Join(fdDir, "4")))
	active, err := hostFileActiveAt(info, proc, 200)
	assert.Error(t, err)
	assert.False(t, active)
}

func TestRetiredDescriptorDoesNotFollowReusedRotationPath(t *testing.T) {
	p := filepath.Join(t.TempDir(), "container-json.log.4")
	require.NoError(t, os.WriteFile(p, []byte("old archived bytes"), 0600))
	f, err := os.Open(p)
	require.NoError(t, err)
	defer f.Close()
	info, err := f.Stat()
	require.NoError(t, err)
	retired, err := sourcePathRetired(info, p)
	require.NoError(t, err)
	assert.False(t, retired)
	require.NoError(t, os.Remove(p))
	require.NoError(t, os.WriteFile(p, []byte("new live bytes"), 0600))
	retired, err = sourcePathRetired(info, p)
	require.NoError(t, err)
	assert.True(t, retired)
	data, err := os.ReadFile(p)
	require.NoError(t, err)
	assert.Equal(t, "new live bytes", string(data))
}
