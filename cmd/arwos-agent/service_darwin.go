//go:build darwin

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
)

func installAsService() {
	executable, configPath := servicePaths()
	home, err := os.UserHomeDir()
	if err != nil {
		fatalService("find home directory: %v", err)
	}
	dir := filepath.Join(home, "Library", "LaunchAgents")
	if err = os.MkdirAll(dir, 0o755); err != nil {
		fatalService("create LaunchAgents: %v", err)
	}
	path := filepath.Join(dir, "com.arwos.arwos-agent.plist")
	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>Label</key><string>com.arwos.arwos-agent</string>
<key>ProgramArguments</key><array><string>%s</string><string>--config=%s</string></array>
<key>WorkingDirectory</key><string>%s</string>
<key>RunAtLoad</key><true/><key>KeepAlive</key><true/>
</dict></plist>
`, executable, configPath, filepath.Dir(executable))
	if err = os.WriteFile(path, []byte(plist), 0o644); err != nil {
		fatalService("write plist: %v", err)
	}
	if out, e := exec.Command("launchctl", "load", path).CombinedOutput(); e != nil {
		fatalService("load launch agent: %v: %s", e, out)
	}
	fmt.Printf("Installed and started %s\n", path)
}
