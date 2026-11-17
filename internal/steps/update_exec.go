package steps

import (
	"os"
	"syscall"
)

// updateReexecEnv marks the process athome update re-executes into the freshly
// installed binary; it stops a binary that reports the wrong version from updating forever.
const updateReexecEnv = "ATHOME_UPDATE_REEXEC"

func reexecEnviron(base []string) []string {
	return append(base, updateReexecEnv+"=1")
}

func execBinary(path string, argv []string) error {
	return syscall.Exec(path, argv, reexecEnviron(os.Environ()))
}
