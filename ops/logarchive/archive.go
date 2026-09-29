package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"time"
)

const chunkSize = 8 << 20

type objectStore interface {
	Put(context.Context, string, []byte) error
	Get(context.Context, string) (io.ReadCloser, error)
}

type archiveChunk struct {
	Key    string `json:"key"`
	Offset int64  `json:"offset"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type manifest struct {
	Version     int            `json:"version"`
	SourceID    string         `json:"source_id"`
	ContainerID string         `json:"container_id"`
	Kind        string         `json:"kind"`
	Name        string         `json:"name"`
	Size        int64          `json:"size"`
	Chunks      []archiveChunk `json:"chunks"`
	Complete    bool           `json:"complete"`
	SHA256      string         `json:"sha256,omitempty"`
	ModifiedAt  time.Time      `json:"modified_at"`
	VerifiedAt  time.Time      `json:"verified_at"`
}

type archiveState struct {
	Manifest    manifest   `json:"manifest"`
	ManifestKey string     `json:"manifest_key"`
	DeletedAt   *time.Time `json:"deleted_at,omitempty"`
}

func digest(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// archiveSnapshot adds only new bytes. Every uploaded chunk is downloaded and
// decompressed before its offset can enter durable state. It never removes input.
func archiveSnapshot(ctx context.Context, store objectStore, prefix string, f *os.File, prior manifest, closed bool, now time.Time) (archiveState, error) {
	before, err := f.Stat()
	if err != nil {
		return archiveState{}, err
	}
	if !before.Mode().IsRegular() || before.Size() < prior.Size {
		return archiveState{}, errors.New("source is not regular or was truncated; retaining source")
	}
	m := prior
	m.Version = 1
	m.Chunks = append([]archiveChunk(nil), prior.Chunks...)
	m.Complete = false
	m.SHA256 = ""
	for offset := m.Size; offset < before.Size(); {
		if err := ctx.Err(); err != nil {
			return archiveState{}, err
		}
		length := min(int64(chunkSize), before.Size()-offset)
		data := make([]byte, length)
		if _, err := f.ReadAt(data, offset); err != nil {
			return archiveState{}, fmt.Errorf("read source segment: %w", err)
		}
		sha := digest(data)
		var compressed bytes.Buffer
		writer, _ := gzip.NewWriterLevel(&compressed, gzip.BestSpeed)
		if _, err := writer.Write(data); err != nil {
			return archiveState{}, err
		}
		if err := writer.Close(); err != nil {
			return archiveState{}, err
		}
		key := path.Join(prefix, m.ContainerID, m.Kind, m.SourceID, fmt.Sprintf("%016d-%s.gz", offset, sha))
		if err := store.Put(ctx, key, compressed.Bytes()); err != nil {
			return archiveState{}, fmt.Errorf("archive chunk upload: %w", err)
		}
		chunk := archiveChunk{Key: key, Offset: offset, Size: length, SHA256: sha}
		if _, err := restoreChunk(ctx, store, chunk, io.Discard); err != nil {
			return archiveState{}, err
		}
		m.Chunks = append(m.Chunks, chunk)
		offset += length
		m.Size = offset
	}
	after, err := f.Stat()
	if err != nil {
		return archiveState{}, err
	}
	if before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		closed = false
	}
	m.ModifiedAt = before.ModTime().UTC()
	if closed {
		localHash := sha256.New()
		if _, err := io.Copy(localHash, io.NewSectionReader(f, 0, m.Size)); err != nil {
			return archiveState{}, err
		}
		remoteHash := sha256.New()
		if err := restoreChunks(ctx, store, m, remoteHash); err != nil {
			return archiveState{}, err
		}
		if !bytes.Equal(localHash.Sum(nil), remoteHash.Sum(nil)) {
			return archiveState{}, errors.New("full restore checksum mismatch; retaining source")
		}
		final, err := f.Stat()
		if err != nil {
			return archiveState{}, err
		}
		if final.Size() != before.Size() || !final.ModTime().Equal(before.ModTime()) {
			return archiveState{}, errors.New("source changed during restore validation; retaining source")
		}
		m.Complete = true
		m.SHA256 = hex.EncodeToString(localHash.Sum(nil))
	}
	m.VerifiedAt = now.UTC()
	encoded, err := json.Marshal(m)
	if err != nil {
		return archiveState{}, err
	}
	key := path.Join(prefix, m.ContainerID, m.Kind, m.SourceID, fmt.Sprintf("manifest-%016d-%s.json", m.Size, digest(encoded)))
	if err := store.Put(ctx, key, encoded); err != nil {
		return archiveState{}, fmt.Errorf("manifest upload: %w", err)
	}
	r, err := store.Get(ctx, key)
	if err != nil {
		return archiveState{}, fmt.Errorf("manifest recovery: %w", err)
	}
	verified, readErr := io.ReadAll(io.LimitReader(r, int64(len(encoded))+1))
	closeErr := r.Close()
	if readErr != nil || closeErr != nil {
		return archiveState{}, errors.Join(readErr, closeErr)
	}
	if !bytes.Equal(verified, encoded) {
		return archiveState{}, errors.New("manifest recovery mismatch; retaining source")
	}
	return archiveState{Manifest: m, ManifestKey: key}, nil
}

func restoreChunk(ctx context.Context, store objectStore, c archiveChunk, output io.Writer) (int64, error) {
	if c.Size < 0 || c.Size > chunkSize || len(c.SHA256) != 64 {
		return 0, errors.New("invalid archive chunk")
	}
	r, err := store.Get(ctx, c.Key)
	if err != nil {
		return 0, fmt.Errorf("archive download failed: %w", err)
	}
	defer r.Close()
	z, err := gzip.NewReader(r)
	if err != nil {
		return 0, errors.New("archive gzip header invalid")
	}
	defer z.Close()
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(output, h), io.LimitReader(z, c.Size+1))
	if err != nil {
		return n, fmt.Errorf("archive read/checksum failed: %w", err)
	}
	if n != c.Size || hex.EncodeToString(h.Sum(nil)) != c.SHA256 {
		return n, errors.New("archive segment checksum/size mismatch")
	}
	return n, nil
}

func restoreChunks(ctx context.Context, store objectStore, m manifest, output io.Writer) error {
	var offset int64
	for _, c := range m.Chunks {
		if c.Offset != offset {
			return errors.New("archive offsets are not contiguous")
		}
		n, err := restoreChunk(ctx, store, c, output)
		if err != nil {
			return err
		}
		offset += n
	}
	if offset != m.Size {
		return errors.New("archive total size mismatch")
	}
	return nil
}

func restoreManifest(ctx context.Context, store objectStore, key string, output io.Writer) (manifest, error) {
	return restoreArchive(ctx, store, key, output, true)
}

func restoreArchive(ctx context.Context, store objectStore, key string, output io.Writer, requireClosed bool) (manifest, error) {
	r, err := store.Get(ctx, key)
	if err != nil {
		return manifest{}, err
	}
	defer r.Close()
	data, err := io.ReadAll(io.LimitReader(r, 8<<20))
	if err != nil {
		return manifest{}, err
	}
	var m manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return manifest{}, errors.New("invalid archive manifest JSON")
	}
	if m.Version != 1 || (requireClosed && !m.Complete) || (m.Complete && len(m.SHA256) != 64) {
		return manifest{}, errors.New("archive is not a verified complete file")
	}
	h := sha256.New()
	if err := restoreChunks(ctx, store, m, io.MultiWriter(output, h)); err != nil {
		return manifest{}, err
	}
	actualHash := hex.EncodeToString(h.Sum(nil))
	if m.SHA256 != "" && actualHash != m.SHA256 {
		return manifest{}, errors.New("restored file checksum mismatch")
	}
	m.SHA256 = actualHash
	return m, nil
}
