package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContainerLogDirectoryCannotEscapeViaSymlink(t *testing.T) {
	containerRoot := t.TempDir()
	outside := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(containerRoot, "data"), 0700))
	require.NoError(t, os.Symlink(outside, filepath.Join(containerRoot, "data", "logs")))
	root, err := openContainerLogsRoot(containerRoot)
	assert.Error(t, err)
	assert.Nil(t, root)
}

func TestPinnedLogDirectoryDoesNotFollowReplacement(t *testing.T) {
	containerRoot := t.TempDir()
	logs := filepath.Join(containerRoot, "data", "logs")
	require.NoError(t, os.MkdirAll(logs, 0700))
	root, err := openContainerLogsRoot(containerRoot)
	require.NoError(t, err)
	defer root.Close()
	const name = "oneapi-20260925004822.log"
	require.NoError(t, root.WriteFile(name, []byte("original"), 0600))
	require.NoError(t, os.Rename(logs, logs+"-old"))
	out := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(out, name), []byte("unrelated important data"), 0600))
	require.NoError(t, os.Symlink(out, logs))
	data, err := root.ReadFile(name)
	require.NoError(t, err)
	assert.Equal(t, "original", string(data))
	require.NoError(t, root.Remove(name))
	data, err = os.ReadFile(filepath.Join(out, name))
	require.NoError(t, err)
	assert.Equal(t, "unrelated important data", string(data))
}
