//go:build !windows

package securefile

import "os"

func protectFile(f *os.File) error { return f.Chmod(0600) }
