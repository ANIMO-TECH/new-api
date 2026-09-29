//go:build darwin

package main

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

func nativeFileGeneration(f *os.File) (string, error) {
	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Birthtimespec.Sec <= 0 {
		return "", errors.New("source generation metadata unavailable")
	}
	return fmt.Sprintf("%d/%d/%d.%09d", stat.Dev, stat.Ino, stat.Birthtimespec.Sec, stat.Birthtimespec.Nsec), nil
}
