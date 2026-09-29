package logger

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

// rotatingFile keeps one stable Writer for Gin and other callers that retain it.
// It never deletes logs. A separate, verified archive process owns retention.
type rotatingFile struct {
	mu       sync.Mutex
	dir      string
	maxBytes int64
	maxAge   time.Duration
	now      func() time.Time
	openFile func(string, int, os.FileMode) (*os.File, error)
	file     *os.File
	path     string
	size     int64
	openedAt time.Time
	closed   bool
	lastWarn time.Time
}

// logMirror attempts both destinations even if one fails. Its bytes are unchanged,
// so existing Docker/fluentd/SigNoz consumers keep receiving the same records.
type logMirror struct{ console, file io.Writer }

func (w logMirror) Write(p []byte) (int, error) {
	nConsole, consoleErr := w.console.Write(p)
	nFile, fileErr := w.file.Write(p)
	if nConsole != len(p) && consoleErr == nil {
		consoleErr = io.ErrShortWrite
	}
	if nFile != len(p) && fileErr == nil {
		fileErr = io.ErrShortWrite
	}
	return min(nConsole, nFile), errors.Join(consoleErr, fileErr)
}

func newRotatingFile(dir string, maxBytes int64, maxAge time.Duration) (*rotatingFile, error) {
	if maxBytes <= 0 || maxAge <= 0 {
		return nil, errors.New("log rotation limits must be positive")
	}
	w := &rotatingFile{dir: dir, maxBytes: maxBytes, maxAge: maxAge, now: time.Now, openFile: os.OpenFile}
	if err := w.rotateLocked(); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *rotatingFile) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return 0, os.ErrClosed
	}
	if len(p) == 0 {
		return 0, nil
	}
	var rotationErr error
	if w.size > 0 && (int64(len(p)) > w.maxBytes-w.size || w.now().Sub(w.openedAt) >= w.maxAge) {
		rotationErr = w.rotateLocked()
		if rotationErr != nil {
			w.reportErrorLocked(rotationErr)
		}
	}
	// When rotation fails, retain the old descriptor and append there. stdout/stderr
	// already received this record through MultiWriter; never silently drop the file copy.
	n, err := w.file.Write(p)
	w.size += int64(n)
	if err != nil {
		w.reportErrorLocked(err)
		return n, err
	}
	if n != len(p) {
		return n, io.ErrShortWrite
	}
	return n, rotationErr
}

func (w *rotatingFile) reportErrorLocked(err error) {
	now := w.now()
	if w.lastWarn.IsZero() || now.Sub(w.lastWarn) >= time.Minute {
		// Direct stderr prevents a recursive write into the same locked file writer.
		_, _ = fmt.Fprintf(os.Stderr, "[SYS] %s | log file write/rotation error; console output remains enabled: %v\n", now.Format("2006/01/02 - 15:04:05"), err)
		w.lastWarn = now
	}
}

func (w *rotatingFile) Rotate() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return os.ErrClosed
	}
	return w.rotateLocked()
}

func (w *rotatingFile) CurrentPath() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.path
}

func (w *rotatingFile) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	w.closed = true
	syncErr := w.file.Sync()
	closeErr := w.file.Close()
	return errors.Join(syncErr, closeErr)
}

func (w *rotatingFile) rotateLocked() error {
	now := w.now()
	if w.file != nil {
		if err := w.file.Sync(); err != nil {
			return fmt.Errorf("sync log before rotation: %w", err)
		}
	}
	// O_EXCL prevents overwrites on rapid rotations, restarts or clock rollback.
	var next *os.File
	var path string
	for attempt := 0; attempt < 100; attempt++ {
		name := fmt.Sprintf("oneapi-%s-%03d.log", now.Format("20060102150405.000000000"), attempt)
		path = filepath.Join(w.dir, name)
		var err error
		next, err = w.openFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL|os.O_APPEND, 0640)
		if err == nil {
			break
		}
		if !errors.Is(err, os.ErrExist) {
			return fmt.Errorf("open next log: %w", err)
		}
	}
	if next == nil {
		return errors.New("unable to allocate unique log filename")
	}
	old := w.file
	w.file, w.path, w.size, w.openedAt = next, path, 0, now
	if old != nil {
		if err := old.Close(); err != nil {
			return fmt.Errorf("close previous log: %w", err)
		}
	}
	return nil
}

func rotationSettings() (bool, int64, time.Duration, error) {
	value := os.Getenv("LOG_ROTATION_ENABLED")
	if value == "" {
		return false, 0, 0, nil
	}
	enabled, err := strconv.ParseBool(value)
	if err != nil {
		return false, 0, 0, errors.New("LOG_ROTATION_ENABLED must be a boolean")
	}
	if !enabled {
		return false, 0, 0, nil
	}
	maxBytes, maxAge := int64(100<<20), 24*time.Hour
	if value = os.Getenv("LOG_ROTATION_MAX_BYTES"); value != "" {
		maxBytes, err = strconv.ParseInt(value, 10, 64)
		if err != nil || maxBytes < 1024 || maxBytes > 1<<30 {
			return false, 0, 0, errors.New("LOG_ROTATION_MAX_BYTES must be between 1024 and 1073741824")
		}
	}
	if value = os.Getenv("LOG_ROTATION_MAX_AGE"); value != "" {
		maxAge, err = time.ParseDuration(value)
		if err != nil || maxAge < time.Minute || maxAge > 24*time.Hour {
			return false, 0, 0, errors.New("LOG_ROTATION_MAX_AGE must be between 1m and 24h")
		}
	}
	return true, maxBytes, maxAge, nil
}
