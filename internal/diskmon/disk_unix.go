//go:build linux || darwin || freebsd || openbsd || netbsd

package diskmon

import "syscall"

func getDiskUsage(path string) (available, total int64, err error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, 0, err
	}
	// Available space in bytes
	available = int64(stat.Bavail) * stat.Bsize
	total = int64(stat.Blocks) * stat.Bsize
	return available, total, nil
}
