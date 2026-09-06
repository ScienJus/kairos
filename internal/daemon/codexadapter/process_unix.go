//go:build darwin || linux

package codexadapter

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

func configureProcess(cmd *exec.Cmd) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return nil
}

func killGroup(pid int) error {
	err := syscall.Kill(-pid, syscall.SIGKILL)
	if err == syscall.ESRCH {
		return nil
	}
	return err
}

func groupAlive(pid int) bool { return syscall.Kill(-pid, 0) != syscall.ESRCH }

func exitedNormally(err error) bool {
	if err == nil {
		return true
	}
	var exit *exec.ExitError
	return errors.As(err, &exit) && exit.ProcessState.Exited()
}

func openOutcome(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
}
