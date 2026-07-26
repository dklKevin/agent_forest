//go:build !aix && !darwin && !dragonfly && !freebsd && !illumos && !linux && !netbsd && !openbsd && !solaris && !windows

package agentrun

import "os"

func platformFileIdentity(_ *os.File, _ os.FileInfo) (string, bool) {
	return "", false
}
