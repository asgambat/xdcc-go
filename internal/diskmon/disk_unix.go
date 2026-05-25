//go:build linux || darwin || freebsd || openbsd || netbsd

package diskmon

import "syscall"

func getDiskUsage(path string) (available, total int64, err error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, 0, err
	}
	// Available space in bytes (guard against uint64 overflow for int64)
	clamp := func(v uint64) int64 {
		if v > 1<<63-1 {
			return 1<<63 - 1
		}
		return int64(v)
	}
	available = clamp(stat.Bavail) * stat.Bsize
	total = clamp(stat.Blocks) * stat.Bsize
	return available, total, nil
}
