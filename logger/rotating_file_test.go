package logger

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRotatingFileSizeAndTimePreserveRecords(t *testing.T) {
	for _, reason := range []string{"size", "age"} {
		t.Run(reason, func(t *testing.T) {
			dir := t.TempDir()
			w, err := newRotatingFile(dir, 12, time.Hour)
			require.NoError(t, err)
			defer w.Close()
			start := w.openedAt
			w.now = func() time.Time { return start }
			first := w.CurrentPath()
			n, err := w.Write([]byte("first-record"))
			require.NoError(t, err)
			require.Equal(t, 12, n)
			if reason == "age" {
				w.maxBytes = 100
				w.now = func() time.Time { return start.Add(time.Hour) }
			}
			_, err = w.Write([]byte("second-record"))
			require.NoError(t, err)
			second := w.CurrentPath()
			require.NotEqual(t, first, second)
			b, err := os.ReadFile(first)
			require.NoError(t, err)
			assert.Equal(t, "first-record", string(b))
			b, err = os.ReadFile(second)
			require.NoError(t, err)
			assert.Equal(t, "second-record", string(b))
		})
	}
}

func TestRotatingFileConcurrentWritesSurviveAllRotations(t *testing.T) {
	dir := t.TempDir()
	w, err := newRotatingFile(dir, 128, time.Hour)
	require.NoError(t, err)
	var wg sync.WaitGroup
	errorsSeen := make(chan error, 12)
	for worker := 0; worker < 12; worker++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for seq := 0; seq < 100; seq++ {
				if _, err := fmt.Fprintf(w, "%02d:%03d\n", id, seq); err != nil {
					errorsSeen <- err
					return
				}
			}
		}(worker)
	}
	wg.Wait()
	close(errorsSeen)
	for err := range errorsSeen {
		require.NoError(t, err)
	}
	require.NoError(t, w.Close())
	files, err := filepath.Glob(filepath.Join(dir, "oneapi-*.log"))
	require.NoError(t, err)
	require.Greater(t, len(files), 1)
	counts := map[string]int{}
	for _, path := range files {
		b, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.LessOrEqual(t, len(b), 128)
		for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
			counts[line]++
		}
	}
	require.Len(t, counts, 1200)
	for worker := 0; worker < 12; worker++ {
		for seq := 0; seq < 100; seq++ {
			assert.Equal(t, 1, counts[fmt.Sprintf("%02d:%03d", worker, seq)])
		}
	}
}

func TestFailedRotationAppendsToOldFileAndRecovers(t *testing.T) {
	w, err := newRotatingFile(t.TempDir(), 3, time.Hour)
	require.NoError(t, err)
	defer w.Close()
	_, err = w.Write([]byte("one"))
	require.NoError(t, err)
	original := w.CurrentPath()
	w.openFile = func(string, int, os.FileMode) (*os.File, error) { return nil, os.ErrPermission }
	n, err := w.Write([]byte("two"))
	assert.ErrorIs(t, err, os.ErrPermission)
	assert.Equal(t, 3, n)
	assert.Equal(t, original, w.CurrentPath())
	b, err := os.ReadFile(original)
	require.NoError(t, err)
	assert.Equal(t, "onetwo", string(b))
	w.openFile = os.OpenFile
	_, err = w.Write([]byte("three"))
	require.NoError(t, err)
	assert.NotEqual(t, original, w.CurrentPath())
	b, err = os.ReadFile(w.CurrentPath())
	require.NoError(t, err)
	assert.Equal(t, "three", string(b), "one oversized record stays intact")
}

func TestRotatingFileDoesNotOverwriteOnClockRollback(t *testing.T) {
	w, err := newRotatingFile(t.TempDir(), 10, time.Hour)
	require.NoError(t, err)
	fixed := w.openedAt
	w.now = func() time.Time { return fixed }
	var paths []string
	for i := 0; i < 10; i++ {
		paths = append(paths, w.CurrentPath())
		_, err := w.Write([]byte("saved"))
		require.NoError(t, err)
		require.NoError(t, w.Rotate())
	}
	require.NoError(t, w.Close())
	sort.Strings(paths)
	for i, path := range paths {
		if i > 0 {
			assert.NotEqual(t, paths[i-1], path)
		}
		b, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Equal(t, "saved", string(b))
	}
	_, err = w.Write([]byte("must not write after close"))
	assert.ErrorIs(t, err, os.ErrClosed)
	assert.NoError(t, w.Close())
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("sink unavailable") }

func TestLogMirrorKeepsOtherDestinationWhenOneFails(t *testing.T) {
	for _, failed := range []string{"console", "file"} {
		t.Run(failed, func(t *testing.T) {
			var kept bytes.Buffer
			w := logMirror{console: &kept, file: failingWriter{}}
			if failed == "console" {
				w.console, w.file = failingWriter{}, &kept
			}
			_, err := w.Write([]byte("important record\n"))
			require.Error(t, err)
			assert.Equal(t, "important record\n", kept.String())
		})
	}
}

func TestRotationSettingsOptInAndValidation(t *testing.T) {
	t.Setenv("LOG_ROTATION_ENABLED", "")
	enabled, _, _, err := rotationSettings()
	require.NoError(t, err)
	assert.False(t, enabled)
	t.Setenv("LOG_ROTATION_ENABLED", "true")
	enabled, size, age, err := rotationSettings()
	require.NoError(t, err)
	assert.True(t, enabled)
	assert.Equal(t, int64(100<<20), size)
	assert.Equal(t, 24*time.Hour, age)
	for _, value := range []string{"-1", "0", "1023", "1073741825", "invalid"} {
		t.Setenv("LOG_ROTATION_MAX_BYTES", value)
		_, _, _, err := rotationSettings()
		assert.Error(t, err)
	}
	t.Setenv("LOG_ROTATION_MAX_BYTES", "104857600")
	t.Setenv("LOG_ROTATION_MAX_AGE", "48h")
	_, _, _, err = rotationSettings()
	assert.Error(t, err)
}
