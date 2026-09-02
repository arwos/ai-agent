/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package workspace

import (
	"context"
	"errors"
	"os/exec"
	"runtime"
	"strings"
	"sync"
)

var ErrPickerCancelled = errors.New("folder selection cancelled")
var ErrPickerBusy = errors.New("folder selection is already open")

// Picker opens the native directory selector on the machine running the agent.
// It deliberately returns only the selected path to the application layer.
type Picker struct {
	mu      sync.Mutex
	picking bool
}

func NewPicker() *Picker { return &Picker{} }

func (p *Picker) Directory(ctx context.Context) (string, error) {
	p.mu.Lock()
	if p.picking {
		p.mu.Unlock()
		return "", ErrPickerBusy
	}
	p.picking = true
	p.mu.Unlock()
	defer func() { p.mu.Lock(); p.picking = false; p.mu.Unlock() }()

	var commands [][]string
	switch runtime.GOOS {
	case "darwin":
		commands = [][]string{{"osascript", "-e", "POSIX path of (choose folder with prompt \"Choose Arwos workspace\")"}}
	case "windows":
		pickerScript := "Add-Type -AssemblyName System.Windows.Forms; $d=New-Object System.Windows.Forms.FolderBrowserDialog; if($d.ShowDialog() -eq 'OK'){[Console]::Write($d.SelectedPath)}"
		commands = [][]string{{"pwsh", "-NoProfile", "-Command", pickerScript}, {"powershell", "-NoProfile", "-Command", pickerScript}}
	default:
		commands = [][]string{{"zenity", "--file-selection", "--directory", "--title=Choose Arwos workspace"}, {"kdialog", "--getexistingdirectory", "."}}
	}
	for _, args := range commands {
		if _, err := exec.LookPath(args[0]); err != nil {
			continue
		}
		output, err := exec.CommandContext(ctx, args[0], args[1:]...).Output()
		path := strings.TrimSpace(string(output))
		if err != nil || path == "" {
			return "", ErrPickerCancelled
		}
		return path, nil
	}
	return "", errors.New("native folder picker is unavailable; install zenity or kdialog")
}
