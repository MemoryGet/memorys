//go:build !windows

package localcli

import "os"

func protect(path string, directory bool) error {
	mode := os.FileMode(0600)
	if directory {
		mode = 0700
	}
	return os.Chmod(path, mode)
}

func transientStateDirectoryRename(error) bool { return false }
func renameStateDirectory(stage, target string) error {
	return os.Rename(stage, target)
}
