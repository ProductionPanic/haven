//go:build unix

package sshx

import (
	"os"
	"os/exec"
	"syscall"
)

// Exec replaces the current process with ssh. It only returns on error.
func Exec(args []string) error {
	bin, err := exec.LookPath("ssh")
	if err != nil {
		return err
	}
	return syscall.Exec(bin, append([]string{"ssh"}, args...), os.Environ())
}
