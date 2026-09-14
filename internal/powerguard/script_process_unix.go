//go:build !windows

package powerguard

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// Terminate the process group, including children holding stdout/stderr open.
// Root scripts are trusted code, not a sandbox: they must be explicitly enabled.
func prepareScriptProcess(command *exec.Cmd) (func(), error) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	kill := func() error {
		if command.Process == nil {
			return nil
		}
		err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	command.Cancel = kill
	return func() { _ = kill() }, nil
}
