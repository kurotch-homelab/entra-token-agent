package securefile

import (
	"golang.org/x/sys/windows"
	"strings"
	"testing"
)

func assertPrivate(t *testing.T, path string) {
	t.Helper()
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	want, err := windows.SecurityDescriptorFromString("D:P(A;;FA;;;SY)(A;;FA;;;" + user.User.Sid.String() + ")")
	if err != nil {
		t.Fatal(err)
	}
	// Windows may retain the auto-inherited control flag even on a protected DACL.
	if strings.Replace(sd.String(), "D:PAI", "D:P", 1) != want.String() {
		t.Fatalf("unexpected file DACL: %s", sd.String())
	}
}
