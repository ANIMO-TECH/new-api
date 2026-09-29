package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRestoreRejectsSubstitutedManifestEvenWhenItsChecksumsAgree(t *testing.T) {
	for _, scenario := range []string{"replaced-content", "forged-completion"} {
		t.Run(scenario, func(t *testing.T) {
			store, source, prior, now := archiveFixture(t, 1024, 96*time.Hour)
			state, err := archiveSnapshot(context.Background(), store, "logs/test", source, prior, scenario != "forged-completion", now)
			require.NoError(t, err)
			original, err := os.ReadFile(source.Name())
			require.NoError(t, err)
			m := state.Manifest
			if scenario == "replaced-content" {
				// A writer outside this worker replaces both the manifest and chunk.
				// Their internal hashes agree, but the caller still uses the original receipt key.
				replacement := bytes.Repeat([]byte("x"), len(original))
				var compressed bytes.Buffer
				z := gzip.NewWriter(&compressed)
				_, err := z.Write(replacement)
				require.NoError(t, err)
				require.NoError(t, z.Close())
				store.objects[m.Chunks[0].Key] = compressed.Bytes()
				m.Chunks[0].SHA256 = digest(replacement)
				m.SHA256 = digest(replacement)
			} else {
				// An archived active prefix must not become a complete file merely
				// because its completion marker and whole-file checksum are rewritten.
				m.Complete = true
				m.SHA256 = digest(original)
			}
			store.objects[state.ManifestKey], err = json.Marshal(m)
			require.NoError(t, err)
			directory := t.TempDir()
			_, err = restoreToFile(context.Background(), store, state.ManifestKey, filepath.Join(directory, "recovered.log"), true)
			require.Error(t, err, "a restore must remain bound to the original manifest receipt")
			entries, err := os.ReadDir(directory)
			require.NoError(t, err)
			assert.Empty(t, entries, "do not publish substituted contents or leave a partial recovery")
		})
	}
}

func TestRestoreRejectsManifestWithDataBeyondReadLimit(t *testing.T) {
	store, source, prior, now := archiveFixture(t, 32, 96*time.Hour)
	state, err := archiveSnapshot(context.Background(), store, "logs/test", source, prior, true, now)
	require.NoError(t, err)
	// A valid JSON document followed by a large suffix must not be silently
	// truncated to a valid prefix and accepted as the original receipt.
	store.objects[state.ManifestKey] = append(store.objects[state.ManifestKey], bytes.Repeat([]byte(" "), 8<<20)...)
	directory := t.TempDir()
	_, err = restoreToFile(context.Background(), store, state.ManifestKey, filepath.Join(directory, "recovered.log"), true)
	require.Error(t, err)
	entries, err := os.ReadDir(directory)
	require.NoError(t, err)
	assert.Empty(t, entries)
}
