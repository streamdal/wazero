//go:build tinygo

package sysfs

import (
	"os"

	"github.com/streamdal/wazero/experimental/sys"
)

func datasync(f *os.File) sys.Errno {
	return sys.ENOSYS
}
