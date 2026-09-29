package main

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

var errContainerMissing = errors.New("container no longer exists")

type dockerReader struct {
	client   *http.Client
	procRoot string
}
type containerInfo struct {
	ID      string `json:"Id"`
	Name    string
	LogPath string
	State   struct {
		Running bool
		Pid     int
	}
	HostConfig struct{ LogConfig struct{ Type string } }
}

func newDockerReader() *dockerReader {
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", "/var/run/docker.sock")
	}}
	return &dockerReader{client: &http.Client{Transport: transport, Timeout: 10 * time.Second}, procRoot: "/proc"}
}

func (d *dockerReader) get(ctx context.Context, endpoint string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://docker"+endpoint, nil)
	if err != nil {
		return err
	}
	response, err := d.client.Do(req)
	if err != nil {
		return errors.New("Docker metadata unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return errContainerMissing
	}
	if response.StatusCode != http.StatusOK {
		return errors.New("Docker metadata request rejected")
	}
	return json.NewDecoder(response.Body).Decode(target)
}

func (d *dockerReader) containers(ctx context.Context, appID string) ([]containerInfo, error) {
	var list []struct {
		ID    string `json:"Id"`
		Names []string
	}
	if err := d.get(ctx, "/containers/json?all=1", &list); err != nil {
		return nil, err
	}
	var results []containerInfo
	for _, item := range list {
		matched := false
		for _, name := range item.Names {
			if strings.HasPrefix(name, "/"+appID+"-") {
				matched = true
			}
		}
		if !matched {
			continue
		}
		var c containerInfo
		if err := d.get(ctx, "/containers/"+item.ID+"/json", &c); err != nil {
			return nil, err
		}
		if !strings.HasPrefix(c.Name, "/"+appID+"-") {
			return nil, errors.New("container identity changed")
		}
		results = append(results, c)
	}
	return results, nil
}

func fileID(containerID, kind string, f *os.File) (string, error) {
	generation, err := nativeFileGeneration(f)
	if err != nil {
		return "", err
	}
	return digest([]byte(containerID + "/" + kind + "/" + generation)), nil
}

// active protects readers too: a log collector or another container can still
// hold an old application log. Unknown access is not proof a file is unused.
func (d *dockerReader) active(ctx context.Context, containerID, kind string, info os.FileInfo) (bool, error) {
	if kind == "app" || kind == "docker" {
		return hostFileActiveAt(info, d.procRoot, os.Getpid())
	}
	return false, errors.New("unsupported source kind")
}

func sourcePathRetired(info os.FileInfo, filePath string) (bool, error) {
	current, err := os.Lstat(filePath)
	if os.IsNotExist(err) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return !os.SameFile(info, current), nil
}

func hostFileActiveAt(info os.FileInfo, procRoot string, ownPID int) (bool, error) {
	processes, err := os.ReadDir(procRoot)
	if err != nil {
		return false, err
	}
	for _, process := range processes {
		pid, err := strconv.Atoi(process.Name())
		if err != nil || pid <= 0 || pid == ownPID {
			continue
		}
		directory := filepath.Join(procRoot, process.Name(), "fd")
		entries, err := os.ReadDir(directory)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return false, err
		}
		for _, entry := range entries {
			fd := filepath.Join(directory, entry.Name())
			opened, err := os.Stat(fd)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return false, err
			}
			if os.SameFile(info, opened) {
				return true, nil
			}
		}
	}
	return false, nil
}
