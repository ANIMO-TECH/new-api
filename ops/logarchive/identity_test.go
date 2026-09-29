package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFileGenerationSurvivesAppendAndRename(t *testing.T) {
	p := filepath.Join(t.TempDir(), "log")
	f, err := os.OpenFile(p, os.O_CREATE|os.O_RDWR, 0600)
	require.NoError(t, err)
	defer f.Close()
	initial, err := fileID("container-test", "docker", f)
	require.NoError(t, err)
	_, err = f.WriteString("new record\n")
	require.NoError(t, err)
	renamed := p + ".1"
	require.NoError(t, os.Rename(p, renamed))
	reopened, err := os.Open(renamed)
	require.NoError(t, err)
	defer reopened.Close()
	current, err := fileID("container-test", "docker", reopened)
	require.NoError(t, err)
	assert.Equal(t, initial, current)
	replacement, err := os.OpenFile(p, os.O_CREATE|os.O_RDWR, 0600)
	require.NoError(t, err)
	defer replacement.Close()
	newID, err := fileID("container-test", "docker", replacement)
	require.NoError(t, err)
	assert.NotEqual(t, initial, newID)
}
