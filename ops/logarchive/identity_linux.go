//go:build linux

package main

import (
	"errors"
	"fmt"
	"os"
	"runtime"

	"golang.org/x/sys/unix"
)

func nativeFileGeneration(f *os.File) (string, error) {
	defer runtime.KeepAlive(f)
	var stat unix.Statx_t
	if err := unix.Statx(int(f.Fd()), "", unix.AT_EMPTY_PATH, unix.STATX_INO|unix.STATX_BTIME, &stat); err != nil {
		return "", errors.New("source generation metadata unavailable; retention withheld")
	}
	if stat.Mask&unix.STATX_BTIME == 0 || stat.Btime.Sec <= 0 {
		return "", errors.New("filesystem does not expose creation time; retention withheld")
	}
	// Birth time is stable across append and rename, but distinguishes a reused inode.
	return fmt.Sprintf("%d:%d/%d/%d.%09d", stat.Dev_major, stat.Dev_minor, stat.Ino, stat.Btime.Sec, stat.Btime.Nsec), nil
}
