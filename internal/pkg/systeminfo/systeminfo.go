/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package systeminfo

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"go.osspkg.com/goppy/v3/plugin"
)

type ConfigGroup struct {
	LocalLLM Config `yaml:"local_llm"`
}
type Config struct {
	Folder string `yaml:"folder"`
}

func (c *ConfigGroup) Default() {
	if c.LocalLLM.Folder == "" {
		c.LocalLLM.Folder = "./datasource/llm"
	}
}
func (c Config) Validate() error { return nil }
func WithPlugin() plugin.Kind {
	return plugin.Kind{Config: &ConfigGroup{}, Inject: func(c *ConfigGroup) *Service { return New(c.LocalLLM.Folder) }}
}

type Service struct{ root string }

func New(root string) *Service  { return &Service{root: root} }
func (s *Service) Root() string { return s.root }

type Info struct {
	CPUType         string  `json:"cpuType"`
	CPUFrequencyMHz int     `json:"cpuFrequencyMHz"`
	CPUCores        int     `json:"cpuCores"`
	MemoryType      string  `json:"memoryType"`
	MemoryGB        float64 `json:"memoryGB"`
	GPUType         string  `json:"gpuType"`
	VRAMGB          float64 `json:"vramGB"`
	OllamaInstalled bool    `json:"ollamaInstalled"`
	LlamaInstalled  bool    `json:"llamaInstalled"`
	Disks           []Disk  `json:"disks"`
}

type Disk struct {
	Name       string   `json:"name"`
	MountPoint string   `json:"mountPoint"`
	TotalGB    float64  `json:"totalGB"`
	FreeGB     float64  `json:"freeGB"`
	Tags       []string `json:"tags"`
}

func (s *Service) Collect(ctx context.Context) Info {
	out := Info{CPUType: runtime.GOARCH, CPUCores: runtime.NumCPU(), MemoryType: "Unknown", GPUType: "Unknown"}
	out.OllamaInstalled = fileExists(filepath.Join(s.root, "ollama", "ollama"))
	out.OllamaInstalled = out.OllamaInstalled || fileExists(filepath.Join(s.root, "ollama", "bin", "ollama"))
	if runtime.GOOS == "windows" {
		out.OllamaInstalled = fileExists(filepath.Join(s.root, "ollama", "ollama.exe")) || fileExists(filepath.Join(s.root, "ollama", "bin", "ollama.exe"))
	} else if runtime.GOOS == "darwin" {
		out.OllamaInstalled = out.OllamaInstalled || fileExists(filepath.Join(s.root, "ollama", "Ollama.dmg"))
	}
	out.LlamaInstalled = fileExists(filepath.Join(s.root, "llama", "llama"))
	out.Disks = disks(ctx)
	if out.OllamaInstalled {
		markDisk(out.Disks, filepath.Join(s.root, "ollama"), "ob")
		markDisk(out.Disks, filepath.Join(s.root, "ollama-models"), "om")
	}
	if out.LlamaInstalled {
		markDisk(out.Disks, filepath.Join(s.root, "llama"), "lb")
		markDisk(out.Disks, filepath.Join(s.root, "llama-models"), "lm")
	}
	if b, err := os.ReadFile("/proc/cpuinfo"); err == nil {
		text := string(b)
		out.CPUType = firstValue(text, "model name", out.CPUType)
		out.CPUFrequencyMHz = int(parseFloat(firstValue(text, "cpu MHz", "0")))
	}
	if b, err := os.ReadFile("/proc/meminfo"); err == nil {
		out.MemoryGB = parseFloat(firstValue(string(b), "MemTotal", "0")) / 1024 / 1024
	}
	if value := command(ctx, "nvidia-smi", "--query-gpu=name,memory.total", "--format=csv,noheader,nounits"); value != "" {
		parts := strings.SplitN(value, ",", 2)
		out.GPUType = strings.TrimSpace(parts[0])
		if len(parts) == 2 {
			out.VRAMGB = parseFloat(strings.TrimSpace(parts[1])) / 1024
		}
	} else if value := command(ctx, "lspci"); value != "" {
		for _, line := range strings.Split(value, "\n") {
			lower := strings.ToLower(line)
			if strings.Contains(lower, "vga compatible controller") || strings.Contains(lower, "3d controller") {
				if i := strings.Index(line, ": "); i >= 0 {
					out.GPUType = strings.TrimSpace(line[i+2:])
				} else {
					out.GPUType = strings.TrimSpace(line)
				}
				break
			}
		}
	}
	return out
}

func disks(ctx context.Context) []Disk {
	if runtime.GOOS == "windows" {
		return nil
	}
	value := command(ctx, "df", "-Pk")
	lines := strings.Split(value, "\n")
	result := make([]Disk, 0, len(lines))
	for _, line := range lines[1:] {
		fields := strings.Fields(line)
		if len(fields) < 6 || !strings.HasPrefix(fields[0], "/dev/") {
			continue
		}
		total, _ := strconv.ParseFloat(fields[1], 64)
		free, _ := strconv.ParseFloat(fields[3], 64)
		result = append(result, Disk{Name: fields[0], MountPoint: strings.Join(fields[5:], " "), TotalGB: total / 1024 / 1024, FreeGB: free / 1024 / 1024})
	}
	return result
}

func markDisk(disks []Disk, location, tag string) {
	abs, err := filepath.Abs(location)
	if err != nil {
		return
	}
	best := -1
	for index, disk := range disks {
		mount, err := filepath.Abs(disk.MountPoint)
		if err != nil || (mount != "/" && abs != mount && !strings.HasPrefix(abs, mount+string(filepath.Separator))) {
			continue
		}
		if best == -1 || len(mount) > len(disks[best].MountPoint) {
			best = index
		}
	}
	if best >= 0 {
		disks[best].Tags = append(disks[best].Tags, tag)
	}
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func firstValue(text, key, fallback string) string {
	for _, line := range strings.Split(text, "\n") {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) == 2 && strings.TrimSpace(parts[0]) == key {
			return strings.TrimSpace(parts[1])
		}
	}
	return fallback
}
func parseFloat(value string) float64 {
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return 0
	}
	n, _ := strconv.ParseFloat(fields[0], 64)
	return n
}
func command(ctx context.Context, name string, args ...string) string {
	cmd := exec.CommandContext(ctx, name, args...)
	b, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
