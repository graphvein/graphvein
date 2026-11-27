//go:build windows

package storeloc

import (
	"path/filepath"

	"golang.org/x/sys/windows"
)

// remoteDrive reports a path on a drive letter Windows lists as a network
// drive (GetDriveType DRIVE_REMOTE), such as a share mapped to Z:. SQLite's
// WAL needs shared memory, which network file systems do not provide.
func remoteDrive(path string) bool {
	volume := filepath.VolumeName(path)
	if len(volume) != 2 || volume[1] != ':' {
		return false
	}
	root, err := windows.UTF16PtrFromString(volume + `\`)
	if err != nil {
		return false
	}
	return windows.GetDriveType(root) == windows.DRIVE_REMOTE
}
