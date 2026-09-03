/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/arwos/ai-agent/internal/configembed"
)

func servicePaths() (string, string) {
	executable, err := os.Executable()
	if err != nil {
		fatalService("find executable: %v", err)
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		fatalService("resolve executable path: %v", err)
	}
	configPath := filepath.Join(filepath.Dir(executable), "config.yaml")
	if _, err = os.Stat(configPath); os.IsNotExist(err) {
		if err = os.WriteFile(configPath, configembed.Default, 0o644); err != nil {
			fatalService("write default config: %v", err)
		}
	} else if err != nil {
		fatalService("check config: %v", err)
	}
	return executable, configPath
}

func fatalService(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "arwos-agent: "+format+"\n", args...)
	os.Exit(1)
}
