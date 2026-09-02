/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package llama

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"

	"github.com/arwos/ai-agent/internal/pkg/download"
	"github.com/arwos/ai-agent/internal/pkg/models"
	"github.com/arwos/ai-agent/internal/pkg/utils"
)

const bucketURL = "https://huggingface.co/buckets/ggml-org/install.sh/resolve"

type Service struct {
	client *download.Client
	root   string
}

type Progress func(string)

type CatalogModel struct {
	ID        string `json:"id"`
	Downloads int    `json:"downloads"`
}

type InstalledModel struct {
	ID   string `json:"id"`
	Size int64  `json:"size"`
}

func (s *Service) Catalog() ([]CatalogModel, error) {
	data, err := os.ReadFile(filepath.Join(s.root, "models.json"))
	if os.IsNotExist(err) {
		return []CatalogModel{}, nil
	}
	if err != nil {
		return nil, err
	}
	var items []CatalogModel
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *Service) RefreshModels(ctx context.Context) ([]CatalogModel, error) {
	next := "https://huggingface.co/api/models?author=ggml-org&limit=1000&sort=downloads&direction=-1"
	items := make([]CatalogModel, 0)
	seen := make(map[string]bool)
	for next != "" {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, next, nil)
		if err != nil {
			return nil, err
		}
		resp, err := s.client.HTTP.Do(req)
		if err != nil {
			return nil, err
		}
		body, readErr := utils.ReadAllResponse(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return nil, readErr
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("Hugging Face models: %s", resp.Status)
		}
		var page []struct {
			ID        string `json:"id"`
			Downloads int    `json:"downloads"`
		}
		if err := json.Unmarshal(body, &page); err != nil {
			return nil, err
		}
		for _, item := range page {
			if item.ID != "" && !seen[item.ID] {
				seen[item.ID] = true
				items = append(items, CatalogModel{ID: item.ID, Downloads: item.Downloads})
			}
		}
		next = nextPage(resp.Header.Get("Link"))
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Downloads > items[j].Downloads })
	data, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(s.root, 0755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(s.root, "models.json"), append(data, '\n'), 0644); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *Service) List(ctx context.Context, settings models.LocalLLMSettings) ([]InstalledModel, error) {
	output, err := s.command(ctx, settings, "cli", "--cache-list")
	if err != nil {
		return nil, err
	}
	cacheRoot, err := s.cacheRoot(settings)
	if err != nil {
		return nil, err
	}
	items := make([]InstalledModel, 0)
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || !strings.HasSuffix(fields[0], ".") {
			continue
		}
		id := fields[1]
		size := int64(0)
		cacheDir := filepath.Join(cacheRoot, "models--"+strings.ReplaceAll(id, "/", "--"))
		if stat, statErr := os.Stat(cacheDir); statErr == nil && stat.IsDir() {
			size, err = directorySize(cacheDir)
			if err != nil {
				return nil, err
			}
		}
		items = append(items, InstalledModel{ID: id, Size: size})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

func (s *Service) Pull(ctx context.Context, settings models.LocalLLMSettings, modelID string) error {
	if !validModelID(modelID) {
		return fmt.Errorf("invalid llama.app model ID")
	}
	// The CLI treats a zero prediction limit inconsistently across versions
	// (some builds interpret -n 0 as unlimited). Generate one token with a
	// non-empty prompt so the process exits after the model has been cached.
	_, err := s.command(ctx, settings, "cli", "-hf", modelID, "-n", "1", "-p", "ok", "--single-turn")
	return err
}

func (s *Service) Remove(ctx context.Context, settings models.LocalLLMSettings, modelID string) error {
	if !validModelID(modelID) {
		return fmt.Errorf("invalid llama.app model ID")
	}
	cacheRoot, err := s.cacheRoot(settings)
	if err != nil {
		return err
	}
	dir := filepath.Join(cacheRoot, "models--"+strings.ReplaceAll(modelID, "/", "--"))
	rel, err := filepath.Rel(cacheRoot, dir)
	if err != nil || rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("invalid llama.app model cache path")
	}
	return os.RemoveAll(dir)
}

func (s *Service) Start(ctx context.Context, settings models.LocalLLMSettings) (*os.Process, error) {
	if settings.BinaryPath == "" {
		return nil, fmt.Errorf("llama.app binary path is not configured")
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
	if err := os.MkdirAll(s.root, 0755); err != nil {
		return nil, err
	}
	cmd.Env, cmd.Dir = s.environment(settings, modelsPath), filepath.Dir(binaryPath)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd.Process, nil
}

func (s *Service) environment(settings models.LocalLLMSettings, modelsPath string) []string {
	envValues := make(map[string]string, len(settings.Env)+3)
	envValues["HOME"] = s.root
	for key, value := range settings.Env {
		envValues[key] = value
	}
	envValues["LLAMA_ARG_MODELS_DIR"] = modelsPath
	envValues["LLAMA_CACHE"] = modelsPath
	env := make([]string, 0, len(envValues))
	for key, value := range envValues {
		env = append(env, key+"="+value)
	}
	return env
}

func (s *Service) cacheRoot(settings models.LocalLLMSettings) (string, error) {
	modelsPath, err := filepath.Abs(settings.ModelsPath)
	if err != nil {
		return "", err
	}
	return modelsPath, nil
}

func (s *Service) command(ctx context.Context, settings models.LocalLLMSettings, args ...string) ([]byte, error) {
	if settings.BinaryPath == "" {
		return nil, fmt.Errorf("llama.app binary path is not configured")
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
	cmd.Env = s.environment(settings, modelsPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("llama.app %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return output, nil
}

func nextPage(value string) string {
	for _, part := range strings.Split(value, ",") {
		if strings.Contains(part, `rel="next"`) {
			match := regexp.MustCompile(`<([^>]+)>`).FindStringSubmatch(part)
			if len(match) == 2 {
				return match[1]
			}
		}
	}
	return ""
}

func validModelID(value string) bool {
	return regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*/[A-Za-z0-9][A-Za-z0-9._-]*$`).MatchString(value)
}

func directorySize(root string) (int64, error) {
	var size int64
	err := filepath.Walk(root, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			size += info.Size()
		}
		return nil
	})
	return size, err
}

func New(root string) *Service {
	absoluteRoot := filepath.Join(root, "llama")
	return &Service{client: download.New(&http.Client{Timeout: 20 * time.Minute}), root: absoluteRoot}
}

func (s *Service) Install(ctx context.Context, progress Progress) (string, error) {
	if progress == nil {
		progress = func(string) {}
	}
	osName, arch, err := platform()
	if err != nil {
		return "", err
	}
	progress("removing")
	if err := os.RemoveAll(s.root); err != nil {
		return "", err
	}
	if err := os.MkdirAll(s.root, 0755); err != nil {
		return "", err
	}
	workDir, err := os.MkdirTemp(s.root, ".install-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(workDir)
	progress("checking")
	version, err := s.latest(ctx)
	if err != nil {
		return "", err
	}
	progress("detecting")
	asset, err := s.selectAsset(ctx, workDir, version, arch, osName)
	if err != nil {
		return "", err
	}
	target := filepath.Join(s.root, "llama")
	progress("downloading:" + asset)
	if err := s.downloadBinary(ctx, version, asset, target, progress); err != nil {
		return "", err
	}
	progress("complete")
	return target, nil
}

func platform() (string, string, error) {
	osName := map[string]string{"linux": "linux", "darwin": "macos", "freebsd": "freebsd"}[runtime.GOOS]
	if osName == "" {
		return "", "", fmt.Errorf("llama.app installer does not support %s", runtime.GOOS)
	}
	arch := map[string]string{"amd64": "x86_64", "arm64": "aarch64"}[runtime.GOARCH]
	if arch == "" {
		return "", "", fmt.Errorf("llama.app installer does not support %s", runtime.GOARCH)
	}
	return osName, arch, nil
}

func (s *Service) latest(ctx context.Context) (string, error) {
	value, err := s.client.Fetch(ctx, bucketURL+"/latest", 2<<30, nil)
	if err != nil {
		return "", fmt.Errorf("download llama.app version: %w", err)
	}
	version := strings.TrimSpace(string(value))
	if version == "" {
		return "", fmt.Errorf("llama.app installer returned an empty version")
	}
	return version, nil
}

func (s *Service) selectAsset(ctx context.Context, dir, version, arch, osName string) (string, error) {
	prefix := arch + "/" + osName + "/"
	if osName == "macos" {
		if metal := metalConfig(); metal != "" {
			return prefix + "metal/" + metal + "/llama-app.zst", nil
		}
		return s.cpuAsset(ctx, dir, version, prefix)
	}
	if osName == "linux" {
		for _, backend := range []string{"cuda", "rocm", "vulkan"} {
			if asset, err := s.probeAsset(ctx, dir, version, prefix, backend); err == nil {
				return asset, nil
			}
		}
	}
	return s.cpuAsset(ctx, dir, version, prefix)
}

func (s *Service) probeAsset(ctx context.Context, dir, version, prefix, backend string) (string, error) {
	probe := filepath.Join(dir, backend+"-probe")
	if err := s.downloadBinary(ctx, version, prefix+backend+"/probe/probe.zst", probe, nil); err != nil {
		return "", err
	}
	config, err := s.run(ctx, dir, probe)
	if err != nil || strings.TrimSpace(config) == "" {
		return "", fmt.Errorf("%s probe failed: %s (output: %q)", backend, errorText(err), config)
	}
	if backend == "vulkan" {
		featcode := filepath.Join(dir, "featcode")
		if err := s.downloadBinary(ctx, version, prefix+"featcode", featcode, nil); err != nil {
			return "", err
		}
		config, err = s.run(ctx, dir, featcode)
		if err != nil || strings.TrimSpace(config) == "" {
			return "", fmt.Errorf("vulkan feature probe failed: %s (output: %q)", errorText(err), config)
		}
	}
	return prefix + backend + "/" + strings.Fields(config)[0] + "/llama-app.zst", nil
}

func (s *Service) cpuAsset(ctx context.Context, dir, version, prefix string) (string, error) {
	featcode := filepath.Join(dir, "featcode")
	if err := s.downloadBinary(ctx, version, prefix+"featcode", featcode, nil); err != nil {
		return "", err
	}
	config, err := s.run(ctx, dir, featcode)
	if err != nil || strings.TrimSpace(config) == "" {
		return "", fmt.Errorf("CPU feature probe failed: %s (output: %q)", errorText(err), config)
	}
	return prefix + "cpu/" + strings.Fields(config)[0] + "/llama-app.zst", nil
}

func (s *Service) downloadBinary(ctx context.Context, version, asset, target string, progress Progress) error {
	if progress == nil {
		progress = func(string) {}
	}
	progress("downloading:" + asset + ":0")
	data, err := s.client.Fetch(ctx, bucketURL+"/"+version+"/"+asset, 2<<30, func(percent float64) {
		progress(fmt.Sprintf("downloading:%s:%.0f", asset, percent))
	})
	if err != nil {
		return err
	}
	if strings.HasSuffix(asset, ".zst") {
		decoder, err := zstd.NewReader(nil)
		if err != nil {
			return err
		}
		data, err = decoder.DecodeAll(data, nil)
		decoder.Close()
		if err != nil {
			return err
		}
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), ".llama-download-")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err = tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Chmod(0755); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, target); err != nil {
		return err
	}
	progress("downloading:" + asset + ":100")
	return nil
}

func (s *Service) run(ctx context.Context, dir, program string, args ...string) (string, error) {
	currDir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	if len(program) > 0 && program[0] != '/' {
		program = filepath.Join(currDir, program)
	}
	cmd := exec.CommandContext(ctx, program, args...)
	if len(dir) > 0 && dir[0] != '/' {
		dir = filepath.Join(currDir, dir)
	}
	cmd.Dir = dir
	output, err := cmd.Output()
	return strings.TrimSpace(string(output)), err
}

func errorText(err error) string {
	if err == nil {
		return "empty output"
	}
	return err.Error()
}

func metalConfig() string {
	if runtime.GOOS != "darwin" {
		return ""
	}
	output, err := exec.Command("sysctl", "-n", "machdep.cpu.brand_string").Output()
	if err != nil {
		return ""
	}
	fields := strings.Fields(string(output))
	if len(fields) < 2 || fields[0] != "Apple" {
		return ""
	}
	model := strings.ToLower(fields[1])
	if strings.HasPrefix(model, "m1") || strings.HasPrefix(model, "m2") || strings.HasPrefix(model, "m3") || strings.HasPrefix(model, "m4") || strings.HasPrefix(model, "m5") || model == "a18" {
		return model
	}
	return ""
}
