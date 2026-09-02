/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package ollama

import (
	"archive/tar"
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"

	"github.com/arwos/ai-agent/internal/pkg/download"
	"github.com/arwos/ai-agent/internal/pkg/models"
)

type Service struct {
	client *download.Client
	root   string
}

func (s *Service) Root() string { return s.root }

type Model struct {
	Name  string   `json:"name"`
	Sizes []string `json:"sizes"`
}

type InstalledModel struct {
	Name     string `json:"name"`
	ID       string `json:"id"`
	Size     string `json:"size"`
	Modified string `json:"modified"`
}

func (s *Service) Catalog() ([]Model, error) {
	data, err := os.ReadFile(filepath.Join(s.root, "models.json"))
	if os.IsNotExist(err) {
		return []Model{}, nil
	}
	if err != nil {
		return nil, err
	}
	var models []Model
	if err := json.Unmarshal(data, &models); err != nil {
		return nil, err
	}
	return models, nil
}

func (s *Service) List(ctx context.Context, settings models.LocalLLMSettings) ([]InstalledModel, error) {
	output, err := s.run(ctx, settings, "list")
	if err != nil {
		return nil, err
	}
	items := make([]InstalledModel, 0)
	for index, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if index == 0 || strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		items = append(items, InstalledModel{
			Name:     fields[0],
			ID:       fields[1],
			Size:     fields[2] + " " + fields[3],
			Modified: strings.Join(fields[4:], " "),
		})
	}
	return items, nil
}

func (s *Service) Pull(ctx context.Context, settings models.LocalLLMSettings, name string) error {
	if !validModelName(name) {
		return fmt.Errorf("invalid Ollama model name")
	}
	_, err := s.run(ctx, settings, "pull", name)
	return err
}

func (s *Service) Remove(ctx context.Context, settings models.LocalLLMSettings, name string) error {
	if !validModelName(name) {
		return fmt.Errorf("invalid Ollama model name")
	}
	_, err := s.run(ctx, settings, "rm", name)
	return err
}

func (s *Service) RefreshModels(ctx context.Context) ([]Model, error) {
	html, err := s.client.Fetch(ctx, "https://ollama.com/library", 16<<20, nil)
	if err != nil {
		return nil, err
	}
	linkRE := regexp.MustCompile(`href="/library/([^"]+)"`)
	sizeRE := regexp.MustCompile(`>([eE]?[0-9]+(?:\.[0-9]+)?[bB])<`)
	matches := linkRE.FindAllSubmatchIndex(html, -1)
	models := make([]Model, 0, len(matches))
	seen := make(map[string]bool)
	for index, match := range matches {
		name := string(html[match[2]:match[3]])
		if seen[name] {
			continue
		}
		seen[name] = true
		end := len(html)
		if index+1 < len(matches) {
			end = matches[index+1][0]
		}
		seenSizes := make(map[string]bool)
		sizes := make([]string, 0)
		for _, sizeMatch := range sizeRE.FindAllSubmatch(html[match[1]:end], -1) {
			size := strings.ToLower(string(sizeMatch[1]))
			if !seenSizes[size] {
				seenSizes[size] = true
				sizes = append(sizes, size)
			}
		}
		if len(sizes) > 0 {
			models = append(models, Model{Name: name, Sizes: sizes})
		}
	}
	data, err := json.MarshalIndent(models, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(s.root, 0755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(s.root, "models.json"), append(data, '\n'), 0644); err != nil {
		return nil, err
	}
	return models, nil
}

func (s *Service) run(ctx context.Context, settings models.LocalLLMSettings, args ...string) ([]byte, error) {
	if settings.BinaryPath == "" {
		return nil, fmt.Errorf("Ollama binary path is not configured")
	}
	binaryPath, err := filepath.Abs(settings.BinaryPath)
	if err != nil {
		return nil, err
	}
	modelsPath, err := filepath.Abs(settings.ModelsPath)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(modelsPath, 0755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(s.root, 0755); err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, binaryPath, args...)
	cmd.Dir = filepath.Dir(binaryPath)
	cmd.Env = s.environment(ctx, settings, modelsPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("ollama %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return output, nil
}

func (s *Service) environment(ctx context.Context, settings models.LocalLLMSettings, modelsPath string) []string {
	envValues := make(map[string]string, len(settings.Env)+4)
	envValues["HOME"] = s.root
	for key, value := range settings.Env {
		envValues[key] = value
	}
	if device := nvidiaDeviceIDs(ctx); device != "" {
		envValues["CUDA_VISIBLE_DEVICES"] = device
		envValues["OLLAMA_LLM_LIBRARY"] = "cuda_v13"
	}
	envValues["OLLAMA_MODELS"] = modelsPath
	env := make([]string, 0, len(envValues))
	for key, value := range envValues {
		env = append(env, key+"="+value)
	}
	return env
}

func validModelName(name string) bool {
	return regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]*$`).MatchString(name)
}

func (s *Service) Start(ctx context.Context, settings models.LocalLLMSettings) (*os.Process, error) {
	if settings.BinaryPath == "" {
		return nil, fmt.Errorf("Ollama binary path is not configured")
	}
	binaryPath, err := filepath.Abs(settings.BinaryPath)
	if err != nil {
		return nil, err
	}
	modelsPath, err := filepath.Abs(settings.ModelsPath)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(modelsPath, 0755); err != nil {
		return nil, err
	}
	args := append([]string{"serve"}, settings.LaunchArgs...)
	cmd := exec.CommandContext(ctx, binaryPath, args...)
	envValues := make(map[string]string)
	if err := os.MkdirAll(s.root, 0755); err != nil {
		return nil, err
	}
	envValues["HOME"] = s.root
	for key, value := range settings.Env {
		envValues[key] = value
	}
	if device := nvidiaDeviceIDs(ctx); device != "" {
		envValues["CUDA_VISIBLE_DEVICES"] = device
		envValues["OLLAMA_LLM_LIBRARY"] = "cuda_v13"
	}
	envValues["OLLAMA_MODELS"] = modelsPath
	env := make([]string, 0, len(envValues))
	for key, value := range envValues {
		env = append(env, key+"="+value)
	}
	cmd.Env = env
	cmd.Dir = filepath.Dir(binaryPath)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd.Process, nil
}

func nvidiaDeviceIDs(ctx context.Context) string {
	output, err := exec.CommandContext(ctx, "nvidia-smi", "--query-gpu=index", "--format=csv,noheader,nounits").Output()
	if err != nil {
		return ""
	}
	ids := make([]string, 0)
	for _, line := range strings.Split(string(output), "\n") {
		if id := strings.TrimSpace(line); id != "" {
			ids = append(ids, id)
		}
	}
	return strings.Join(ids, ",")
}

type Progress func(string)

const copyBufferSize = 10 * 1024 * 1024

func New(root string) *Service {
	absoluteRoot := filepath.Join(root, "ollama")
	return &Service{client: download.New(&http.Client{Timeout: 20 * time.Minute}), root: absoluteRoot}
}

func (s *Service) Install(ctx context.Context, progress Progress) (string, error) {
	if progress == nil {
		progress = func(string) {}
	}
	progress("removing")
	if err := os.RemoveAll(s.root); err != nil {
		return "", err
	}
	artifacts, err := artifacts()
	if err != nil {
		return "", err
	}
	dir := s.root
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	for _, artifact := range artifacts {
		progress("downloading:" + artifact)
		if err := s.downloadAndExtract(ctx, dir, artifact, progress); err != nil {
			return "", err
		}
	}
	target, err := executable(dir)
	if err != nil {
		return "", err
	}
	progress("complete")
	return target, nil
}

func artifacts() ([]string, error) {
	switch runtime.GOOS {
	case "linux":
		if runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64" {
			return nil, fmt.Errorf("unsupported Linux architecture: %s", runtime.GOARCH)
		}
		result := []string{fmt.Sprintf("ollama-linux-%s", runtime.GOARCH)}
		if jetpack := jetpackVersion(); jetpack != "" {
			return append(result, fmt.Sprintf("ollama-linux-%s-jetpack%s", runtime.GOARCH, jetpack)), nil
		}
		if hasAMDGPU() {
			result = append(result, fmt.Sprintf("ollama-linux-%s-rocm", runtime.GOARCH))
		}
		return result, nil
	case "windows":
		if runtime.GOARCH != "amd64" {
			return nil, fmt.Errorf("Ollama standalone Windows binary is unavailable for %s", runtime.GOARCH)
		}
		return []string{"ollama-windows-amd64.zip"}, nil
	case "darwin":
		return []string{"Ollama-darwin.zip"}, nil
	default:
		return nil, fmt.Errorf("unsupported operating system: %s", runtime.GOOS)
	}
}

func (s *Service) downloadAndExtract(ctx context.Context, dir, name string, progress Progress) error {
	url := "https://ollama.com/download/" + name
	if !strings.HasSuffix(name, ".zip") {
		url += ".tar.zst"
	}
	tmp, err := os.CreateTemp(dir, ".ollama-download-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := s.client.Download(ctx, url, tmpName, 2<<30, func(percent float64) { progress(fmt.Sprintf("downloading:%s:%.0f", name, percent)) }); err != nil {
		return err
	}
	return extract(tmpName, dir, strings.HasSuffix(name, ".zip"))
}

func extract(archive, targetDir string, zipArchive bool) error {
	if zipArchive {
		z, err := zip.OpenReader(archive)
		if err != nil {
			return err
		}
		defer z.Close()
		for _, entry := range z.File {
			if entry.FileInfo().IsDir() {
				continue
			}
			target, ok := archivePath(targetDir, entry.Name)
			if !ok {
				return fmt.Errorf("unsafe Ollama archive path: %s", entry.Name)
			}
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}
			{
				in, err := entry.Open()
				if err != nil {
					return err
				}
				out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, entry.Mode())
				if err == nil {
					_, err = io.CopyBuffer(out, in, make([]byte, copyBufferSize))
					out.Close()
				}
				in.Close()
				if err != nil {
					return err
				}
			}
		}
		return ensureLibraryLinks(targetDir)
	}
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	zr, err := zstd.NewReader(f)
	if err != nil {
		return err
	}
	defer zr.Close()
	tr := tar.NewReader(zr)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if h.Typeflag == tar.TypeDir {
			continue
		}
		target, ok := archivePath(targetDir, h.Name)
		if !ok {
			return fmt.Errorf("unsafe Ollama archive path: %s", h.Name)
		}
		if h.Typeflag == tar.TypeSymlink {
			linkTarget, ok := archivePath(filepath.Dir(target), h.Linkname)
			if !ok {
				return fmt.Errorf("unsafe Ollama symlink: %s -> %s", h.Name, h.Linkname)
			}
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}
			_ = os.Remove(target)
			if err := os.Symlink(linkTarget, target); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		if h.Typeflag == tar.TypeReg || h.Typeflag == tar.TypeRegA {
			out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(h.Mode))
			if err != nil {
				return err
			}
			_, err = io.CopyBuffer(out, tr, make([]byte, copyBufferSize))
			cerr := out.Close()
			if err != nil {
				return err
			}
			if cerr != nil {
				return cerr
			}
		}
	}
	return ensureLibraryLinks(targetDir)
}

func ensureLibraryLinks(root string) error {
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		name := entry.Name()
		marker := strings.Index(name, ".so.")
		if marker < 0 || marker+4 >= len(name) {
			return nil
		}
		version := name[marker+4:]
		if version == "" || version[0] < '0' || version[0] > '9' {
			return nil
		}
		parts := strings.Split(version, ".")
		linkName := name[:marker+4] + parts[0]
		linkPath := filepath.Join(filepath.Dir(path), linkName)
		if info, err := os.Lstat(linkPath); err == nil {
			if info.Mode()&os.ModeSymlink == 0 {
				return nil
			}
			if _, err := os.Stat(linkPath); err == nil {
				return nil
			}
			if err := os.Remove(linkPath); err != nil {
				return err
			}
		} else if !os.IsNotExist(err) {
			return err
		}
		relativeTarget, err := filepath.Rel(filepath.Dir(linkPath), path)
		if err != nil {
			return err
		}
		return os.Symlink(relativeTarget, linkPath)
	})
}

func archivePath(root, name string) (string, bool) {
	path := filepath.Join(root, filepath.Clean(name))
	rel, err := filepath.Rel(root, path)
	return path, err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func executable(dir string) (string, error) {
	for _, path := range []string{filepath.Join(dir, "ollama"), filepath.Join(dir, "bin", "ollama"), filepath.Join(dir, "ollama.exe"), filepath.Join(dir, "bin", "ollama.exe"), filepath.Join(dir, "Ollama.app", "Contents", "Resources", "ollama")} {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path, nil
		}
	}
	return "", fmt.Errorf("Ollama archive has no executable")
}

func jetpackVersion() string {
	b, err := os.ReadFile("/etc/nv_tegra_release")
	if err != nil {
		return ""
	}
	if strings.Contains(string(b), "R36") {
		return "6"
	}
	if strings.Contains(string(b), "R35") {
		return "5"
	}
	return ""
}

func hasAMDGPU() bool {
	out, err := exec.Command("lspci", "-d", "1002:").Output()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(strings.ToLower(string(out)), "\n") {
		if strings.Contains(line, "vga compatible controller") || strings.Contains(line, "3d controller") || strings.Contains(line, "display controller") {
			return true
		}
	}
	return false
}
