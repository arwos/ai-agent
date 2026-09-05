/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.osspkg.com/goppy/v3/plugins/ws"
	"go.osspkg.com/goppy/v3/plugins/ws/event"
	"go.osspkg.com/logx"

	"github.com/arwos/ai-agent/internal/pkg/models"
)

type localLLMInstall func(context.Context, func(string)) (string, error)
type localLLMStart func(context.Context, models.LocalLLMSettings) (*os.Process, error)
type localLLMList func(context.Context, models.LocalLLMSettings) (any, error)
type localLLMPull func(context.Context, models.LocalLLMSettings, string, func(string)) error
type localLLMRemove func(context.Context, models.LocalLLMSettings, string) error

func (a *App) ollamaInstall(ev event.Event, meta ws.Meta) error {
	return a.installLocalLLM(ev, meta, "ollama", func(ctx context.Context, progress func(string)) (string, error) {
		return a.ollama.Install(ctx, progress)
	})
}

func (a *App) llamaInstall(ev event.Event, meta ws.Meta) error {
	return a.installLocalLLM(ev, meta, "llama", func(ctx context.Context, progress func(string)) (string, error) {
		return a.llama.Install(ctx, progress)
	})
}

func (a *App) localLLMList(ev event.Event, _ ws.Meta) error {
	profile, err := a.activeProfile()
	if err != nil {
		return err
	}
	items, err := a.store.LocalLLMList(profile)
	if err != nil {
		return err
	}
	return ev.Encode(items)
}

func (a *App) localLLMUpsert(ev event.Event, _ ws.Meta) error {
	var item models.LocalLLMSettings
	if err := ev.Decode(&item); err != nil {
		return err
	}
	profile, err := a.activeProfile()
	if err != nil {
		return err
	}
	item.ProfileID = profile
	if item.Runtime != "ollama" && item.Runtime != "llama" {
		return fmt.Errorf("unsupported local LLM runtime")
	}
	if item.ModelsPath == "" {
		item.ModelsPath = filepath.Join(a.systemInfo.Root(), item.Runtime+"-models")
	}
	if err := os.MkdirAll(item.ModelsPath, 0755); err != nil {
		return err
	}
	if len(item.Env) == 0 {
		item.Env = defaultLocalLLMEnv(item.Runtime)
	}
	item, err = a.store.LocalLLMUpsert(item)
	if err != nil {
		return err
	}
	if a.backgroundJobs != nil {
		if err := a.backgroundJobs.Restart("local-llm-" + item.Runtime); err != nil {
			logx.Error("failed to restart local LLM job after settings update", "runtime", item.Runtime, "err", err)
		}
	}
	return ev.Encode(item)
}

func (a *App) localLLMDelete(ev event.Event, _ ws.Meta) error {
	var request struct {
		Runtime string `json:"runtime"`
	}
	if err := ev.Decode(&request); err != nil {
		return err
	}
	profile, err := a.activeProfile()
	if err != nil {
		return err
	}
	if request.Runtime != "ollama" && request.Runtime != "llama" {
		return fmt.Errorf("unsupported local LLM runtime")
	}
	if err := a.store.LocalLLMDelete(profile, request.Runtime); err != nil {
		return err
	}
	return ev.Encode(map[string]bool{"deleted": true})
}

func (a *App) ollamaStart(ev event.Event, meta ws.Meta) error {
	return a.startLocalLLM(ev, meta, "ollama", "OllAMA", a.ollama.Start)
}

func (a *App) llamaStart(ev event.Event, meta ws.Meta) error {
	return a.startLocalLLM(ev, meta, "llama", "llama.app", a.llama.Start)
}

func (a *App) ollamaModelsRefresh(ev event.Event, meta ws.Meta) error {
	items, err := a.ollama.RefreshModels(meta.Context())
	if err != nil {
		return err
	}
	return ev.Encode(map[string]any{"models": items, "path": filepath.Join(a.ollama.Root(), "models.json")})
}

func (a *App) llamaModelsRefresh(ev event.Event, meta ws.Meta) error {
	items, err := a.llama.RefreshModels(meta.Context())
	if err != nil {
		return err
	}
	return ev.Encode(map[string]any{"catalog": items})
}

func (a *App) ollamaModelsList(ev event.Event, meta ws.Meta) error {
	settings, err := a.localLLMSettings("ollama")
	if err != nil {
		return err
	}
	catalog, err := a.ollama.Catalog()
	if err != nil {
		return err
	}
	installed, err := a.ollama.List(meta.Context(), settings)
	if err != nil {
		return err
	}
	return ev.Encode(map[string]any{"catalog": catalog, "installed": installed})
}

func (a *App) llamaModelsList(ev event.Event, meta ws.Meta) error {
	settings, err := a.localLLMSettings("llama")
	if err != nil {
		return err
	}
	catalog, err := a.llama.Catalog()
	if err != nil {
		return err
	}
	installed, err := a.llama.List(meta.Context(), settings)
	if err != nil {
		return err
	}
	return ev.Encode(map[string]any{"catalog": catalog, "installed": installed})
}

func (a *App) ollamaModelPull(ev event.Event, meta ws.Meta) error {
	var request struct {
		Name string `json:"name"`
	}
	if err := ev.Decode(&request); err != nil {
		return err
	}
	settings, err := a.localLLMSettings("ollama")
	if err != nil {
		return err
	}
	return a.pullLocalLLMModel(ev, meta, "ollama", request.Name, settings, a.ollama.PullWithProgress, func(ctx context.Context, s models.LocalLLMSettings) (any, error) { return a.ollama.List(ctx, s) }, false)
}

func (a *App) llamaModelPull(ev event.Event, meta ws.Meta) error {
	var request struct {
		ID string `json:"id"`
	}
	if err := ev.Decode(&request); err != nil {
		return err
	}
	settings, err := a.localLLMSettings("llama")
	if err != nil {
		return err
	}
	return a.pullLocalLLMModel(ev, meta, "llama", request.ID, settings, func(ctx context.Context, s models.LocalLLMSettings, model string, progress func(string)) error {
		return a.llama.PullWithProgress(ctx, s, model, progress)
	}, func(ctx context.Context, s models.LocalLLMSettings) (any, error) { return a.llama.List(ctx, s) }, true)
}

func (a *App) ollamaModelRemove(ev event.Event, meta ws.Meta) error {
	var request struct {
		Name string `json:"name"`
	}
	if err := ev.Decode(&request); err != nil {
		return err
	}
	settings, err := a.localLLMSettings("ollama")
	if err != nil {
		return err
	}
	return a.removeLocalLLMModel(ev, meta, request.Name, settings, a.ollama.Remove, func(ctx context.Context, s models.LocalLLMSettings) (any, error) { return a.ollama.List(ctx, s) }, false)
}

func (a *App) llamaModelRemove(ev event.Event, meta ws.Meta) error {
	var request struct {
		ID string `json:"id"`
	}
	if err := ev.Decode(&request); err != nil {
		return err
	}
	settings, err := a.localLLMSettings("llama")
	if err != nil {
		return err
	}
	return a.removeLocalLLMModel(ev, meta, request.ID, settings, a.llama.Remove, func(ctx context.Context, s models.LocalLLMSettings) (any, error) { return a.llama.List(ctx, s) }, true)
}

func (a *App) installLocalLLM(ev event.Event, meta ws.Meta, runtime string, install localLLMInstall) error {
	path, err := install(meta.Context(), func(status string) {
		a.publish(StreamMessage{Type: "local_llm.install", Payload: map[string]string{"runtime": runtime, "status": status}})
	})
	if err != nil {
		return err
	}
	settings, err := a.saveLocalLLMBinaryPath(runtime, path)
	if err != nil {
		return err
	}
	return ev.Encode(settings)
}

func (a *App) startLocalLLM(ev event.Event, meta ws.Meta, runtime, displayName string, start localLLMStart) error {
	settings, err := a.localLLMSettings(runtime)
	if err != nil {
		return err
	}
	if !settings.Enabled {
		return fmt.Errorf("%s is disabled", displayName)
	}
	process, err := start(meta.Context(), settings)
	if err != nil {
		return err
	}
	return ev.Encode(map[string]any{"started": true, "pid": process.Pid, "path": settings.BinaryPath})
}

func (a *App) pullLocalLLMModel(ev event.Event, meta ws.Meta, runtime, model string, settings models.LocalLLMSettings, pull localLLMPull, list localLLMList, restart bool) error {
	a.publish(StreamMessage{Type: "local_llm.model", Payload: map[string]string{"runtime": runtime, "status": "starting", "model": model}})
	if err := pull(meta.Context(), settings, model, func(status string) {
		a.publish(StreamMessage{Type: "local_llm.model_pull", Payload: map[string]string{"runtime": runtime, "status": status}})
	}); err != nil {
		a.publish(StreamMessage{Type: "local_llm.model", Payload: map[string]string{"runtime": runtime, "status": "error", "model": model}})
		return err
	}
	a.publish(StreamMessage{Type: "local_llm.model", Payload: map[string]string{"runtime": runtime, "status": "complete", "model": model}})
	installed, err := list(meta.Context(), settings)
	if err != nil {
		return err
	}
	if restart {
		a.restartConfiguredLocalLLM(settings)
	}
	return ev.Encode(map[string]any{"installed": installed})
}

func (a *App) removeLocalLLMModel(ev event.Event, meta ws.Meta, model string, settings models.LocalLLMSettings, remove localLLMRemove, list localLLMList, restart bool) error {
	if err := remove(meta.Context(), settings, model); err != nil {
		return err
	}
	installed, err := list(meta.Context(), settings)
	if err != nil {
		return err
	}
	if restart {
		a.restartConfiguredLocalLLM(settings)
	}
	return ev.Encode(map[string]any{"installed": installed})
}

func (a *App) localLLMSettings(runtime string) (models.LocalLLMSettings, error) {
	profileID, err := a.activeProfile()
	if err != nil {
		return models.LocalLLMSettings{}, err
	}
	items, err := a.store.LocalLLMList(profileID)
	if err != nil {
		return models.LocalLLMSettings{}, err
	}
	for _, item := range items {
		if item.Runtime == runtime {
			return item, nil
		}
	}
	return models.LocalLLMSettings{}, fmt.Errorf("%s settings are not configured", runtime)
}

func envFromLines(lines []string) map[string]string {
	env := make(map[string]string, len(lines))
	for _, line := range lines {
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 && strings.TrimSpace(parts[0]) != "" {
			env[strings.TrimSpace(parts[0])] = parts[1]
		}
	}
	return env
}

func defaultOllamaEnv() map[string]string {
	return envFromLines([]string{
		"OLLAMA_CONTEXT_LENGTH=131072", "OLLAMA_ORIGINS=*", "OLLAMA_HOST=127.0.0.1:20001",
		"OLLAMA_VULKAN=0", "OLLAMA_FLASH_ATTENTION=1", "OLLAMA_NUM_PARALLEL=1",
		"OLLAMA_KV_CACHE_TYPE=q4_0", "OLLAMA_NO_CLOUD=1", "OLLAMA_NUM_GPU=99", "OLLAMA_NEW_ESTIMATES=1",
	})
}

func defaultLlamaEnv() map[string]string {
	return envFromLines([]string{
		"LLAMA_ARG_CTX_SIZE=131072", "LLAMA_ARG_FLASH_ATTN=1", "LLAMA_ARG_N_GPU_LAYERS=99",
		"LLAMA_ARG_CACHE_TYPE_V=q4_0", "LLAMA_ARG_CACHE_TYPE_K=q4_0", "LLAMA_ARG_HOST=127.0.0.1",
		"LLAMA_ARG_PORT=20002", "LLAMA_ARG_CORS_ORIGINS=*",
	})
}

func defaultLocalLLMEnv(runtime string) map[string]string {
	if runtime == "llama" {
		return defaultLlamaEnv()
	}
	return defaultOllamaEnv()
}
func (a *App) saveLocalLLMBinaryPath(runtime, path string) (models.LocalLLMSettings, error) {
	profileID, err := a.activeProfile()
	if err != nil {
		return models.LocalLLMSettings{}, err
	}
	items, err := a.store.LocalLLMList(profileID)
	if err != nil {
		return models.LocalLLMSettings{}, err
	}
	item := models.LocalLLMSettings{ProfileID: profileID, Runtime: runtime, Enabled: true, LaunchArgs: []string{}, Env: map[string]string{}}
	for _, existing := range items {
		if existing.Runtime == runtime {
			item = existing
			break
		}
	}
	item.BinaryPath = path
	if item.ModelsPath == "" {
		item.ModelsPath = filepath.Join(a.systemInfo.Root(), runtime+"-models")
		if err := os.MkdirAll(item.ModelsPath, 0755); err != nil {
			return models.LocalLLMSettings{}, err
		}
	}
	if len(item.Env) == 0 {
		item.Env = defaultLocalLLMEnv(runtime)
	}
	item, err = a.store.LocalLLMUpsert(item)
	if err != nil {
		return models.LocalLLMSettings{}, err
	}
	if a.backgroundJobs != nil {
		if err := a.backgroundJobs.Restart("local-llm-" + runtime); err != nil {
			logx.Error("failed to restart local LLM job after installation", "runtime", runtime, "err", err)
		}
	}
	return item, nil
}
