//go:build windows

/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package main

import (
	"fmt"
	"os/exec"
)

func installAsService() {
	executable, configPath := servicePaths()
	binPath := fmt.Sprintf(`"%s" --config="%s"`, executable, configPath)
	cmd := exec.Command("sc.exe", "create", "ArwosAI", "start=", "auto", "binPath=", binPath, "DisplayName=", "Arwos AI Agent")
	if out, err := cmd.CombinedOutput(); err != nil {
		fatalService("create Windows service: %v: %s", err, out)
	}
	if out, err := exec.Command("sc.exe", "start", "ArwosAI").CombinedOutput(); err != nil {
		fatalService("start Windows service: %v: %s", err, out)
	}
	fmt.Println("Installed and started Windows service ArwosAI")
}

func uninstallAsService() {
	if out, err := exec.Command("sc.exe", "stop", "ArwosAI").CombinedOutput(); err != nil {
		_ = out // The service may already be stopped.
	}
	if out, err := exec.Command("sc.exe", "delete", "ArwosAI").CombinedOutput(); err != nil {
		fatalService("delete Windows service: %v: %s", err, out)
	}
	fmt.Println("Uninstalled Windows service ArwosAI")
}
