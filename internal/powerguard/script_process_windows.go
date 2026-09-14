//go:build windows

package powerguard

import (
	"errors"
	"os/exec"
)

func prepareScriptProcess(command *exec.Cmd) (func(), error) {
	return nil, errors.New("GPIO script execution requires Unix process groups")
}
