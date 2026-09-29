//go:build linux

package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLinuxBirthTimeDistinguishesGenerationFromModifiedDate(t *testing.T) {
	p := filepath.Join(t.TempDir(), "log")
	f, err := os.Create(p)
	require.NoError(t, err)
	defer f.Close()
	initial, err := fileID("container-test", "app", f)
	require.NoError(t, err)
	past := time.Now().Add(-96 * time.Hour)
	require.NoError(t, os.Chtimes(p, past, past))
	current, err := fileID("container-test", "app", f)
	require.NoError(t, err)
	assert.Equal(t, initial, current)
}
