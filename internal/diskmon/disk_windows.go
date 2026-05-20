//go:build windows

package diskmon

import (
	"golang.org/x/sys/windows"
)

func getDiskUsage(path string) (available, total int64, err error) {
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, 0, err
	}

	var freeBytes int64
	var totalBytes int64
	var availBytes int64

	err = windows.GetDiskFreeSpaceEx(pathPtr, &availBytes, &totalBytes, &freeBytes)
	if err != nil {
		return 0, 0, err
	}

	return availBytes, totalBytes, nil
}
