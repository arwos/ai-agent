//go:build !windows

/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package updater

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

func updateScript(dir, current, next string) string {
	file := filepath.Join(dir, "apply-update.sh")
	content := fmt.Sprintf("#!/bin/sh\necho 'Arwos: applying update...'\nwhile kill -0 %d 2>/dev/null; do sleep 1; done\ncp %q %q && chmod +x %q && echo 'Update complete. Starting Arwos...' && exec %q\necho 'Update failed.'\nread _\n", os.Getpid(), next, current, current, current)
	_ = os.WriteFile(file, []byte(content), 0700)
	return file
}

// StartScript uses a separate session so closing the agent's process group
// cannot terminate the updater before it replaces the binary.
func StartScript(script string) error {
	command := exec.Command("/bin/sh", script)
	if terminal, err := exec.LookPath("x-terminal-emulator"); err == nil {
		command = exec.Command(terminal, "-e", "/bin/sh", script)
	}
	command.Stdout, command.Stderr, command.Stdin = os.Stdout, os.Stderr, os.Stdin
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return command.Start()
}
