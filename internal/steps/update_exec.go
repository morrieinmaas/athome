package steps

import (
	"os"
	"syscall"
)

func execBinary(path string, argv []string) error {
	return syscall.Exec(path, argv, os.Environ())
}
