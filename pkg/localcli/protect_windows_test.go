package localcli

import (
	"golang.org/x/sys/windows"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
)

func TestWindowsPrivateKeyDACL(t *testing.T) {
	s, err := createState(filepath.Join(t.TempDir(), "runtime"))
	if err != nil {
		t.Fatal(err)
	}
	u, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{s.root, filepath.Join(s.root, "private.pem")} {
		sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
		if err != nil {
			t.Fatal(err)
		}
		acl, _, err := sd.DACL()
		if err != nil || acl == nil || acl.AceCount != 1 {
			t.Fatal("private path grants other principals", err)
		}
		control, _, err := sd.Control()
		if err != nil || control&windows.SE_DACL_PROTECTED == 0 || !strings.Contains(sd.String(), u.Uid) {
			t.Fatal("DACL not protected for current SID")
		}
	}
}
