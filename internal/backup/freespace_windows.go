//go:build windows

package backup

import "golang.org/x/sys/windows"

// freeSpace returns the bytes available to the caller on the volume that
// contains path (honoring any disk quota).
func freeSpace(path string) (uint64, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	var freeToCaller, total, totalFree uint64
	if err := windows.GetDiskFreeSpaceEx(p, &freeToCaller, &total, &totalFree); err != nil {
		return 0, err
	}
	return freeToCaller, nil
}
