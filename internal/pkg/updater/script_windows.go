//go:build windows

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
	file := filepath.Join(dir, "apply-update.cmd")
	content := fmt.Sprintf("@echo off\r\necho Arwos: applying update...\r\n:wait\r\ntasklist /FI \"PID eq %d\" | find \"%d\" >nul && (timeout /t 1 >nul & goto wait)\r\ncopy /Y \"%s\" \"%s\" && start \"Arwos\" \"%s\"\r\necho Update complete.\r\npause\r\n", os.Getpid(), os.Getpid(), next, current, current)
	_ = os.WriteFile(file, []byte(content), 0700)
	return file
}

func StartScript(script string) error {
	command := exec.Command("cmd.exe", "/c", "start", "Arwos update", "cmd.exe", "/k", script)
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000200 | 0x00000008}
	return command.Start()
}
