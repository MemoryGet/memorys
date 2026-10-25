package localcli

import (
	"errors"
	"golang.org/x/sys/windows"
	"os/user"
	"strings"
)

// A protected DACL replaces inherited and explicit grants, including on files
// restored from a backup with broader permissions. The owner SID alone has access.
func protect(path string, directory bool) error {
	u, err := user.Current()
	if err != nil {
		return errState
	}
	flags := ""
	if directory {
		flags = "OICI"
	}
	sd, err := windows.SecurityDescriptorFromString("D:P(A;" + flags + ";FA;;;" + u.Uid + ")")
	if err != nil {
		return errState
	}
	acl, _, err := sd.DACL()
	if err != nil {
		return errState
	}
	return windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil)
}

func transientStateDirectoryRename(err error) bool {
	return errors.Is(err, windows.ERROR_ACCESS_DENIED) || errors.Is(err, windows.ERROR_SHARING_VIOLATION) || errors.Is(err, windows.ERROR_LOCK_VIOLATION)
}

func renameStateDirectory(stage, target string) error {
	// Explicitly omit MOVEFILE_REPLACE_EXISTING. Even a destination created
	// between the caller's check and this syscall must remain untouched.
	longPath := func(path string) string {
		if strings.HasPrefix(path, `\\?\`) {
			return path
		}
		if strings.HasPrefix(path, `\\`) {
			return `\\?\UNC\` + strings.TrimPrefix(path, `\\`)
		}
		return `\\?\` + path
	}
	from, err := windows.UTF16PtrFromString(longPath(stage))
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(longPath(target))
	if err != nil {
		return err
	}
	return windows.MoveFileEx(from, to, 0)
}
