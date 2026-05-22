//go:build !windows
// +build !windows

package api

import "syscall"

func getDiskInfo(path string) (*diskInfo, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return nil, err
	}
	return &diskInfo{
		available: int64(stat.Bavail) * int64(stat.Bsize),
		total:     int64(stat.Blocks) * int64(stat.Bsize),
		used:      (int64(stat.Blocks) - int64(stat.Bfree)) * int64(stat.Bsize),
	}, nil
}
