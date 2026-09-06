//go:build !darwin && !linux

package codexadapter

import (
	"errors"
	"os"
	"os/exec"
)

func configureProcess(*exec.Cmd) error          { return errors.New("Codex Adapter requires Linux or macOS") }
func killGroup(int) error                       { return errors.New("process groups are unsupported") }
func groupAlive(int) bool                       { return true }
func exitedNormally(error) bool                 { return false }
func openOutcome(path string) (*os.File, error) { return os.Open(path) }
