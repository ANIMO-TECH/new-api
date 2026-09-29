package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type memoryStore struct {
	objects map[string][]byte
	failPut bool
	failGet bool
	corrupt bool
	puts    int
}

func (s *memoryStore) Put(_ context.Context, key string, b []byte) error {
	if s.failPut {
		return errors.New("upload unavailable")
	}
	s.objects[key] = bytes.Clone(b)
	s.puts++
	return nil
}

func TestActiveSnapshotRestoresItsExactPrefixButCannotAuthorizeDeletion(t *testing.T) {
	s, f, m, now := archiveFixture(t, 2048, 96*time.Hour)
	state, err := archiveSnapshot(context.Background(), s, "logs/test", f, m, false, now)
	require.NoError(t, err)
	_, err = restoreManifest(context.Background(), s, state.ManifestKey, io.Discard)
	assert.Error(t, err)
	puts := s.puts
	var restored bytes.Buffer
	snapshot, err := restoreArchive(context.Background(), s, state.ManifestKey, &restored, false)
	require.NoError(t, err)
	assert.False(t, snapshot.Complete)
	assert.Equal(t, puts, s.puts, "recovery is read-only")
	original, err := os.ReadFile(f.Name())
	require.NoError(t, err)
	assert.Equal(t, original, restored.Bytes())
	removed, err := deleteVerifiedFile(context.Background(), s, testRoot(t, f.Name()), filepath.Base(f.Name()), state, now, true, func() (bool, error) { return false, nil })
	assert.NoError(t, err)
	assert.False(t, removed)
}

func (s *memoryStore) Get(_ context.Context, key string) (io.ReadCloser, error) {
	if s.failGet {
		return nil, errors.New("recovery unavailable")
	}
	b, ok := s.objects[key]
	if !ok {
		return nil, os.ErrNotExist
	}
	b = bytes.Clone(b)
	if s.corrupt && len(b) > 4 {
		b[len(b)/2] ^= 0x7f
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

func archiveFixture(t *testing.T, size int, age time.Duration) (*memoryStore, *os.File, manifest, time.Time) {
	t.Helper()
	s := &memoryStore{objects: map[string][]byte{}}
	p := filepath.Join(t.TempDir(), "oneapi-20260925004822.log")
	data := bytes.Repeat([]byte("archive recovery test record\n"), size/29+1)[:size]
	require.NoError(t, os.WriteFile(p, data, 0600))
	now := time.Now().UTC()
	require.NoError(t, os.Chtimes(p, now.Add(-age), now.Add(-age)))
	f, err := os.OpenFile(p, os.O_RDWR, 0)
	require.NoError(t, err)
	t.Cleanup(func() { f.Close() })
	id, err := fileID("container-test", "app", f)
	require.NoError(t, err)
	m := manifest{SourceID: id, ContainerID: "container-test", Kind: "app", Name: filepath.Base(p)}
	return s, f, m, now
}

func TestRetentionRefusesReplacedFileEvenWithIdenticalContentAndDate(t *testing.T) {
	s, f, m, now := archiveFixture(t, 2048, 96*time.Hour)
	state, err := archiveSnapshot(context.Background(), s, "logs/test", f, m, true, now)
	require.NoError(t, err)
	data, err := os.ReadFile(f.Name())
	require.NoError(t, err)
	require.NoError(t, os.Remove(f.Name()))
	require.NoError(t, os.WriteFile(f.Name(), data, 0600))
	require.NoError(t, os.Chtimes(f.Name(), state.Manifest.ModifiedAt, state.Manifest.ModifiedAt))
	removed, err := deleteVerifiedFile(context.Background(), s, testRoot(t, f.Name()), filepath.Base(f.Name()), state, now, true, func() (bool, error) { return false, nil })
	assert.Error(t, err)
	assert.False(t, removed)
	_, err = os.Stat(f.Name())
	assert.NoError(t, err)
}

func TestOpenSourceCanBeArchivedAfterExternalUnlink(t *testing.T) {
	s, f, m, now := archiveFixture(t, 2048, 0)
	data, err := os.ReadFile(f.Name())
	require.NoError(t, err)
	require.NoError(t, os.Remove(f.Name()))
	state, err := archiveSnapshot(context.Background(), s, "logs/test", f, m, true, now)
	require.NoError(t, err)
	var restored bytes.Buffer
	_, err = restoreManifest(context.Background(), s, state.ManifestKey, &restored)
	require.NoError(t, err)
	assert.Equal(t, data, restored.Bytes())
}

func TestArchiveAndRestoreCompleteFileAcrossChunks(t *testing.T) {
	s, f, prior, now := archiveFixture(t, chunkSize+12345, 96*time.Hour)
	state, err := archiveSnapshot(context.Background(), s, "logs/test", f, prior, true, now)
	require.NoError(t, err)
	require.True(t, state.Manifest.Complete)
	require.Len(t, state.Manifest.Chunks, 2)
	var restored bytes.Buffer
	m, err := restoreManifest(context.Background(), s, state.ManifestKey, &restored)
	require.NoError(t, err)
	original, err := os.ReadFile(f.Name())
	require.NoError(t, err)
	assert.Equal(t, original, restored.Bytes())
	assert.Equal(t, digest(original), m.SHA256)
}

func TestIncrementalArchiveResumesWithoutDuplicatingBytes(t *testing.T) {
	s, f, m, now := archiveFixture(t, 120, 0)
	first, err := archiveSnapshot(context.Background(), s, "logs/test", f, m, false, now)
	require.NoError(t, err)
	assert.False(t, first.Manifest.Complete)
	_, err = f.WriteAt([]byte("new bytes"), 120)
	require.NoError(t, err)
	second, err := archiveSnapshot(context.Background(), s, "logs/test", f, first.Manifest, true, now.Add(time.Minute))
	require.NoError(t, err)
	require.Len(t, second.Manifest.Chunks, 2)
	assert.Equal(t, first.Manifest.Chunks[0], second.Manifest.Chunks[0])
	var restored bytes.Buffer
	_, err = restoreManifest(context.Background(), s, second.ManifestKey, &restored)
	require.NoError(t, err)
	original, err := os.ReadFile(f.Name())
	require.NoError(t, err)
	assert.Equal(t, original, restored.Bytes())
}

func TestArchiveFailuresNeverYieldDeletionProof(t *testing.T) {
	for _, failure := range []string{"upload", "download", "corruption"} {
		t.Run(failure, func(t *testing.T) {
			s, f, m, now := archiveFixture(t, 1024, 96*time.Hour)
			s.failPut, s.failGet, s.corrupt = failure == "upload", failure == "download", failure == "corruption"
			state, err := archiveSnapshot(context.Background(), s, "logs/test", f, m, true, now)
			require.Error(t, err)
			assert.Empty(t, state.ManifestKey)
			assert.False(t, state.Manifest.Complete)
			_, err = os.Stat(f.Name())
			assert.NoError(t, err)
		})
	}
}

func TestRetentionRequiresAgeArchiveAndNoWriter(t *testing.T) {
	for _, scenario := range []string{"disabled", "young", "active", "check-failed", "archive-unavailable", "archive-corrupt", "source-changed", "verified-old"} {
		t.Run(scenario, func(t *testing.T) {
			age := 96 * time.Hour
			if scenario == "young" {
				age = 71 * time.Hour
			}
			s, f, m, now := archiveFixture(t, 4096, age)
			state, err := archiveSnapshot(context.Background(), s, "logs/test", f, m, true, now)
			require.NoError(t, err)
			if scenario == "archive-unavailable" {
				s.failGet = true
			}
			if scenario == "archive-corrupt" {
				s.corrupt = true
			}
			if scenario == "source-changed" {
				_, err := f.WriteAt([]byte("changed"), 0)
				require.NoError(t, err)
			}
			active := func() (bool, error) {
				if scenario == "check-failed" {
					return false, errors.New("unable to verify writers")
				}
				return scenario == "active", nil
			}
			removed, err := deleteVerifiedFile(context.Background(), s, testRoot(t, f.Name()), filepath.Base(f.Name()), state, now, scenario != "disabled", active)
			if scenario == "verified-old" {
				require.NoError(t, err)
				assert.True(t, removed)
				_, err = os.Stat(f.Name())
				assert.ErrorIs(t, err, os.ErrNotExist)
			} else {
				assert.False(t, removed)
				_, statErr := os.Stat(f.Name())
				assert.NoError(t, statErr)
			}
		})
	}
}

func TestRetentionRejectsSymlinkAndDockerLogs(t *testing.T) {
	s, f, m, now := archiveFixture(t, 100, 96*time.Hour)
	state, err := archiveSnapshot(context.Background(), s, "logs/test", f, m, true, now)
	require.NoError(t, err)
	p := filepath.Join(t.TempDir(), filepath.Base(f.Name()))
	require.NoError(t, os.Symlink(f.Name(), p))
	removed, err := deleteVerifiedFile(context.Background(), s, testRoot(t, p), filepath.Base(p), state, now, true, func() (bool, error) { return false, nil })
	assert.Error(t, err)
	assert.False(t, removed)
	state.Manifest.Kind = "docker"
	removed, err = deleteVerifiedFile(context.Background(), s, testRoot(t, f.Name()), filepath.Base(f.Name()), state, now, true, func() (bool, error) { return false, nil })
	assert.NoError(t, err)
	assert.False(t, removed)
	_, err = os.Stat(f.Name())
	assert.NoError(t, err)
}

func testRoot(t *testing.T, name string) *os.Root {
	t.Helper()
	root, err := os.OpenRoot(filepath.Dir(name))
	require.NoError(t, err)
	t.Cleanup(func() { root.Close() })
	return root
}
