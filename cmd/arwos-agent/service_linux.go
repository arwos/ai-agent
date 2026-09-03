//go:build linux

/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func installAsService() {
	executable, configPath := servicePaths()
	configDir, err := os.UserConfigDir()
	if err != nil {
		fatalService("find user config directory: %v", err)
	}
	unitDir := filepath.Join(configDir, "systemd", "user")
	if err := os.MkdirAll(unitDir, 0o755); err != nil {
		fatalService("create systemd user directory: %v", err)
	}
	unitPath := filepath.Join(unitDir, "arwos-agent.service")
	unit := fmt.Sprintf("[Unit]\nDescription=Arwos AI Agent\nAfter=network-online.target\n\n[Service]\nType=simple\nWorkingDirectory=%s\nExecStart=%s --config=%s\nRestart=on-failure\n\n[Install]\nWantedBy=default.target\n", executableDir(executable), executable, configPath)
	if err := os.WriteFile(unitPath, []byte(unit), 0o644); err != nil {
		fatalService("write unit file: %v", err)
	}
	for _, args := range [][]string{{"daemon-reload"}, {"enable", "--now", "arwos-agent.service"}} {
		out, err := exec.Command("systemctl", append([]string{"--user"}, args...)...).CombinedOutput()
		if err != nil {
			fatalService("systemctl: %v: %s", err, strings.TrimSpace(string(out)))
		}
	}
	fmt.Printf("Installed and started %s\n", unitPath)
}

func executableDir(path string) string { return filepath.Dir(path) }
