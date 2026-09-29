package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

type config struct {
	ExpectedHostname string `json:"expected_hostname"`
	ApplicationID    string `json:"application_id"`
	Region           string `json:"region"`
	Bucket           string `json:"bucket"`
	Prefix           string `json:"prefix"`
	StateDirectory   string `json:"state_directory"`
	DeleteEnabled    bool   `json:"delete_enabled"`
}

type trackedSource struct {
	file     *os.File
	filePath string
	root     *os.Root
	leaf     string
	state    archiveState
}
type worker struct {
	cfg     config
	store   objectStore
	docker  *dockerReader
	sources map[string]*trackedSource
}

func readConfig(name string, requireHost bool) (config, error) {
	f, err := os.Open(name)
	if err != nil {
		return config{}, err
	}
	defer f.Close()
	var c config
	decoder := json.NewDecoder(io.LimitReader(f, 65536))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&c); err != nil {
		return c, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return c, errors.New("configuration must contain one JSON object")
	}
	host, err := os.Hostname()
	if err != nil {
		return c, err
	}
	if c.ExpectedHostname == "" || (requireHost && host != c.ExpectedHostname) || !regexp.MustCompile(`^[a-zA-Z0-9_-]{8,64}$`).MatchString(c.ApplicationID) {
		return c, errors.New("configuration is not for the approved test host/application")
	}
	if !regexp.MustCompile(`^[a-z0-9-]+$`).MatchString(c.Region) || !regexp.MustCompile(`^[a-z0-9][a-z0-9-]{2,62}$`).MatchString(c.Bucket) || !regexp.MustCompile(`^[a-zA-Z0-9_-]+(?:/[a-zA-Z0-9_-]+)+$`).MatchString(c.Prefix) {
		return c, errors.New("invalid archive destination")
	}
	if c.StateDirectory != "/var/lib/newapi-logarchive" {
		return c, errors.New("invalid state directory")
	}
	return c, nil
}

func saveJSON(name string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(name), ".archive-state-")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err := f.Chmod(0600); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, name); err != nil {
		return err
	}
	d, err := os.Open(filepath.Dir(name))
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

func openContainerLogsRoot(containerRoot string) (*os.Root, error) {
	root, err := os.OpenRoot(containerRoot)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return root.OpenRoot("data/logs")
}

func (w *worker) discoverDirectory(c containerInfo, root *os.Root, basePath, kind, dockerLeaf string) error {
	dir, err := root.Open(".")
	if err != nil {
		return err
	}
	entries, err := dir.ReadDir(-1)
	closeErr := dir.Close()
	if err != nil || closeErr != nil {
		return errors.Join(err, closeErr)
	}
	for _, entry := range entries {
		name := entry.Name()
		allowed := kind == "app" && appLogName.MatchString(name)
		if kind == "docker" {
			allowed = name == dockerLeaf || regexp.MustCompile(`^`+regexp.QuoteMeta(dockerLeaf)+`\.[0-9]+$`).MatchString(name)
		}
		if !allowed {
			continue
		}
		info, err := root.Lstat(name)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return errors.New("source is not a regular log file")
		}
		f, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		opened, err := f.Stat()
		if err != nil || !os.SameFile(info, opened) {
			f.Close()
			return errors.New("source changed while opening")
		}
		id, err := fileID(c.ID, kind, f)
		if err != nil {
			f.Close()
			return err
		}
		filePath := filepath.Join(basePath, name)
		if source, ok := w.sources[id]; ok {
			updatedRoot, err := root.OpenRoot(".")
			if err != nil {
				f.Close()
				return err
			}
			source.root.Close()
			source.root = updatedRoot
			source.filePath = filePath
			source.leaf = name
			f.Close()
			continue
		}
		ownedRoot, err := root.OpenRoot(".")
		if err != nil {
			f.Close()
			return err
		}
		state := archiveState{Manifest: manifest{Version: 1, SourceID: id, ContainerID: c.ID, Kind: kind, Name: name}}
		data, err := os.ReadFile(filepath.Join(w.cfg.StateDirectory, id+".json"))
		if err == nil {
			if err := json.Unmarshal(data, &state); err != nil {
				f.Close()
				ownedRoot.Close()
				return errors.New("invalid archive checkpoint")
			}
			if state.Manifest.SourceID != id || state.Manifest.ContainerID != c.ID || state.Manifest.Kind != kind {
				f.Close()
				ownedRoot.Close()
				return errors.New("archive checkpoint identity mismatch")
			}
		} else if !os.IsNotExist(err) {
			f.Close()
			ownedRoot.Close()
			return err
		}
		w.sources[id] = &trackedSource{file: f, filePath: filePath, root: ownedRoot, leaf: name, state: state}
	}
	return nil
}

func (w *worker) discover(ctx context.Context) error {
	containers, err := w.docker.containers(ctx, w.cfg.ApplicationID)
	if err != nil {
		return err
	}
	if len(containers) == 0 && len(w.sources) == 0 {
		return errors.New("approved application container not found")
	}
	for _, c := range containers {
		if c.State.Running && c.State.Pid > 0 {
			containerRoot := fmt.Sprintf("/proc/%d/root", c.State.Pid)
			root, err := openContainerLogsRoot(containerRoot)
			if err != nil {
				return err
			}
			var current containerInfo
			err = w.docker.get(ctx, "/containers/"+c.ID+"/json", &current)
			if err != nil || !current.State.Running || current.State.Pid != c.State.Pid {
				root.Close()
				return errors.New("container changed while opening its log directory")
			}
			err = w.discoverDirectory(c, root, containerRoot+"/data/logs", "app", "")
			root.Close()
			if err != nil {
				return err
			}
		}
		if c.HostConfig.LogConfig.Type == "json-file" && c.LogPath != "" {
			root, err := os.OpenRoot(filepath.Dir(c.LogPath))
			if err != nil {
				return err
			}
			err = w.discoverDirectory(c, root, filepath.Dir(c.LogPath), "docker", filepath.Base(c.LogPath))
			root.Close()
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (w *worker) cycle(ctx context.Context) error {
	if err := w.discover(ctx); err != nil {
		return err
	}
	var failures []error
	for id, source := range w.sources {
		if err := ctx.Err(); err != nil {
			return err
		}
		info, err := source.file.Stat()
		if err != nil {
			failures = append(failures, err)
			continue
		}
		m := source.state.Manifest
		active, err := w.docker.active(ctx, m.ContainerID, m.Kind, info)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		if info.Size() < m.Size {
			failures = append(failures, errors.New("source was truncated; retention is withheld"))
			continue
		}
		changed := !active && (m.Size != info.Size() || !m.ModifiedAt.Equal(info.ModTime()) || !m.Complete)
		if active && info.Size() > m.Size && (info.Size()-m.Size >= chunkSize || time.Since(m.VerifiedAt) >= 5*time.Minute) {
			changed = true
		}
		if changed || source.state.ManifestKey == "" {
			state, err := archiveSnapshot(ctx, w.store, w.cfg.Prefix, source.file, m, !active, time.Now())
			if err != nil {
				failures = append(failures, err)
				continue
			}
			if err := saveJSON(filepath.Join(w.cfg.StateDirectory, id+".json"), state); err != nil {
				failures = append(failures, err)
				continue
			}
			source.state = state
		}
		if !active && source.state.Manifest.Complete {
			retired, err := sourcePathRetired(info, source.filePath)
			if err != nil {
				failures = append(failures, err)
				continue
			}
			if retired {
				// A rotation can reuse the same pathname for a different inode. Once
				// this inode is fully archived, close it without touching that new file.
				source.file.Close()
				source.root.Close()
				delete(w.sources, id)
				continue
			}
		}
		if !active && source.state.DeletedAt == nil {
			removed, err := deleteVerifiedFile(ctx, w.store, source.root, source.leaf, source.state, time.Now(), w.cfg.DeleteEnabled, func() (bool, error) { return w.docker.active(ctx, m.ContainerID, m.Kind, info) })
			if err != nil && !os.IsNotExist(err) {
				failures = append(failures, err)
				continue
			}
			if removed {
				now := time.Now().UTC()
				source.state.DeletedAt = &now
				if err := saveJSON(filepath.Join(w.cfg.StateDirectory, id+".json"), source.state); err != nil {
					failures = append(failures, err)
					continue
				}
			}
		}
		if !active && source.state.Manifest.Complete {
			if _, err := os.Lstat(source.filePath); os.IsNotExist(err) {
				source.file.Close()
				source.root.Close()
				delete(w.sources, id)
			}
		}
	}
	return errors.Join(failures...)
}

func main() {
	configPath := flag.String("config", "/etc/newapi-logarchive.json", "archive configuration")
	once := flag.Bool("once", false, "archive one pass and exit")
	restoreKey := flag.String("restore", "", "restore a complete archive manifest")
	snapshot := flag.Bool("snapshot", false, "allow recovery of the archived prefix of a file that was still active")
	output := flag.String("output", "", "new destination file for restored bytes")
	flag.Parse()
	if (*snapshot || *output != "") && *restoreKey == "" {
		fmt.Fprintln(os.Stderr, "--snapshot/--output require --restore")
		os.Exit(1)
	}
	cfg, err := readConfig(*configPath, *restoreKey == "")
	if err != nil {
		fmt.Fprintln(os.Stderr, "invalid log archive configuration:", err)
		os.Exit(1)
	}
	store, err := newOSSStore(cfg.Region, cfg.Bucket, cfg.Prefix)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	if *restoreKey != "" {
		if *output == "" {
			fmt.Fprintln(os.Stderr, "restore requires --output")
			os.Exit(1)
		}
		m, err := restoreToFile(ctx, store, *restoreKey, *output, !*snapshot)
		if err != nil {
			fmt.Fprintln(os.Stderr, "restore verification failed:", err)
			os.Exit(1)
		}
		fmt.Printf("restored_bytes=%d sha256=%s source_closed=%t\n", m.Size, m.SHA256, m.Complete)
		return
	}
	if err := os.MkdirAll(cfg.StateDirectory, 0700); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.Chmod(cfg.StateDirectory, 0700); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	lock, err := os.OpenFile(filepath.Join(cfg.StateDirectory, "worker.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		fmt.Fprintln(os.Stderr, "archive worker lock unavailable")
		os.Exit(1)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		fmt.Fprintln(os.Stderr, "another archive worker is active")
		os.Exit(1)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	w := worker{cfg: cfg, store: store, docker: newDockerReader(), sources: map[string]*trackedSource{}}
	defer func() {
		for _, source := range w.sources {
			source.file.Close()
			source.root.Close()
		}
	}()
	for {
		// Reload only the deletion switch. Keeping the process and open sources
		// alive avoids losing access to a retired container's final log bytes.
		updated, configErr := readConfig(*configPath, true)
		oldIdentity, newIdentity := cfg, updated
		oldIdentity.DeleteEnabled, newIdentity.DeleteEnabled = false, false
		if configErr != nil || oldIdentity != newIdentity {
			w.cfg.DeleteEnabled = false
			configErr = errors.New("archive configuration reload failed; deletion disabled")
		} else {
			w.cfg.DeleteEnabled = updated.DeleteEnabled
		}
		err := w.cycle(ctx)
		err = errors.Join(err, configErr)
		var sources []map[string]any
		for _, source := range w.sources {
			m := source.state.Manifest
			info, statErr := source.file.Stat()
			if statErr != nil {
				continue
			}
			sources = append(sources, map[string]any{"container_id": m.ContainerID, "kind": m.Kind, "source_id": m.SourceID, "name": m.Name, "bytes": info.Size(), "archived_bytes": m.Size, "complete": m.Complete, "manifest_key": source.state.ManifestKey, "verified_at": m.VerifiedAt})
		}
		status := map[string]any{"at": time.Now().UTC(), "tracked_sources": len(w.sources), "sources": sources, "delete_enabled": w.cfg.DeleteEnabled, "success": err == nil}
		if err != nil {
			status["error"] = strings.ReplaceAll(err.Error(), "\n", "; ")
		}
		_ = saveJSON(filepath.Join(cfg.StateDirectory, "status.json"), status)
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"at": status["at"], "tracked_sources": len(w.sources), "delete_enabled": w.cfg.DeleteEnabled, "success": err == nil, "error": status["error"]})
		if *once {
			if err != nil {
				os.Exit(1)
			}
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(20 * time.Second):
		}
	}
}
