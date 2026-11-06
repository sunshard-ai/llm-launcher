//go:build !windows && !darwin && !linux && !dragonfly && !freebsd && !netbsd && !openbsd && !illumos

package catalog

import (
	"fmt"
	"os"
)

func tryCatalogLock(_ *os.File) (bool, error) {
	return false, fmt.Errorf("catalog updates are unavailable on this platform")
}
