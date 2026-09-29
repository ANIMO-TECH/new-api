package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWorkerBatchesActiveUpdatesAndFlushesOnClose(t *testing.T) {
	s, f, m, _ := archiveFixture(t, 1024, 0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/containers/json", r.URL.Path)
		_, _ = w.Write([]byte("[]"))
	}))
	defer server.Close()
	proc := t.TempDir()
	fdDir := filepath.Join(proc, "123456789", "fd")
	require.NoError(t, os.MkdirAll(fdDir, 0700))
	require.NoError(t, os.Symlink(f.Name(), filepath.Join(fdDir, "4")))
	client := server.Client()
	client.Transport = rewriteTransport{base: client.Transport, target: server.URL}
	source := &trackedSource{file: f, filePath: f.Name(), root: testRoot(t, f.Name()), leaf: filepath.Base(f.Name()), state: archiveState{Manifest: m}}
	w := worker{cfg: config{ApplicationID: "test-app-id", Prefix: "logs/test", StateDirectory: t.TempDir()}, store: s,
		docker: &dockerReader{client: client, procRoot: proc}, sources: map[string]*trackedSource{m.SourceID: source}}
	require.NoError(t, w.cycle(context.Background()))
	initialPuts := s.puts
	_, err := f.WriteAt([]byte("append"), 1024)
	require.NoError(t, err)
	require.NoError(t, w.cycle(context.Background()))
	assert.Equal(t, initialPuts, s.puts, "small active writes do not publish ever-growing manifests every poll")
	assert.Equal(t, int64(1024), source.state.Manifest.Size)
	source.state.Manifest.VerifiedAt = time.Now().Add(-6 * time.Minute)
	require.NoError(t, w.cycle(context.Background()))
	assert.Greater(t, s.puts, initialPuts)
	assert.Equal(t, int64(1030), source.state.Manifest.Size)
	assert.False(t, source.state.Manifest.Complete)
	require.NoError(t, os.Remove(filepath.Join(fdDir, "4")))
	require.NoError(t, w.cycle(context.Background()))
	assert.True(t, source.state.Manifest.Complete, "closing a source forces full recovery verification")
}

type rewriteTransport struct {
	base   http.RoundTripper
	target string
}

func (r rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	forward, err := http.NewRequestWithContext(req.Context(), req.Method, r.target+req.URL.RequestURI(), req.Body)
	if err != nil {
		return nil, err
	}
	return r.base.RoundTrip(forward)
}
