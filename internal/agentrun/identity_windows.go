//go:build windows

package agentrun

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

func platformFileIdentity(file *os.File, _ os.FileInfo) (string, bool) {
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(windows.Handle(file.Fd()), &info); err != nil {
		return "", false
	}
	index := uint64(info.FileIndexHigh)<<32 | uint64(info.FileIndexLow)
	return fmt.Sprintf("%x:%x", info.VolumeSerialNumber, index), true
}
