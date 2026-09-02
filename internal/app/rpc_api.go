/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package app

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	mime2 "go.osspkg.com/goppy/v3/pkg/mime"
	"go.osspkg.com/goppy/v3/plugins/ws"
	"go.osspkg.com/goppy/v3/plugins/ws/event"
	"go.osspkg.com/logx"

	"github.com/arwos/ai-agent/internal/pkg/configstore"
	"github.com/arwos/ai-agent/internal/pkg/dialog"
	"github.com/arwos/ai-agent/internal/pkg/interaction"
	"github.com/arwos/ai-agent/internal/pkg/llm"
	"github.com/arwos/ai-agent/internal/pkg/mcp"
	"github.com/arwos/ai-agent/internal/pkg/models"
	"github.com/arwos/ai-agent/internal/pkg/prompts"
	settingspkg "github.com/arwos/ai-agent/internal/pkg/settings"
	"github.com/arwos/ai-agent/internal/pkg/toolexecutor"
	"github.com/arwos/ai-agent/internal/pkg/updater"
	"github.com/arwos/ai-agent/internal/pkg/utils"
	"github.com/arwos/ai-agent/internal/pkg/workspace"
)

func exportRaw(items any) ([]json.RawMessage, error) {
	b, err := json.Marshal(items)
	if err != nil {
		return nil, err
	}
	var values []map[string]any
	if err = json.Unmarshal(b, &values); err != nil {
		return nil, err
	}
	out := make([]json.RawMessage, 0, len(values))
	for _, value := range values {
		delete(value, "profileId")
		delete(value, "profile_id")
		raw, e := json.Marshal(value)
		if e != nil {
			return nil, e
		}
		out = append(out, raw)
	}
	return out, nil
}

func uniqueStrings(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func (a *App) updateStatus(ev event.Event, _ ws.Meta) error {
	// A startup check is intentionally non-blocking. Refresh here so opening the
	// version dialog shortly after startup still has current release data.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	release, err := a.updater.Check(ctx)
	if err != nil {
		return err
	}
	return ev.Encode(release)
}

func (a *App) updateApply(ev event.Event, _ ws.Meta) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	script, err := a.updater.Prepare(ctx)
	if err != nil {
		return err
	}
	if err := ev.Encode(map[string]bool{"started": true}); err != nil {
		return err
	}
	go func() {
		time.Sleep(500 * time.Millisecond) // let the WebSocket response reach the browser
		if err := updater.StartScript(script); err != nil {
			logx.Error("failed to start update script", "err", err)
			return
		}
		logx.Info("update script started", "script", script)
		time.Sleep(500 * time.Millisecond)
		os.Exit(0)
	}()
	return nil
}

func (a *App) ollamaInstall(ev event.Event, meta ws.Meta) error {
	path, err := a.ollama.Install(meta.Context(), func(status string) {
		a.publish(StreamMessage{Type: "local_llm.install", Payload: map[string]string{"runtime": "ollama", "status": status}})
	})
	if err != nil {
		return err
	}
	settings, err := a.saveLocalLLMBinaryPath("ollama", path)
	if err != nil {
		return err
	}
	return ev.Encode(settings)
}

func (a *App) llamaInstall(ev event.Event, meta ws.Meta) error {
	path, err := a.llama.Install(meta.Context(), func(status string) {
		a.publish(StreamMessage{Type: "local_llm.install", Payload: map[string]string{"runtime": "llama", "status": status}})
	})
	if err != nil {
		return err
	}
	settings, err := a.saveLocalLLMBinaryPath("llama", path)
	if err != nil {
		return err
	}
	return ev.Encode(settings)
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

func defaultOllamaEnv() map[string]string {
	lines := []string{
		"OLLAMA_CONTEXT_LENGTH=131072",
		"OLLAMA_ORIGINS=*",
		"OLLAMA_HOST=127.0.0.1:20001",
		"OLLAMA_VULKAN=0",
		"OLLAMA_FLASH_ATTENTION=1",
		"OLLAMA_NUM_PARALLEL=1",
		"OLLAMA_KV_CACHE_TYPE=q4_0",
		"OLLAMA_NO_CLOUD=1",
		"OLLAMA_NUM_GPU=99",
		"OLLAMA_NEW_ESTIMATES=1",
	}
	env := make(map[string]string, len(lines))
	for _, line := range lines {
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 && strings.TrimSpace(parts[0]) != "" {
			env[strings.TrimSpace(parts[0])] = parts[1]
		}
	}
	return env
}

func defaultLlamaEnv() map[string]string {
	lines := []string{
		"LLAMA_ARG_CTX_SIZE=131072",
		"LLAMA_ARG_FLASH_ATTN=1",
		"LLAMA_ARG_N_GPU_LAYERS=99",
		"LLAMA_ARG_CACHE_TYPE_V=q4_0",
		"LLAMA_ARG_CACHE_TYPE_K=q4_0",
		"LLAMA_ARG_HOST=127.0.0.1",
		"LLAMA_ARG_PORT=20002",
		"LLAMA_ARG_CORS_ORIGINS=*",
	}
	env := make(map[string]string, len(lines))
	for _, line := range lines {
		parts := strings.SplitN(line, "=", 2)
		env[parts[0]] = parts[1]
	}
	return env
}

func defaultLocalLLMEnv(runtime string) map[string]string {
	if runtime == "llama" {
		return defaultLlamaEnv()
	}
	return defaultOllamaEnv()
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
	profileID, err := a.activeProfile()
	if err != nil {
		return err
	}
	items, err := a.store.LocalLLMList(profileID)
	if err != nil {
		return err
	}
	for _, settings := range items {
		if settings.Runtime != "ollama" {
			continue
		}
		if !settings.Enabled {
			return fmt.Errorf("OllAMA is disabled")
		}
		process, startErr := a.ollama.Start(meta.Context(), settings)
		if startErr != nil {
			return startErr
		}
		return ev.Encode(map[string]any{"started": true, "pid": process.Pid, "path": settings.BinaryPath})
	}
	return fmt.Errorf("Ollama settings are not configured")
}

func (a *App) llamaStart(ev event.Event, meta ws.Meta) error {
	profileID, err := a.activeProfile()
	if err != nil {
		return err
	}
	items, err := a.store.LocalLLMList(profileID)
	if err != nil {
		return err
	}
	for _, settings := range items {
		if settings.Runtime != "llama" {
			continue
		}
		if !settings.Enabled {
			return fmt.Errorf("llama.app is disabled")
		}
		process, startErr := a.llama.Start(meta.Context(), settings)
		if startErr != nil {
			return startErr
		}
		return ev.Encode(map[string]any{"started": true, "pid": process.Pid, "path": settings.BinaryPath})
	}
	return fmt.Errorf("llama.app settings are not configured")
}

func (a *App) ollamaModelsRefresh(ev event.Event, meta ws.Meta) error {
	models, err := a.ollama.RefreshModels(meta.Context())
	if err != nil {
		return err
	}
	return ev.Encode(map[string]any{"models": models, "path": filepath.Join(a.ollama.Root(), "models.json")})
}

func (a *App) ollamaModelsList(ev event.Event, meta ws.Meta) error {
	settings, err := a.ollamaSettings()
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

func (a *App) ollamaModelPull(ev event.Event, meta ws.Meta) error {
	var request struct {
		Name string `json:"name"`
	}
	if err := ev.Decode(&request); err != nil {
		return err
	}
	settings, err := a.ollamaSettings()
	if err != nil {
		return err
	}
	if err := a.ollama.Pull(meta.Context(), settings, request.Name); err != nil {
		return err
	}
	installed, err := a.ollama.List(meta.Context(), settings)
	if err != nil {
		return err
	}
	return ev.Encode(map[string]any{"installed": installed})
}

func (a *App) ollamaModelRemove(ev event.Event, meta ws.Meta) error {
	var request struct {
		Name string `json:"name"`
	}
	if err := ev.Decode(&request); err != nil {
		return err
	}
	settings, err := a.ollamaSettings()
	if err != nil {
		return err
	}
	if err := a.ollama.Remove(meta.Context(), settings, request.Name); err != nil {
		return err
	}
	installed, err := a.ollama.List(meta.Context(), settings)
	if err != nil {
		return err
	}
	return ev.Encode(map[string]any{"installed": installed})
}

func (a *App) ollamaSettings() (models.LocalLLMSettings, error) {
	profileID, err := a.activeProfile()
	if err != nil {
		return models.LocalLLMSettings{}, err
	}
	items, err := a.store.LocalLLMList(profileID)
	if err != nil {
		return models.LocalLLMSettings{}, err
	}
	for _, item := range items {
		if item.Runtime == "ollama" {
			return item, nil
		}
	}
	return models.LocalLLMSettings{}, fmt.Errorf("Ollama settings are not configured")
}

func (a *App) llamaModelsRefresh(ev event.Event, meta ws.Meta) error {
	items, err := a.llama.RefreshModels(meta.Context())
	if err != nil {
		return err
	}
	return ev.Encode(map[string]any{"catalog": items})
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
	if err := a.llama.Pull(meta.Context(), settings, request.ID); err != nil {
		return err
	}
	installed, err := a.llama.List(meta.Context(), settings)
	if err != nil {
		return err
	}
	// Router mode discovers cached models at startup. Restart the managed
	// llama.app job so a newly downloaded model becomes available immediately.
	a.restartConfiguredLocalLLM(settings)
	return ev.Encode(map[string]any{"installed": installed})
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
	if err := a.llama.Remove(meta.Context(), settings, request.ID); err != nil {
		return err
	}
	installed, err := a.llama.List(meta.Context(), settings)
	if err != nil {
		return err
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

func containsString(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func (a *App) settingsExport(ev event.Event, _ ws.Meta) error {
	var request struct {
		Include []string `json:"include"`
	}
	if err := ev.Decode(&request); err != nil {
		return err
	}
	include := make(map[string]bool)
	for _, key := range request.Include {
		include[key] = true
	}
	want := func(key string) bool { return len(include) == 0 || include[key] }
	bundle := settingspkg.New()
	profile, err := a.activeProfile()
	if err != nil {
		return err
	}
	if a.store != nil {
		settings, err := a.store.Settings()
		if err != nil {
			return err
		}
		if want("appSettings") {
			bundle.AppSettings, err = json.Marshal(settings)
		}
		if err != nil {
			return err
		}
		providers, err := a.store.ProfileProviders(profile)
		if err != nil {
			return err
		}
		for _, provider := range providers {
			if !want("providers") {
				break
			}
			item := map[string]any{"id": provider.ID, "name": provider.Name, "kind": provider.Kind, "baseUrl": provider.BaseURL, "models": provider.Models, "enabled": provider.Enabled, "proxyId": provider.ProxyID}
			_, key, e := a.store.ProfileProviderSecret(profile, provider.ID)
			if e != nil {
				return e
			}
			item["apiKeyB64"] = settingspkg.EncodeSecret(key)
			raw, e := json.Marshal(item)
			if e != nil {
				return e
			}
			bundle.Providers = append(bundle.Providers, raw)
		}
		mcp, err := a.store.ProfileMCP(profile)
		if err != nil {
			return err
		}
		if want("mcp") {
			bundle.MCP, err = exportRaw(mcp)
		}
		if err != nil {
			return err
		}
		agents, err := a.store.Agents(profile)
		if err != nil {
			return err
		}
		if want("agents") {
			bundle.Agents, err = exportRaw(agents)
		}
		if err != nil {
			return err
		}
		presets, err := a.store.Presets(profile)
		if err != nil {
			return err
		}
		if want("presets") {
			bundle.Presets, err = exportRaw(presets)
		}
		if err != nil {
			return err
		}
		skills, err := a.skills.List(profile)
		if err != nil {
			return err
		}
		if want("skills") {
			for i := range skills {
				content, getErr := a.skills.Get(profile, skills[i].ID)
				if getErr != nil {
					return getErr
				}
				skills[i].Content = content
			}
			bundle.Skills, err = exportRaw(skills)
		}
		if err != nil {
			return err
		}
		notes, err := a.memory.Notes(profile, "")
		if err != nil {
			return err
		}
		if want("notes") {
			bundle.Notes, err = exportRaw(notes)
		}
		if err != nil {
			return err
		}
		topics, err := a.memory.Topics(profile, "")
		if err != nil {
			return err
		}
		if want("topics") {
			bundle.Topics, err = exportRaw(topics)
		}
		if err != nil {
			return err
		}
		kb, _, err := a.knowledge.List(profile, "", "", nil, 100000)
		if err != nil {
			return err
		}
		if want("kb") {
			bundle.KB, err = exportRaw(kb)
		}
		if err != nil {
			return err
		}
	}
	if a.proxies != nil && want("proxies") {
		proxies, err := a.proxies.List(profile)
		if err != nil {
			return err
		}
		for _, proxy := range proxies {
			item := map[string]any{"id": proxy.ID, "name": proxy.Name, "description": proxy.Description, "type": proxy.Type, "host": proxy.Host, "port": proxy.Port, "username": proxy.Username, "insecureSkipVerify": proxy.InsecureSkipVerify}
			secret, e := a.proxies.Secret(profile, proxy.ID)
			if e != nil {
				return e
			}
			item["passwordB64"] = settingspkg.EncodeSecret(secret.Password)
			raw, e := json.Marshal(item)
			if e != nil {
				return e
			}
			bundle.Proxies = append(bundle.Proxies, raw)
		}
	}
	data, err := settingspkg.Marshal(bundle)
	if err != nil {
		return err
	}
	return ev.Encode(map[string]any{"filename": "arwos-settings.json", "content": string(data)})
}

func (a *App) settingsImport(ev event.Event, _ ws.Meta) error {
	var q struct {
		Content string   `json:"content"`
		Include []string `json:"include"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	bundle, err := settingspkg.Unmarshal([]byte(q.Content))
	if err != nil {
		return err
	}
	selected := make(map[string]bool)
	for _, key := range q.Include {
		selected[key] = true
	}
	if len(selected) > 0 {
		if !selected["appSettings"] {
			bundle.AppSettings = nil
		}
		if !selected["providers"] {
			bundle.Providers = nil
		}
		if !selected["mcp"] {
			bundle.MCP = nil
		}
		if !selected["proxies"] {
			bundle.Proxies = nil
		}
		if !selected["agents"] {
			bundle.Agents = nil
		}
		if !selected["presets"] {
			bundle.Presets = nil
		}
		if !selected["skills"] {
			bundle.Skills = nil
		}
		if !selected["kb"] {
			bundle.KB = nil
		}
		if !selected["notes"] {
			bundle.Notes = nil
		}
		if !selected["topics"] {
			bundle.Topics = nil
		}
	}
	profile, err := a.activeProfile()
	if err != nil {
		return err
	}
	result := map[string]int{"updated": 0, "skipped": 0}
	importID := func(id string) string { return profileImportID(id, profile) }
	newID := func(id, prefix string) string {
		if id == "" {
			return entityID(prefix)
		}
		return importID(id)
	}
	if len(bundle.AppSettings) > 0 {
		var items []models.AppSettings
		if err := json.Unmarshal(bundle.AppSettings, &items); err != nil {
			return err
		}
		for _, item := range items {
			if item.Key == "" {
				result["skipped"]++
				continue
			}
			if err := a.store.Set(item.Key, item.Value); err != nil {
				return err
			}
			result["updated"]++
		}
	}
	for _, raw := range bundle.Providers {
		var x models.Provider
		var secret struct {
			APIKeyB64 string `json:"apiKeyB64"`
		}
		if err := json.Unmarshal(raw, &x); err != nil {
			result["skipped"]++
			continue
		}
		if err := json.Unmarshal(raw, &secret); err != nil {
			return err
		}
		apiKey, err := settingspkg.DecodeSecret(secret.APIKeyB64)
		if err != nil {
			return err
		}
		x.ProfileID = profile
		x.ProxyID = importID(x.ProxyID)
		x.ID = newID(x.ID, "provider")
		if _, err := a.store.ProfileProviderUpsert(x, apiKey); err != nil {
			return err
		}
		result["updated"]++
	}
	for _, raw := range bundle.Agents {
		var x models.Agent
		if err := json.Unmarshal(raw, &x); err != nil {
			result["skipped"]++
			continue
		}
		x.ProfileID = profile
		for i := range x.SkillGroupIDs {
			x.SkillGroupIDs[i] = importID(x.SkillGroupIDs[i])
		}
		for i := range x.MCPIDs {
			x.MCPIDs[i] = importID(x.MCPIDs[i])
		}
		x.ID = newID(x.ID, "agent")
		if _, err := a.store.AgentUpsert(x); err != nil {
			return err
		}
		result["updated"]++
	}
	for _, raw := range bundle.MCP {
		var x models.MCPServer
		if err := json.Unmarshal(raw, &x); err != nil {
			result["skipped"]++
			continue
		}
		x.ProfileID = profile
		x.ID = newID(x.ID, "mcp")
		if _, err := a.store.ProfileMCPUpsert(x); err != nil {
			return err
		}
		result["updated"]++
	}
	for _, raw := range bundle.Presets {
		var x models.Preset
		if err := json.Unmarshal(raw, &x); err != nil {
			result["skipped"]++
			continue
		}
		x.ProfileID = profile
		if x.AgentID != nil {
			value := importID(*x.AgentID)
			x.AgentID = &value
		}
		x.ID = newID(x.ID, "preset")
		if _, err := a.store.PresetUpsert(x); err != nil {
			return err
		}
		result["updated"]++
	}
	for _, raw := range bundle.Skills {
		var x models.Skill
		if err := json.Unmarshal(raw, &x); err != nil {
			result["skipped"]++
			continue
		}
		x.ProfileID = profile
		x.ID = newID(x.ID, "skill")
		if _, err := a.skills.Upsert(x); err != nil {
			return err
		}
		result["updated"]++
	}
	for _, raw := range bundle.KB {
		var x models.KBDoc
		if err := json.Unmarshal(raw, &x); err != nil {
			result["skipped"]++
			continue
		}
		x.ProfileID = profile
		x.ID = newID(x.ID, "kb")
		if _, err := a.knowledge.Upsert(x); err != nil {
			return err
		}
		result["updated"]++
	}
	for _, raw := range bundle.Notes {
		var x dialog.LongTermNote
		if err := json.Unmarshal(raw, &x); err != nil {
			result["skipped"]++
			continue
		}
		if x.ID == "" {
			x.ID = entityID("note")
		}
		if err := a.memory.SaveNote(profile, "", x); err != nil {
			return err
		}
		result["updated"]++
	}
	for _, raw := range bundle.Topics {
		var x dialog.TopicMemory
		if err := json.Unmarshal(raw, &x); err != nil {
			result["skipped"]++
			continue
		}
		if x.ID == "" {
			x.ID = entityID("topic")
		}
		if err := a.memory.SaveTopic(profile, "", x); err != nil {
			return err
		}
		result["updated"]++
	}
	for _, raw := range bundle.Proxies {
		var x models.Proxy
		var secret struct {
			PasswordB64 string `json:"passwordB64"`
		}
		if err := json.Unmarshal(raw, &x); err != nil {
			result["skipped"]++
			continue
		}
		if err := json.Unmarshal(raw, &secret); err != nil {
			return err
		}
		x.Password, err = settingspkg.DecodeSecret(secret.PasswordB64)
		if err != nil {
			return err
		}
		if x.ID == "" {
			x.ID = entityID("proxy")
		}
		x.ID = importID(x.ID)
		if _, err := a.proxies.Upsert(profile, x); err != nil {
			return err
		}
		result["updated"]++
	}
	return ev.Encode(result)
}

// settingsCleanup removes only data that cannot belong to a current profile.
// It deliberately does not touch existing profile data or user workspace
// directories, which are external to the application's datasource.
func (a *App) settingsCleanup(ev event.Event, _ ws.Meta) error {
	profiles, _, err := a.store.Profiles()
	if err != nil {
		return err
	}
	validProfiles := make(map[string]struct{}, len(profiles))
	validDialogs := make(map[string]map[string]struct{}, len(profiles))
	for _, profile := range profiles {
		validProfiles[profile.ID] = struct{}{}
		conversations, listErr := a.store.ProfileConversations(profile.ID)
		if listErr != nil {
			return listErr
		}
		sessions := make(map[string]struct{}, len(conversations))
		for _, conversation := range conversations {
			sessions[conversation.ID] = struct{}{}
		}
		validDialogs[profile.ID] = sessions
	}
	if err := a.store.CleanupOrphanProfileData(); err != nil {
		return err
	}
	result := map[string]int{}
	if a.dialogs != nil {
		profilesRemoved, dialogsRemoved, cleanupErr := a.dialogs.CleanupOrphans(validDialogs)
		if cleanupErr != nil {
			return fmt.Errorf("clean dialog datasource: %w", cleanupErr)
		}
		result["dialogProfiles"] = profilesRemoved
		result["dialogs"] = dialogsRemoved
	}
	if a.memory != nil {
		removed, cleanupErr := a.memory.CleanupOrphanProfiles(validProfiles)
		if cleanupErr != nil {
			return fmt.Errorf("clean memory datasource: %w", cleanupErr)
		}
		result["memoryProfiles"] = removed
	}
	if a.knowledge != nil {
		removed, cleanupErr := a.knowledge.CleanupOrphanProfiles(validProfiles)
		if cleanupErr != nil {
			return fmt.Errorf("clean knowledge datasource: %w", cleanupErr)
		}
		result["knowledgeProfiles"] = removed
	}
	removed, cleanupErr := a.skills.CleanupOrphanProfiles(validProfiles)
	if cleanupErr != nil {
		return fmt.Errorf("clean skills datasource: %w", cleanupErr)
	}
	result["skillProfiles"] = removed
	logx.Info("settings cleanup completed", "result", result)
	return ev.Encode(result)
}

func (a *App) proxiesList(ev event.Event, _ ws.Meta) error {
	if a.proxies == nil {
		return ev.Encode([]models.Proxy{})
	}
	profile, err := a.activeProfile()
	if err != nil {
		return err
	}
	items, err := a.proxies.List(profile)
	if err != nil {
		return err
	}
	return ev.Encode(items)
}
func (a *App) proxyUpsert(ev event.Event, _ ws.Meta) error {
	var x models.Proxy
	if err := ev.Decode(&x); err != nil {
		return err
	}
	if x.ID == "" {
		x.ID = entityID("proxy")
	}
	profile, err := a.activeProfile()
	if err != nil {
		return err
	}
	item, err := a.proxies.Upsert(profile, x)
	if err != nil {
		return err
	}
	return ev.Encode(item)
}
func (a *App) proxyDelete(ev event.Event, _ ws.Meta) error {
	var q struct {
		ID string `json:"id"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	if q.ID == "" {
		return fmt.Errorf("proxy id is required")
	}
	profile, err := a.activeProfile()
	if err != nil {
		return err
	}
	if err := a.proxies.Delete(profile, q.ID); err != nil {
		return err
	}
	return ev.Encode(map[string]any{"deleted": q.ID})
}
func (a *App) proxyResetPassword(ev event.Event, _ ws.Meta) error {
	var q struct {
		ID string `json:"id"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	if a.proxies == nil {
		return fmt.Errorf("proxy store unavailable")
	}
	profile, err := a.activeProfile()
	if err != nil {
		return err
	}
	if err := a.proxies.ResetPassword(profile, q.ID); err != nil {
		return err
	}
	return ev.Encode(map[string]any{"id": q.ID, "passwordReset": true})
}
func (a *App) proxyTest(ev event.Event, meta ws.Meta) error {
	var q struct {
		ID string `json:"id"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	if a.proxies == nil {
		return fmt.Errorf("proxy store unavailable")
	}
	profile, err := a.activeProfile()
	if err != nil {
		return err
	}
	proxyConfig, err := a.proxies.Secret(profile, q.ID)
	if err != nil {
		return err
	}
	address, err := llm.CheckProxyIP(meta.Context(), &llm.ProxyConfig{Type: proxyConfig.Type, Host: proxyConfig.Host, Port: proxyConfig.Port, Username: proxyConfig.Username, Password: proxyConfig.Password, InsecureSkipVerify: proxyConfig.InsecureSkipVerify})
	if err != nil {
		return err
	}
	return ev.Encode(map[string]any{"id": q.ID, "ip": address})
}
func (a *App) profileList(ev event.Event, _ ws.Meta) error {
	if a.store == nil {
		return ev.Encode(map[string]any{"profiles": []any{}, "activeId": "default"})
	}
	profiles, active, err := a.store.Profiles()
	if err != nil {
		return err
	}
	return ev.Encode(map[string]any{"profiles": profiles, "activeId": active})
}
func (a *App) conversationList(ev event.Event, _ ws.Meta) error {
	var q struct {
		WorkspaceID string `json:"workspaceId"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	if q.WorkspaceID == "" {
		q.WorkspaceID = "default"
	}
	profile, err := a.activeProfile()
	if err != nil {
		return err
	}
	items, err := a.store.Conversations(profile, q.WorkspaceID)
	if err != nil {
		return err
	}
	return ev.Encode(items)
}
func (a *App) conversationGet(ev event.Event, _ ws.Meta) error {
	var q struct {
		ID string `json:"id"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	if q.ID == "" {
		return fmt.Errorf("id is required")
	}
	profile, err := a.activeProfile()
	if err != nil {
		return err
	}
	item, err := a.store.Conversation(profile, q.ID)
	if err != nil {
		return err
	}
	history, err := a.dialogs.History(dialog.SessionKey(profile, item.ID), 0)
	if err != nil {
		return err
	}
	memory, err := a.memory.Get(profile, item.WorkspaceID, item.ID)
	if err != nil {
		return err
	}
	goal, err := a.dialogs.Goal(dialog.SessionKey(profile, item.ID))
	if err != nil {
		return err
	}
	goals, err := a.dialogs.Goals(dialog.SessionKey(profile, item.ID))
	if err != nil {
		return err
	}
	resolvedErrors := make(map[string]bool)
	for _, message := range history {
		if message.Role == "error_resolution" && message.Resolves != "" {
			resolvedErrors[message.Resolves] = true
		}
	}
	messages := make([]map[string]any, 0, len(history))
	for index, message := range history {
		if message.Role == "error_resolution" || (message.Error && resolvedErrors[message.ID]) {
			continue
		}
		role := "agent"
		if message.Role == "user" {
			role = "user"
		}
		if message.Role == "tool_call" || message.Role == "tool_result" {
			toolResult := message.Role == "tool_result"
			messages = append(messages, map[string]any{"id": message.ID, "role": "tool", "time": message.Timestamp.Format(time.RFC3339), "toolName": message.Tool, "toolArguments": message.Arguments, "toolResult": message.Content, "toolRunning": !toolResult, "model": message.Model, "provider": message.Provider, "parts": []map[string]string{}})
			continue
		}
		messageID := message.ID
		if messageID == "" { // compatibility for JSONL files created before IDs
			messageID = fmt.Sprintf("%s-%d", item.ID, index)
		}
		messages = append(messages, map[string]any{"id": messageID, "role": role, "time": message.Timestamp.Format(time.RFC3339), "compact": message.Compact, "contextSize": message.ContextSize, "tokens": message.Tokens, "model": message.Model, "provider": message.Provider, "retryableError": message.Error, "parts": []map[string]string{{"type": "text", "content": message.Content}}})
	}
	return ev.Encode(map[string]any{"id": item.ID, "workspaceId": item.WorkspaceID, "agentId": item.AgentID, "title": item.Title, "updatedAt": item.UpdatedAt, "activeModel": item.ActiveModel, "messages": messages, "memory": memory, "goal": goal, "goals": goals})
}

func (a *App) conversationMemory(ev event.Event, meta ws.Meta) error {
	var q struct {
		ID, WorkspaceID string
		Title           string
		Summary         string
		Topics          []string
		Save            bool
		Model           string
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	if q.ID == "" {
		return fmt.Errorf("id is required")
	}
	profile, err := a.activeProfile()
	if err != nil {
		return err
	}
	item, err := a.store.Conversation(profile, q.ID)
	if err != nil {
		return err
	}
	if q.WorkspaceID == "" {
		q.WorkspaceID = item.WorkspaceID
	}
	if q.Save {
		if err := a.memory.Save(profile, q.WorkspaceID, q.ID, dialog.Memory{Title: q.Title, Summary: q.Summary, Topics: q.Topics}); err != nil {
			return err
		}
		memory, err := a.memory.Get(profile, q.WorkspaceID, q.ID)
		if err != nil {
			return err
		}
		return ev.Encode(memory)
	}
	if q.Model == "" {
		for _, agent := range mustAgents(a.store, profile) {
			if agent.ID == item.AgentID {
				q.Model = agent.MemoryModel
				if q.Model == "" {
					q.Model = item.ActiveModel
					if q.Model == "" && len(agent.MainModels) > 0 {
						q.Model = agent.MainModels[0]
					}
				}
				break
			}
		}
	}
	if q.Model == "" {
		memory, err := a.memory.Get(profile, q.WorkspaceID, q.ID)
		if err != nil {
			return err
		}
		return ev.Encode(memory)
	}
	history, err := a.dialogs.History(dialog.SessionKey(profile, item.ID), 0)
	if err != nil {
		return err
	}
	messages := make([]llm.Message, 0, len(history)+1)
	for _, m := range history {
		if m.Role == "user" || m.Role == "assistant" {
			messages = append(messages, llm.Message{Role: m.Role, Content: m.Content})
		}
	}
	messages = append(messages, llm.Message{Role: "user", Content: prompts.MemoryRequest})
	providerConfig, apiKey, err := a.store.DefaultProfileProvider(profile)
	if at := strings.LastIndex(q.Model, "@"); at > 0 {
		providerConfig, apiKey, err = a.store.ProfileProviderSecret(profile, q.Model[at+1:])
		q.Model = q.Model[:at]
	}
	if err != nil {
		return err
	}
	response, err := llm.New(providerConfig.Kind, providerConfig.BaseURL, apiKey, q.Model, a.providerProxy(profile, providerConfig.ProxyID), llm.WithRPM(providerConfig.ID, providerConfig.RPM)).ChatCompletion(context.Background(), prompts.MemorySystem, messages, false)
	if err != nil {
		return err
	}
	generated := parseGeneratedMemory(response.Content)
	memory := generated.Memory
	if err := a.memory.Save(profile, q.WorkspaceID, q.ID, memory); err != nil {
		return err
	}
	if err := a.saveExtractedMemories(profile, q.WorkspaceID, generated); err != nil {
		return err
	}
	return ev.Encode(memory)
}

func (a *App) memoryNotesList(ev event.Event, _ ws.Meta) error {
	var q struct {
		WorkspaceID string `json:"workspaceId"`
		Cursor      string `json:"cursor"`
		Limit       int    `json:"limit"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	profile, err := a.activeProfile()
	if err != nil {
		return err
	}
	page, err := a.memory.NotesPage(profile, q.Cursor, q.Limit)
	if err != nil {
		return err
	}
	return ev.Encode(page)
}

func (a *App) memoryNoteSave(ev event.Event, _ ws.Meta) error {
	var q struct {
		WorkspaceID string `json:"workspaceId"`
		dialog.LongTermNote
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	profile, err := a.activeProfile()
	if err != nil {
		return err
	}
	if err = a.memory.SaveNote(profile, q.WorkspaceID, q.LongTermNote); err != nil {
		return err
	}
	return ev.Encode(q.LongTermNote)
}

func (a *App) memoryNoteDelete(ev event.Event, _ ws.Meta) error {
	var q struct {
		WorkspaceID string `json:"workspaceId"`
		ID          string `json:"id"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	profile, err := a.activeProfile()
	if err != nil {
		return err
	}
	if err := a.memory.DeleteNote(profile, q.WorkspaceID, q.ID); err != nil {
		return err
	}
	return ev.Encode(map[string]bool{"ok": true})
}

func (a *App) memoryTopicsList(ev event.Event, _ ws.Meta) error {
	var q struct {
		WorkspaceID string `json:"workspaceId"`
		Cursor      string `json:"cursor"`
		Limit       int    `json:"limit"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	profile, err := a.activeProfile()
	if err != nil {
		return err
	}
	page, err := a.memory.TopicsPage(profile, q.Cursor, q.Limit)
	if err != nil {
		return err
	}
	return ev.Encode(page)
}

func (a *App) memoryTopicSave(ev event.Event, _ ws.Meta) error {
	var q struct {
		WorkspaceID string `json:"workspaceId"`
		dialog.TopicMemory
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	profile, err := a.activeProfile()
	if err != nil {
		return err
	}
	if err = a.memory.SaveTopic(profile, q.WorkspaceID, q.TopicMemory); err != nil {
		return err
	}
	return ev.Encode(q.TopicMemory)
}

func (a *App) memoryTopicDelete(ev event.Event, _ ws.Meta) error {
	var q struct {
		WorkspaceID string `json:"workspaceId"`
		ID          string `json:"id"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	profile, err := a.activeProfile()
	if err != nil {
		return err
	}
	if err := a.memory.DeleteTopic(profile, q.WorkspaceID, q.ID); err != nil {
		return err
	}
	return ev.Encode(map[string]bool{"ok": true})
}

func (a *App) memoryReindex(ev event.Event, _ ws.Meta) error {
	var q struct {
		ProfileID string `json:"profileId"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	if q.ProfileID == "" {
		profile, err := a.activeProfile()
		if err != nil {
			return err
		}
		q.ProfileID = profile
	}
	notes, topics, err := a.memory.Reindex(q.ProfileID)
	if err != nil {
		return err
	}
	return ev.Encode(map[string]int{"notes": notes, "topics": topics})
}

func mustAgents(store *configstore.Store, profile string) []models.Agent {
	agents, _ := store.Agents(profile)
	return agents
}

type generatedMemory struct {
	dialog.Memory
	Notes         []dialog.LongTermNote `json:"notes"`
	TopicMemories []dialog.TopicMemory  `json:"topicMemories"`
}

func parseGeneratedMemory(content string) generatedMemory {
	clean := strings.TrimSpace(content)
	clean = strings.TrimPrefix(clean, "```json")
	clean = strings.TrimPrefix(clean, "```")
	clean = strings.TrimSuffix(strings.TrimSpace(clean), "```")
	// Decode every optional field separately. A provider may produce a valid
	// summary but violate one optional array schema (for example objects in
	// `topics`). That must not turn the entire JSON response into raw text in
	// the stored session memory.
	var raw struct {
		Title         string          `json:"title"`
		Summary       string          `json:"summary"`
		Topics        json.RawMessage `json:"topics"`
		Notes         json.RawMessage `json:"notes"`
		TopicMemories json.RawMessage `json:"topicMemories"`
	}
	if err := json.Unmarshal([]byte(clean), &raw); err == nil && strings.TrimSpace(raw.Summary) != "" {
		memory := generatedMemory{Memory: dialog.Memory{Title: raw.Title, Summary: raw.Summary, Topics: []string{}}, Notes: []dialog.LongTermNote{}, TopicMemories: []dialog.TopicMemory{}}
		if memory.Memory.Title == "" {
			memory.Memory.Title = memory.Memory.Summary
		}
		var topics []string
		if json.Unmarshal(raw.Topics, &topics) == nil {
			memory.Memory.Topics = topics
		}
		var notes []dialog.LongTermNote
		if json.Unmarshal(raw.Notes, &notes) == nil {
			memory.Notes = notes
		}
		var topicMemories []dialog.TopicMemory
		if json.Unmarshal(raw.TopicMemories, &topicMemories) == nil {
			memory.TopicMemories = topicMemories
		}
		return memory
	}
	firstLine := strings.TrimSpace(strings.SplitN(content, "\n", 2)[0])
	return generatedMemory{Memory: dialog.Memory{Title: firstLine, Summary: content, Topics: []string{}}, Notes: []dialog.LongTermNote{}, TopicMemories: []dialog.TopicMemory{}}
}

func extractedMemoryID(prefix, title string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(title))))
	return fmt.Sprintf("%s-%x", prefix, sum[:8])
}

func (a *App) saveExtractedMemories(profileID, workspaceID string, generated generatedMemory) error {
	for index, note := range generated.Notes {
		if index >= 3 || strings.TrimSpace(note.Title) == "" || strings.TrimSpace(note.Content) == "" {
			continue
		}
		note.ID = extractedMemoryID("note", note.Title)
		if err := a.memory.SaveNote(profileID, "", note); err != nil {
			return err
		}
	}
	for index, topic := range generated.TopicMemories {
		if index >= 3 || strings.TrimSpace(topic.Title) == "" || strings.TrimSpace(topic.Summary) == "" {
			continue
		}
		topic.ID = extractedMemoryID("topic", topic.Title)
		if err := a.memory.SaveTopic(profileID, workspaceID, topic); err != nil {
			return err
		}
	}
	return nil
}
func (a *App) conversationAppend(ev event.Event, _ ws.Meta) error {
	var q struct {
		ConvID  string `json:"convId"`
		Message struct {
			Role  string `json:"role"`
			Parts []struct {
				Content string `json:"content"`
			} `json:"parts"`
		} `json:"message"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	if q.ConvID == "" || len(q.Message.Parts) == 0 {
		return fmt.Errorf("convId and message are required")
	}
	profile, err := a.activeProfile()
	if err != nil {
		return err
	}
	item, err := a.store.Conversation(profile, q.ConvID)
	if err != nil {
		return err
	}
	content := q.Message.Parts[0].Content
	if err = a.dialogs.Append(dialog.SessionKey(profile, item.ID), dialog.Message{Role: q.Message.Role, Content: content}); err != nil {
		return err
	}
	if q.Message.Role == "user" && item.Title == "New conversation" {
		item.Title = truncateTitle(content)
	}
	item, err = a.store.ConversationUpsert(item)
	if err != nil {
		return err
	}
	return ev.Encode(map[string]string{"title": item.Title, "updatedAt": item.UpdatedAt})
}
func (a *App) conversationCompact(ev event.Event, meta ws.Meta) error {
	var q struct {
		ConvID string `json:"convId"`
		Model  string `json:"model"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	profile, err := a.activeProfile()
	if err != nil {
		return err
	}
	item, err := a.store.Conversation(profile, q.ConvID)
	if err != nil {
		return err
	}
	history, err := a.dialogs.History(dialog.SessionKey(profile, item.ID), 0)
	if err != nil {
		return err
	}
	var conversationAgent models.Agent
	for _, candidate := range mustAgents(a.store, profile) {
		if candidate.ID == item.AgentID {
			conversationAgent = candidate
			break
		}
	}
	if q.Model == "" {
		if conversationAgent.ID != "" {
			candidate := conversationAgent
			if candidate.ID == item.AgentID {
				q.Model = item.ActiveModel
				if q.Model == "" && len(candidate.MainModels) > 0 {
					q.Model = candidate.MainModels[0]
				}
			}
		}
		if q.Model == "" {
			return fmt.Errorf("compaction model is required")
		}
	}
	providerConfig, apiKey, err := a.store.DefaultProfileProvider(profile)
	if at := strings.LastIndex(q.Model, "@"); at > 0 && at < len(q.Model)-1 {
		providerConfig, apiKey, err = a.store.ProfileProviderSecret(profile, q.Model[at+1:])
		q.Model = q.Model[:at]
	}
	if err != nil {
		return fmt.Errorf("compaction provider is not configured: %w", err)
	}
	messages := make([]llm.Message, 0, len(history)+1)
	for _, message := range history {
		role := message.Role
		if role == "system" && message.Compact {
			// A prior compaction is the durable boundary context. It must be
			// available when the dialog is compacted again, otherwise each new
			// compaction silently loses everything before the last boundary.
			messages = append(messages, llm.Message{Role: role, Content: message.Content})
			continue
		}
		if role != "user" && role != "assistant" && role != "tool" {
			continue
		}
		messages = append(messages, llm.Message{Role: role, Content: message.Content})
	}
	messages = append(messages, llm.Message{Role: "user", Content: prompts.CompactionRequestFor(conversationAgent.CompactionLevel)})
	var provider llm.Provider = llm.New(providerConfig.Kind, providerConfig.BaseURL, apiKey, q.Model, a.providerProxy(profile, providerConfig.ProxyID), llm.WithRPM(providerConfig.ID, providerConfig.RPM))
	response, err := provider.ChatCompletion(meta.Context(), prompts.CompactionSystemFor(conversationAgent.CompactionLevel), messages, false)
	if err != nil {
		return err
	}
	if response.Content == "" {
		return fmt.Errorf("compaction provider returned an empty summary")
	}
	generated := parseGeneratedMemory(response.Content)
	memory := generated.Memory
	if err = a.dialogs.Append(dialog.SessionKey(profile, item.ID), dialog.Message{Role: "system", Content: memory.Summary, Model: q.Model, Provider: providerConfig.Name, ContextSize: response.Usage.CompletionTokens, Tokens: response.Usage.PromptTokens + response.Usage.CompletionTokens, Compact: true}); err != nil {
		return err
	}
	before := 0
	for _, message := range history {
		before += message.ContextSize
	}
	tokens := response.Usage.TotalTokens
	if tokens == 0 {
		tokens = response.Usage.PromptTokens + response.Usage.CompletionTokens
	}
	if err := a.memory.Save(profile, item.WorkspaceID, item.ID, memory); err != nil {
		return err
	}
	if err := a.saveExtractedMemories(profile, item.WorkspaceID, generated); err != nil {
		return err
	}
	return ev.Encode(map[string]any{"before": before, "after": response.Usage.CompletionTokens, "title": memory.Title, "summary": memory.Summary, "topics": memory.Topics, "tokens": tokens})
}
func (a *App) conversationUpsert(ev event.Event, _ ws.Meta) error {
	var q models.Conversation
	if err := ev.Decode(&q); err != nil {
		return err
	}
	if q.ProfileID == "" {
		id, err := a.activeProfile()
		if err != nil {
			return err
		}
		q.ProfileID = id
	}
	if q.WorkspaceID == "" {
		q.WorkspaceID = "default"
	}
	if q.ID == "" {
		q.ID = entityID("conversation")
	}
	conversation, err := a.store.ConversationUpsert(q)
	if err != nil {
		return err
	}
	return ev.Encode(conversation)
}

func (a *App) conversationSetModel(ev event.Event, _ ws.Meta) error {
	var q struct {
		ID    string `json:"id"`
		Mode  string `json:"mode"`
		Model string `json:"model"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	if q.ID == "" || (q.Mode != "main" && q.Mode != "model" && q.Mode != "fallback") {
		return fmt.Errorf("id and model mode are required")
	}
	profileID, err := a.activeProfile()
	if err != nil {
		return err
	}
	conversation, err := a.store.Conversation(profileID, q.ID)
	if err != nil {
		return err
	}
	// Deprecated: the fallback mode is retained only to return a clear
	// compatibility error to clients of older frontend versions.
	if q.Mode == "fallback" {
		return fmt.Errorf("fallback models are no longer supported")
	}
	if q.Model != "" {
		for _, candidate := range mustAgents(a.store, profileID) {
			if candidate.ID == conversation.AgentID && !containsString(candidate.MainModels, q.Model) {
				return fmt.Errorf("model %q is not configured as an agent main model", q.Model)
			}
		}
		conversation.ActiveModel = q.Model
	} else {
		conversation.ActiveModel = ""
	}
	if err := a.store.ConversationSetActiveModel(profileID, q.ID, conversation.ActiveModel); err != nil {
		return err
	}
	return ev.Encode(conversation)
}

func (a *App) conversationRunStatus(ev event.Event, _ ws.Meta) error {
	var q struct {
		ID string `json:"id"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	if q.ID == "" {
		return fmt.Errorf("conversation id is required")
	}
	a.chatMu.RLock()
	_, running := a.chatJobs[q.ID]
	a.chatMu.RUnlock()
	a.requestMu.Lock()
	var request map[string]any
	for _, pending := range a.requests {
		if pending.dialogID == q.ID {
			request = pending.request
			break
		}
	}
	a.requestMu.Unlock()
	return ev.Encode(map[string]any{"running": running, "request": request})
}

func (a *App) conversationDelete(ev event.Event, _ ws.Meta) error {
	var q struct {
		ID string `json:"id"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	profile, err := a.activeProfile()
	if err != nil {
		return err
	}
	conversation, err := a.store.Conversation(profile, q.ID)
	if err != nil {
		return err
	}
	if err := a.dialogs.Delete(dialog.SessionKey(profile, conversation.ID)); err != nil {
		return err
	}
	if err := a.memory.Delete(profile, conversation.WorkspaceID, conversation.ID); err != nil {
		return err
	}
	if err := a.dialogs.DeleteGoals(dialog.SessionKey(profile, conversation.ID)); err != nil {
		return err
	}
	if err := a.store.ConversationDelete(profile, q.ID); err != nil {
		return err
	}
	return ev.Encode(map[string]bool{"ok": true})
}
func (a *App) conversationClear(ev event.Event, _ ws.Meta) error {
	var q struct {
		ID string `json:"id"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	if q.ID == "" {
		return fmt.Errorf("id is required")
	}
	profile, err := a.activeProfile()
	if err != nil {
		return err
	}
	conversation, err := a.store.Conversation(profile, q.ID)
	if err != nil {
		return err
	}
	if err := a.dialogs.Delete(dialog.SessionKey(profile, conversation.ID)); err != nil {
		return err
	}
	if err := a.memory.Delete(profile, conversation.WorkspaceID, conversation.ID); err != nil {
		return err
	}
	if err := a.dialogs.DeleteGoals(dialog.SessionKey(profile, conversation.ID)); err != nil {
		return err
	}
	return ev.Encode(map[string]bool{"ok": true})
}

func (a *App) conversationDeleteMessage(ev event.Event, _ ws.Meta) error {
	var q struct {
		ID        string `json:"id"`
		MessageID string `json:"messageId"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	if q.ID == "" || q.MessageID == "" {
		return fmt.Errorf("conversation id and message id are required")
	}
	profile, err := a.activeProfile()
	if err != nil {
		return err
	}
	conversation, err := a.store.Conversation(profile, q.ID)
	if err != nil {
		return err
	}
	if err := a.dialogs.DeleteMessage(dialog.SessionKey(profile, conversation.ID), q.MessageID); err != nil {
		return err
	}
	return ev.Encode(map[string]bool{"ok": true})
}
func (a *App) profileCreate(ev event.Event, _ ws.Meta) error {
	var q models.Profile
	if err := ev.Decode(&q); err != nil {
		return err
	}
	if q.ID == "" {
		q.ID = entityID("profile")
	}
	created, err := a.store.ProfileCreate(q)
	if err != nil {
		return err
	}
	if err := a.store.ProfileSetActive(created.ID); err != nil {
		return err
	}
	return ev.Encode(created)
}
func (a *App) profileUpdate(ev event.Event, _ ws.Meta) error {
	var q struct {
		ID    string         `json:"id"`
		Patch models.Profile `json:"patch"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	q.Patch.ID = q.ID
	if err := a.store.ProfileUpdate(q.Patch); err != nil {
		return err
	}
	return ev.Encode(q.Patch)
}
func (a *App) profileSetActive(ev event.Event, _ ws.Meta) error {
	var q struct {
		ID string `json:"id"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	if err := a.store.ProfileSetActive(q.ID); err != nil {
		return err
	}
	return ev.Encode(map[string]bool{"ok": true})
}
func (a *App) profileDelete(ev event.Event, _ ws.Meta) error {
	var q struct {
		ID string `json:"id"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	profiles, _, err := a.store.Profiles()
	if err != nil {
		return err
	}
	if len(profiles) <= 1 {
		return fmt.Errorf("cannot delete last profile")
	}
	if q.ID == "" {
		return fmt.Errorf("profile id is required")
	}
	found := false
	for _, profile := range profiles {
		if profile.ID == q.ID {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("profile %q not found", q.ID)
	}
	conversations, err := a.store.ProfileConversations(q.ID)
	if err != nil {
		return err
	}
	workspaces, err := a.store.Workspaces(q.ID)
	if err != nil {
		return err
	}
	a.stopProfileChats(conversations)
	// Workspaces only register folders opened by the user. Close their roots but
	// never remove the user-owned folders themselves.
	if a.workspaces != nil {
		for _, item := range workspaces {
			if closeErr := a.workspaces.Close(item.ID); closeErr != nil {
				logx.Error("failed to close profile workspace", "profile_id", q.ID, "workspace_id", item.ID, "err", closeErr)
			}
		}
	}
	if a.dialogs != nil {
		if err := a.dialogs.DeleteProfile(q.ID); err != nil {
			return fmt.Errorf("delete profile dialog history: %w", err)
		}
	}
	if a.memory != nil {
		if err := a.memory.DeleteProfile(q.ID); err != nil {
			return fmt.Errorf("delete profile memory: %w", err)
		}
	}
	if a.knowledge != nil {
		if err := a.knowledge.DeleteProfile(q.ID); err != nil {
			return fmt.Errorf("delete profile knowledge: %w", err)
		}
	}
	if err := a.skills.DeleteProfile(q.ID); err != nil {
		return fmt.Errorf("delete profile skills: %w", err)
	}
	if err := a.store.ProfileDelete(q.ID); err != nil {
		return err
	}
	return ev.Encode(map[string]bool{"ok": true})
}

func (a *App) stopProfileChats(conversations []models.Conversation) {
	ids := make(map[string]struct{}, len(conversations))
	for _, conversation := range conversations {
		ids[conversation.ID] = struct{}{}
	}
	a.chatMu.RLock()
	jobs := make([]*chatJob, 0, len(ids))
	for id := range ids {
		if job := a.chatJobs[id]; job != nil {
			jobs = append(jobs, job)
		}
	}
	a.chatMu.RUnlock()
	for _, job := range jobs {
		job.cancel()
	}
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for _, job := range jobs {
		select {
		case <-job.done:
		case <-deadline.C:
			logx.Error("timed out waiting for profile chat to stop")
			return
		}
	}
}
func (a *App) activeProfile() (string, error) {
	if a.store == nil {
		return "", fmt.Errorf("configuration store unavailable")
	}
	_, id, err := a.store.Profiles()
	return id, err
}
func (a *App) agentsList(ev event.Event, _ ws.Meta) error {
	var q struct {
		ProfileID string `json:"profileId"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	if q.ProfileID == "" {
		_, id, err := a.store.Profiles()
		if err != nil {
			return err
		}
		q.ProfileID = id
	}
	items, err := a.store.Agents(q.ProfileID)
	if err != nil {
		return err
	}
	return ev.Encode(items)
}
func (a *App) agentsUpsert(ev event.Event, _ ws.Meta) error {
	var request struct {
		ID    string          `json:"id"`
		Patch json.RawMessage `json:"patch"`
	}
	if err := ev.Decode(&request); err != nil {
		return err
	}
	var q models.Agent
	if request.ID != "" {
		profileID, err := a.activeProfile()
		if err != nil {
			return err
		}
		items, err := a.store.Agents(profileID)
		if err != nil {
			return err
		}
		for _, item := range items {
			if item.ID == request.ID {
				q = item
				break
			}
		}
		if q.ID == "" {
			return fmt.Errorf("agent %q not found", request.ID)
		}
		if err = applyPatch(&q, request.Patch); err != nil {
			return err
		}
	} else if err := ev.Decode(&q); err != nil {
		return err
	}
	if q.ProfileID == "" {
		_, id, err := a.store.Profiles()
		if err != nil {
			return err
		}
		q.ProfileID = id
	}
	if q.ID == "" {
		q.ID = entityID("agent")
	}
	item, err := a.store.AgentUpsert(q)
	if err != nil {
		return err
	}
	return ev.Encode(item)
}
func (a *App) agentsDelete(ev event.Event, _ ws.Meta) error {
	var q struct {
		ID        string `json:"id"`
		ProfileID string `json:"profileId"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	if q.ProfileID == "" {
		_, id, err := a.store.Profiles()
		if err != nil {
			return err
		}
		q.ProfileID = id
	}
	if q.ID == "" {
		return fmt.Errorf("id is required")
	}
	if err := a.store.AgentDelete(q.ProfileID, q.ID); err != nil {
		return err
	}
	return ev.Encode(map[string]bool{"ok": true})
}
func (a *App) presetsList(ev event.Event, _ ws.Meta) error {
	var q struct {
		ProfileID string `json:"profileId"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	if q.ProfileID == "" {
		id, err := a.activeProfile()
		if err != nil {
			return err
		}
		q.ProfileID = id
	}
	items, err := a.store.Presets(q.ProfileID)
	if err != nil {
		return err
	}
	return ev.Encode(items)
}
func (a *App) presetsUpsert(ev event.Event, _ ws.Meta) error {
	var request struct {
		ID    string          `json:"id"`
		Patch json.RawMessage `json:"patch"`
	}
	if err := ev.Decode(&request); err != nil {
		return err
	}
	var q models.Preset
	if request.ID != "" {
		profileID, err := a.activeProfile()
		if err != nil {
			return err
		}
		items, err := a.store.Presets(profileID)
		if err != nil {
			return err
		}
		for _, item := range items {
			if item.ID == request.ID {
				q = item
				break
			}
		}
		if q.ID == "" {
			return fmt.Errorf("preset %q not found", request.ID)
		}
		if err = applyPatch(&q, request.Patch); err != nil {
			return err
		}
	} else if err := ev.Decode(&q); err != nil {
		return err
	}
	if q.ProfileID == "" {
		id, err := a.activeProfile()
		if err != nil {
			return err
		}
		q.ProfileID = id
	}
	if q.ID == "" {
		q.ID = entityID("preset")
	}
	item, err := a.store.PresetUpsert(q)
	if err != nil {
		return err
	}
	return ev.Encode(item)
}
func (a *App) presetsDelete(ev event.Event, _ ws.Meta) error {
	var q struct {
		ID        string `json:"id"`
		ProfileID string `json:"profileId"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	if q.ProfileID == "" {
		id, err := a.activeProfile()
		if err != nil {
			return err
		}
		q.ProfileID = id
	}
	if err := a.store.PresetDelete(q.ProfileID, q.ID); err != nil {
		return err
	}
	return ev.Encode(map[string]bool{"ok": true})
}
func (a *App) managedSkillsList(ev event.Event, _ ws.Meta) error {
	var q struct {
		ProfileID string `json:"profileId"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	if q.ProfileID == "" {
		id, err := a.activeProfile()
		if err != nil {
			return err
		}
		q.ProfileID = id
	}
	items, err := a.skills.List(q.ProfileID)
	if err != nil {
		return err
	}
	return ev.Encode(items)
}

func (a *App) skillsOpenFolder(ev event.Event, _ ws.Meta) error {
	var q struct {
		ID string `json:"id"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	if q.ID == "" {
		return fmt.Errorf("skill id is required")
	}
	profile, err := a.activeProfile()
	if err != nil {
		return err
	}
	if err := a.skills.OpenFolder(profile, q.ID); err != nil {
		return err
	}
	return ev.Encode(map[string]any{"ok": true})
}
func (a *App) managedSkillsUpsert(ev event.Event, _ ws.Meta) error {
	var payload map[string]json.RawMessage
	if err := ev.Decode(&payload); err != nil {
		return err
	}
	var request struct {
		ID            string          `json:"id"`
		Patch         json.RawMessage `json:"patch"`
		Dependencies  []string        `json:"dependencies"`
		DependencyIDs []string        `json:"dependencyIds"`
	}
	_ = json.Unmarshal(payload["id"], &request.ID)
	_ = json.Unmarshal(payload["patch"], &request.Patch)
	_ = json.Unmarshal(payload["dependencies"], &request.Dependencies)
	_ = json.Unmarshal(payload["dependencyIds"], &request.DependencyIDs)
	var q models.Skill
	dependencies := request.Dependencies
	if len(dependencies) == 0 {
		dependencies = request.DependencyIDs
	}
	if request.ID != "" {
		profileID, err := a.activeProfile()
		if err != nil {
			return err
		}
		items, err := a.skills.List(profileID)
		if err != nil {
			return err
		}
		for _, item := range items {
			if item.ID == request.ID {
				q = item
				break
			}
		}
		if q.ID == "" {
			return fmt.Errorf("skill %q not found", request.ID)
		}
		if err = applyPatch(&q, request.Patch); err != nil {
			return err
		}
	} else {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(encoded, &q); err != nil {
			return err
		}
	}
	if q.ProfileID == "" {
		id, err := a.activeProfile()
		if err != nil {
			return err
		}
		q.ProfileID = id
	}
	if q.ID == "" {
		q.ID = entityID("skill")
	}
	item, err := a.skills.Upsert(q)
	if err != nil {
		return err
	}
	return ev.Encode(item)
}
func (a *App) managedSkillsDelete(ev event.Event, _ ws.Meta) error {
	var q struct {
		ID        string `json:"id"`
		ProfileID string `json:"profileId"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	if q.ProfileID == "" {
		id, err := a.activeProfile()
		if err != nil {
			return err
		}
		q.ProfileID = id
	}
	if err := a.skills.Delete(q.ProfileID, q.ID); err != nil {
		return err
	}
	return ev.Encode(map[string]bool{"ok": true})
}
func (a *App) skillsDiscover(ev event.Event, meta ws.Meta) error {
	var q struct {
		Source string `json:"source"`
		Ref    string `json:"ref"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	if q.Ref == "" {
		return fmt.Errorf("ref is required")
	}
	if q.Source == "link" {
		if isArchiveURL(q.Ref) {
			req, err := http.NewRequestWithContext(meta.Context(), http.MethodGet, q.Ref, nil)
			if err != nil {
				return err
			}
			response, err := http.DefaultClient.Do(req)
			if err != nil {
				return err
			}
			defer response.Body.Close()
			if response.StatusCode < 200 || response.StatusCode >= 300 {
				return fmt.Errorf("skill archive returned HTTP %d", response.StatusCode)
			}
			archive, err := utils.ReadAllResponse(response.Body)
			if err != nil {
				return err
			}
			archiveKind := ".zip"
			if strings.HasSuffix(strings.ToLower(q.Ref), ".tar.gz") {
				archiveKind = ".tar.gz"
			} else if strings.HasSuffix(strings.ToLower(q.Ref), ".tar") {
				archiveKind = ".tar"
			}
			root, err := extractSkillArchive(archive, archiveKind)
			if err != nil {
				return err
			}
			defer os.RemoveAll(root)
			return discoverSkillDirectory(ev, root, "")
		}
		// A repository URL is not a skill document. Clone it to a temporary
		// directory and discover every Markdown skill recursively.
		if isGitRepositoryURL(q.Ref) {
			tmp, err := os.MkdirTemp("", "arwos-skills-")
			if err != nil {
				return err
			}
			defer os.RemoveAll(tmp)
			clone := exec.CommandContext(meta.Context(), "git", "clone", "--depth", "1", q.Ref, tmp)
			if output, err := clone.CombinedOutput(); err != nil {
				return fmt.Errorf("clone skill repository: %w: %s", err, strings.TrimSpace(string(output)))
			}
			return discoverSkillDirectory(ev, tmp, q.Ref)
		}
		requestURL := rawGitFileURL(q.Ref)
		req, err := http.NewRequestWithContext(meta.Context(), http.MethodGet, requestURL, nil)
		if err != nil {
			return err
		}
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		defer response.Body.Close()
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return fmt.Errorf("skill URL returned HTTP %d", response.StatusCode)
		}
		body, err := utils.ReadAllResponse(response.Body)
		if err != nil {
			return err
		}
		if !isSkillDocument(body) {
			return fmt.Errorf("skill document must contain name and description frontmatter")
		}
		return ev.Encode([]map[string]any{discoveredSkill(path.Base(requestURL), string(body), q.Ref)})
	}
	if q.Source != "directory" {
		return fmt.Errorf("unsupported skill source %q", q.Source)
	}
	root := filepath.Clean(q.Ref)
	out := make([]map[string]any, 0)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if !isSkillFile(entry.Name()) {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !isSkillDocument(body) {
			return nil
		}
		out = append(out, discoveredSkill(filepath.Base(path), string(body), path))
		return nil
	})
	if err != nil {
		return err
	}
	if len(out) == 0 {
		return fmt.Errorf("no skill documents with name and description frontmatter were found")
	}
	return ev.Encode(out)
}

func discoverSkillDirectory(ev event.Event, root, sourceRef string) error {
	out := make([]map[string]any, 0)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !isSkillFile(entry.Name()) {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !isSkillDocument(body) {
			return nil
		}
		item := discoveredSkill(entry.Name(), string(body), path)
		if sourceRef != "" {
			if relative, err := filepath.Rel(root, path); err == nil {
				item["sourceRef"] = sourceRef
				item["sourcePath"] = filepath.ToSlash(relative)
			}
		}
		out = append(out, item)
		return nil
	})
	if err != nil {
		return err
	}
	if len(out) == 0 {
		return fmt.Errorf("no skill documents with name and description frontmatter were found")
	}
	return ev.Encode(out)
}
func (a *App) skillsImportMany(ev event.Event, _ ws.Meta) error {
	var q struct {
		Items []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Icon        string `json:"icon"`
			Accent      string `json:"accent"`
			Content     string `json:"content"`
			Origin      string `json:"origin"`
			SourceRef   string `json:"sourceRef"`
			SourcePath  string `json:"sourcePath"`
		} `json:"items"`
		Source string `json:"source"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	profileID, err := a.activeProfile()
	if err != nil {
		return err
	}
	created := make([]models.Skill, 0, len(q.Items))
	var gitRoot string
	if q.Source == "link" && len(q.Items) > 0 && q.Items[0].SourceRef != "" {
		gitRoot, err = os.MkdirTemp("", "arwos-skills-import-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(gitRoot)
		clone := exec.CommandContext(context.Background(), "git", "clone", "--depth", "1", q.Items[0].SourceRef, gitRoot)
		if output, cloneErr := clone.CombinedOutput(); cloneErr != nil {
			return fmt.Errorf("clone skill repository: %w: %s", cloneErr, strings.TrimSpace(string(output)))
		}
	}
	for _, item := range q.Items {
		if item.Name == "" || item.Content == "" {
			return fmt.Errorf("imported skill must have name and content")
		}
		out, err := a.skills.Upsert(models.Skill{ID: entityID("skill"), ProfileID: profileID, Name: item.Name, Description: item.Description, Content: item.Content, Icon: item.Icon, Accent: item.Accent, Enabled: true, Source: q.Source, SourceRef: item.Origin})
		if err != nil {
			return err
		}
		if q.Source == "directory" {
			if err := a.skills.AttachDirectory(profileID, out.ID, filepath.Dir(item.Origin)); err != nil {
				return err
			}
		} else if gitRoot != "" {
			if err := a.skills.AttachDirectory(profileID, out.ID, filepath.Dir(filepath.Join(gitRoot, filepath.FromSlash(item.SourcePath)))); err != nil {
				return err
			}
		}
		created = append(created, out)
	}
	return ev.Encode(created)
}

func (a *App) skillGroupsList(ev event.Event, _ ws.Meta) error {
	profile, err := a.activeProfile()
	if err != nil {
		return err
	}
	groups, err := a.skills.Groups(profile)
	if err != nil {
		return err
	}
	return ev.Encode(groups)
}

func (a *App) skillGroupSave(ev event.Event, _ ws.Meta) error {
	var group models.SkillGroup
	if err := ev.Decode(&group); err != nil {
		return err
	}
	profile, err := a.activeProfile()
	if err != nil {
		return err
	}
	if group.ID == "" {
		group.ID = entityID("skill-group")
	}
	group.ProfileID = profile
	groups, err := a.skills.Groups(profile)
	if err != nil {
		return err
	}
	found := false
	for i := range groups {
		if groups[i].ID == group.ID {
			groups[i] = group
			found = true
			break
		}
	}
	if !found {
		groups = append(groups, group)
	}
	if err := a.skills.SaveGroups(profile, groups); err != nil {
		return err
	}
	return ev.Encode(group)
}

func (a *App) skillGroupDelete(ev event.Event, _ ws.Meta) error {
	var q struct {
		ID string `json:"id"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	profile, err := a.activeProfile()
	if err != nil {
		return err
	}
	groups, err := a.skills.Groups(profile)
	if err != nil {
		return err
	}
	out := groups[:0]
	for _, group := range groups {
		if group.ID != q.ID {
			out = append(out, group)
		}
	}
	if err := a.skills.SaveGroups(profile, out); err != nil {
		return err
	}
	return ev.Encode(map[string]any{"ok": true})
}

func (a *App) skillGroupAssign(ev event.Event, _ ws.Meta) error {
	var q struct {
		SkillID string `json:"skillId"`
		GroupID string `json:"groupId"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	profile, err := a.activeProfile()
	if err != nil {
		return err
	}
	if err := a.skills.SetSkillGroup(profile, q.SkillID, q.GroupID); err != nil {
		return err
	}
	return ev.Encode(map[string]any{"ok": true})
}
func isSkillFile(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return ext == ".md" || ext == ".mdc"
}

var skillFrontmatterRE = regexp.MustCompile(`(?ms)^---[ \t]*\r?\n(.*?)\r?\n---[ \t]*(?:\r?\n|$)`)
var skillNameRE = regexp.MustCompile(`(?mi)^\s*name\s*:\s*(?:"([^"]*)"|'([^']*)'|([^\r\n]*))\s*$`)
var skillDescriptionRE = regexp.MustCompile(`(?mi)^\s*description\s*:\s*(?:"([^"]*)"|'([^']*)'|([^\r\n]*))\s*$`)
var skillFoldedDescriptionRE = regexp.MustCompile(`(?ms)^\s*description\s*:\s*[>|][+-]?\s*\r?\n((?:[ \t]+[^\r\n]*(?:\r?\n|$))+)`)

func isGitRepositoryURL(value string) bool {
	clean := strings.TrimSuffix(strings.TrimSpace(value), "/")
	if strings.HasSuffix(clean, ".git") {
		return true
	}
	u, err := url.Parse(clean)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if host != "github.com" && host != "gitlab.com" && host != "bitbucket.org" {
		return false
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	return len(parts) == 2
}

func isArchiveURL(value string) bool {
	u, err := url.Parse(value)
	if err != nil {
		return false
	}
	name := strings.ToLower(u.Path)
	return strings.HasSuffix(name, ".zip") || strings.HasSuffix(name, ".tar") || strings.HasSuffix(name, ".tar.gz")
}

func extractSkillArchive(data []byte, kind string) (string, error) {
	root, err := os.MkdirTemp("", "arwos-skills-archive-")
	if err != nil {
		return "", err
	}
	removeOnError := true
	defer func() {
		if removeOnError {
			_ = os.RemoveAll(root)
		}
	}()
	writeFile := func(name string, directory bool, reader io.Reader) error {
		rel := filepath.Clean(filepath.FromSlash(name))
		if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("skill archive contains unsafe path %q", name)
		}
		target := filepath.Join(root, rel)
		if directory {
			if err := os.MkdirAll(target, 0755); err != nil {
				return err
			}
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		content, readErr := utils.ReadAllResponse(reader)
		if readErr != nil {
			return readErr
		}
		if err := os.WriteFile(target, content, 0644); err != nil {
			return err
		}
		return nil
	}
	if kind == ".zip" {
		archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return "", fmt.Errorf("read skill archive: %w", err)
		}
		for _, file := range archive.File {
			input, err := file.Open()
			if err != nil {
				return "", err
			}
			err = writeFile(file.Name, file.FileInfo().IsDir(), input)
			input.Close()
			if err != nil {
				return "", err
			}
		}
	} else {
		var reader io.Reader = bytes.NewReader(data)
		if kind == ".tar.gz" {
			gz, err := gzip.NewReader(bytes.NewReader(data))
			if err != nil {
				return "", fmt.Errorf("read gzip skill archive: %w", err)
			}
			defer gz.Close()
			reader = gz
		}
		tarReader := tar.NewReader(reader)
		for {
			header, err := tarReader.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return "", fmt.Errorf("read tar skill archive: %w", err)
			}
			if err := writeFile(header.Name, header.FileInfo().IsDir(), tarReader); err != nil {
				return "", err
			}
		}
	}
	removeOnError = false
	return root, nil
}

func rawGitFileURL(value string) string {
	u, err := url.Parse(value)
	if err != nil || strings.ToLower(u.Hostname()) != "github.com" {
		return value
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 5 || parts[2] != "blob" {
		return value
	}
	rawPath := strings.Join(append([]string{parts[0], parts[1]}, parts[3:]...), "/")
	return "https://raw.githubusercontent.com/" + rawPath
}

func skillMetadata(body []byte) (string, string, bool) {
	match := skillFrontmatterRE.FindSubmatch(body)
	if len(match) == 0 {
		return "", "", false
	}
	value := func(re *regexp.Regexp) string {
		parts := re.FindStringSubmatch(string(match[1]))
		if len(parts) == 0 {
			return ""
		}
		for _, part := range parts[1:] {
			if strings.TrimSpace(part) != "" {
				return strings.TrimSpace(part)
			}
		}
		return ""
	}
	name := value(skillNameRE)
	description := value(skillDescriptionRE)
	if folded := skillFoldedDescriptionRE.FindStringSubmatch(string(match[1])); len(folded) > 1 {
		description = strings.Join(strings.Fields(folded[1]), " ")
	}
	return name, description, name != "" && description != ""
}

func isSkillDocument(body []byte) bool {
	_, _, ok := skillMetadata(body)
	return ok
}
func discoveredSkill(name, content, origin string) map[string]any {
	skillName := strings.TrimSuffix(name, filepath.Ext(name))
	description := ""
	if parsedName, parsedDescription, ok := skillMetadata([]byte(content)); ok {
		skillName, description = parsedName, parsedDescription
	}
	return map[string]any{"tempId": entityID("discovered"), "name": skillName, "description": description, "icon": "bot", "accent": "indigo", "checked": true, "origin": origin, "content": content}
}
func (a *App) kbList(ev event.Event, _ ws.Meta) error {
	var q struct {
		ProfileID string   `json:"profileId"`
		Limit     int      `json:"limit"`
		LastID    string   `json:"lastId"`
		Query     string   `json:"query"`
		Tags      []string `json:"tags"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	if q.ProfileID == "" {
		id, err := a.activeProfile()
		if err != nil {
			return err
		}
		q.ProfileID = id
	}
	if q.Limit < 0 {
		return fmt.Errorf("limit must be non-negative")
	}
	if q.Limit == 0 {
		q.Limit = a.knowledge.PageSize()
	}
	// Tags are encoded separately in the public contract. The store applies
	// them as an additional filter when querying the cursor.
	docs, total, err := a.knowledge.List(q.ProfileID, q.LastID, q.Query, q.Tags, q.Limit)
	if err != nil {
		return err
	}
	next := ""
	if len(docs) > 0 {
		next = docs[len(docs)-1].ID
	}
	tags, err := a.knowledge.Tags(q.ProfileID)
	if err != nil {
		return err
	}
	return ev.Encode(map[string]any{"docs": docs, "total": total, "tags": tags, "hasMore": len(docs) == q.Limit, "nextLastId": next, "quotaBytes": a.kbQuotaBytes()})
}

const defaultKBQuotaBytes int64 = 25 * 1048576

func (a *App) kbQuotaBytes() int64 {
	if a.store != nil {
		if raw, err := a.store.Get("kb.quota_bytes"); err == nil {
			if value, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64); err == nil && value > 0 {
				return value
			}
		}
	}
	return defaultKBQuotaBytes
}
func (a *App) kbUpsert(ev event.Event, _ ws.Meta) error {
	var q models.KBDoc
	if err := ev.Decode(&q); err != nil {
		return err
	}
	if q.ProfileID == "" {
		id, err := a.activeProfile()
		if err != nil {
			return err
		}
		q.ProfileID = id
	}
	if q.ID == "" {
		q.ID = entityID("kb")
	}
	item, err := a.knowledge.Upsert(q)
	if err != nil {
		return err
	}
	return ev.Encode(item)
}
func (a *App) kbImportLink(ev event.Event, meta ws.Meta) error {
	var q struct {
		URL   string   `json:"url"`
		Title string   `json:"title"`
		Tags  []string `json:"tags"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	if q.URL == "" {
		return fmt.Errorf("url is required")
	}
	req, err := http.NewRequestWithContext(meta.Context(), http.MethodGet, q.URL, nil)
	if err != nil {
		return err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("document URL returned HTTP %d", res.StatusCode)
	}
	body, err := utils.ReadAllResponse(res.Body)
	if err != nil {
		return err
	}
	if q.Title == "" {
		q.Title = path.Base(req.URL.Path)
		if q.Title == "" || q.Title == "/" {
			q.Title = req.URL.Host
		}
	}
	profile, err := a.activeProfile()
	if err != nil {
		return err
	}
	doc, err := a.knowledge.Upsert(models.KBDoc{ID: entityID("kb"), ProfileID: profile, Title: q.Title, Tags: q.Tags, Source: q.URL, Kind: "link", Content: string(body)})
	if err != nil {
		return err
	}
	return ev.Encode(doc)
}
func (a *App) kbScanFolder(ev event.Event, _ ws.Meta) error {
	var q struct {
		Path string `json:"path"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	root := filepath.Clean(q.Path)
	allowed := false
	for _, e := range a.workspaces.List() {
		if filepath.Clean(e.Path) == root {
			allowed = true
			break
		}
	}
	if !allowed {
		return fmt.Errorf("folder must be an opened workspace")
	}
	out := []map[string]any{}
	err := filepath.WalkDir(root, func(name string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(name))
		if !map[string]bool{".md": true, ".mdc": true, ".txt": true, ".yaml": true, ".yml": true, ".json": true, ".go": true, ".html": true, ".js": true, ".jsx": true, ".ts": true, ".tsx": true, ".tsc": true}[ext] {
			return nil
		}
		rel, _ := filepath.Rel(root, name)
		info, _ := entry.Info()
		out = append(out, map[string]any{"path": filepath.ToSlash(rel), "name": entry.Name(), "ext": strings.TrimPrefix(ext, "."), "size": info.Size()})
		return nil
	})
	if err != nil {
		return err
	}
	return ev.Encode(out)
}
func (a *App) kbImportFiles(ev event.Event, _ ws.Meta) error {
	var q struct {
		Folder string   `json:"folder"`
		Files  []string `json:"files"`
		Title  string   `json:"title"`
		Tags   []string `json:"tags"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	root := filepath.Clean(q.Folder)
	profile, err := a.activeProfile()
	if err != nil {
		return err
	}
	imported := 0
	for _, rel := range q.Files {
		clean := filepath.Clean(rel)
		if clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) || filepath.IsAbs(clean) {
			return fmt.Errorf("file path is outside folder")
		}
		body, e := os.ReadFile(filepath.Join(root, clean))
		if e != nil {
			return e
		}
		title := strings.TrimSuffix(filepath.Base(clean), filepath.Ext(clean))
		if len(q.Files) == 1 && q.Title != "" {
			title = q.Title
		}
		if _, e = a.knowledge.Upsert(models.KBDoc{ID: entityID("kb"), ProfileID: profile, Title: title, Tags: q.Tags, Source: filepath.ToSlash(clean), Kind: "doc", Content: string(body)}); e != nil {
			return e
		}
		imported++
	}
	return ev.Encode(map[string]int{"imported": imported})
}
func (a *App) kbDelete(ev event.Event, _ ws.Meta) error {
	var q struct {
		ID        string `json:"id"`
		ProfileID string `json:"profileId"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	if q.ProfileID == "" {
		id, err := a.activeProfile()
		if err != nil {
			return err
		}
		q.ProfileID = id
	}
	if err := a.knowledge.Delete(q.ProfileID, q.ID); err != nil {
		return err
	}
	return ev.Encode(map[string]bool{"ok": true})
}
func (a *App) kbReindex(ev event.Event, _ ws.Meta) error {
	var q struct {
		ProfileID string `json:"profileId"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	if q.ProfileID == "" {
		id, err := a.activeProfile()
		if err != nil {
			return err
		}
		q.ProfileID = id
	}
	count, err := a.knowledge.Reindex(q.ProfileID)
	if err != nil {
		return err
	}
	return ev.Encode(map[string]int{"indexed": count, "chunks": count})
}
func (a *App) filesList(ev event.Event, _ ws.Meta) error {
	var q struct {
		WorkspaceID string `json:"workspaceId"`
		Dir         string `json:"dir"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	if q.WorkspaceID == "" {
		q.WorkspaceID = "default"
	}
	w, err := a.workspaces.Get(q.WorkspaceID)
	if err != nil {
		return err
	}
	items, err := w.ListFileInfo(q.Dir)
	if err != nil {
		return err
	}
	return ev.Encode(items)
}
func (a *App) filesRead(ev event.Event, _ ws.Meta) error {
	var q struct {
		WorkspaceID string `json:"workspaceId"`
		FileID      string `json:"fileId"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	if q.WorkspaceID == "" {
		q.WorkspaceID = "default"
	}
	if q.FileID == "" {
		return fmt.Errorf("fileId is required")
	}
	w, err := a.workspaces.Get(q.WorkspaceID)
	if err != nil {
		return err
	}
	const maxPreviewSize int64 = 5 << 20
	fileSize, err := w.FileSize(q.FileID)
	if err != nil {
		return err
	}
	if fileSize > maxPreviewSize {
		return fmt.Errorf("file %q is too large to preview (maximum is 5 MiB)", q.FileID)
	}
	body, err := w.ReadFileBytes(q.FileID)
	if err != nil {
		return err
	}
	contentType := mime2.DetectByFilename(q.FileID)
	ext := strings.TrimPrefix(strings.ToLower(path.Ext(q.FileID)), ".")
	image := strings.HasPrefix(contentType, "image/")
	if image || contentType == "application/octet-stream" {
		return ev.Encode(map[string]any{"id": q.FileID, "name": path.Base(q.FileID), "ext": ext, "content": "", "data": "data:" + http.DetectContentType(body) + ";base64," + base64.StdEncoding.EncodeToString(body), "size": len(body), "kind": "binary"})
	}
	return ev.Encode(map[string]any{"id": q.FileID, "name": path.Base(q.FileID), "ext": ext, "content": string(body), "size": len(body), "kind": "text"})
}
func (a *App) filesWrite(ev event.Event, _ ws.Meta) error {
	var q struct {
		WorkspaceID string `json:"workspaceId"`
		FileID      string `json:"fileId"`
		Content     string `json:"content"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	if q.WorkspaceID == "" {
		q.WorkspaceID = "default"
	}
	if q.FileID == "" {
		return fmt.Errorf("fileId is required")
	}
	w, err := a.workspaces.Get(q.WorkspaceID)
	if err != nil {
		return err
	}
	if err = w.WriteFile(q.FileID, q.Content); err != nil {
		return err
	}
	return ev.Encode(map[string]any{"id": q.FileID, "name": path.Base(q.FileID), "content": q.Content, "size": len(q.Content)})
}
func (a *App) filesAdd(ev event.Event, _ ws.Meta) error {
	var q struct {
		WorkspaceID string `json:"workspaceId"`
		Name        string `json:"name"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	if q.WorkspaceID == "" {
		q.WorkspaceID = "default"
	}
	if q.Name == "" {
		return fmt.Errorf("name is required")
	}
	w, err := a.workspaces.Get(q.WorkspaceID)
	if err != nil {
		return err
	}
	if err = w.WriteFile(q.Name, ""); err != nil {
		return err
	}
	return ev.Encode(map[string]any{"id": q.Name, "name": path.Base(q.Name), "content": "", "size": 0})
}
func (a *App) filesRemove(ev event.Event, _ ws.Meta) error {
	var q struct {
		WorkspaceID string `json:"workspaceId"`
		FileID      string `json:"fileId"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	if q.WorkspaceID == "" {
		q.WorkspaceID = "default"
	}
	if q.FileID == "" {
		return fmt.Errorf("fileId is required")
	}
	w, err := a.workspaces.Get(q.WorkspaceID)
	if err != nil {
		return err
	}
	if err = w.RemoveFile(q.FileID); err != nil {
		return err
	}
	return ev.Encode(map[string]bool{"ok": true})
}
func mustJSON(value any) json.RawMessage { b, _ := json.Marshal(value); return b }

func entityID(prefix string) string {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
	}
	return prefix + "-" + hex.EncodeToString(buf)
}

var profileScopedID = regexp.MustCompile(`^(.+)-([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})-([0-9a-fA-F]{24})$`)
var entityIDPattern = regexp.MustCompile(`^(.+)-([0-9a-fA-F]{24})$`)

// profileImportID namespaces exported entity IDs while preserving the hash.
// This also makes every reference in the imported model deterministic.
func profileImportID(id, profileID string) string {
	if id == "" || profileID == "" {
		return id
	}
	if match := profileScopedID.FindStringSubmatch(id); len(match) == 4 {
		return match[1] + "-" + profileID + "-" + match[3]
	}
	if match := entityIDPattern.FindStringSubmatch(id); len(match) == 3 {
		return match[1] + "-" + profileID + "-" + match[2]
	}
	return id
}

// applyPatch overlays a React `{id, patch}` request onto a persisted object.
// Using JSON here keeps the public camelCase schema as the only mapping layer.
func applyPatch(target any, patch json.RawMessage) error {
	if len(patch) == 0 || string(patch) == "null" {
		return nil
	}
	base, err := json.Marshal(target)
	if err != nil {
		return err
	}
	var values map[string]json.RawMessage
	if err = json.Unmarshal(base, &values); err != nil {
		return err
	}
	var updates map[string]json.RawMessage
	if err = json.Unmarshal(patch, &updates); err != nil {
		return fmt.Errorf("invalid patch: %w", err)
	}
	for key, value := range updates {
		values[key] = value
	}
	merged, err := json.Marshal(values)
	if err != nil {
		return err
	}
	return json.Unmarshal(merged, target)
}
func decode(p json.RawMessage, v any) error {
	if len(p) == 0 || string(p) == "null" {
		return nil
	}
	if e := json.Unmarshal(p, v); e != nil {
		return fmt.Errorf("invalid params: %w", e)
	}
	return nil
}
func (a *App) configGet(ev event.Event, _ ws.Meta) error {
	var q struct {
		Key string `json:"key"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	if a.store != nil {
		if q.Key != "" {
			value, err := a.store.Get(q.Key)
			if err != nil {
				return err
			}
			return ev.Encode(map[string]any{"key": q.Key, "value": value})
		}
		settings, err := a.store.Settings()
		if err != nil {
			return err
		}
		return ev.Encode(settings)
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	if q.Key != "" {
		return ev.Encode(map[string]any{"key": q.Key, "value": a.settings[q.Key]})
	}
	settings := make(map[string]string, len(a.settings))
	for key, value := range a.settings {
		settings[key] = value
	}
	return ev.Encode(settings)
}

func (a *App) configSet(ev event.Event, _ ws.Meta) error {
	var q struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	if q.Key == "" {
		return fmt.Errorf("key is required")
	}
	if a.store != nil {
		if err := a.store.Set(q.Key, q.Value); err != nil {
			return err
		}
		return ev.Encode(map[string]string{"key": q.Key, "value": q.Value})
	}
	a.mu.Lock()
	a.settings[q.Key] = q.Value
	a.mu.Unlock()
	return ev.Encode(map[string]string{"key": q.Key, "value": q.Value})
}
func (a *App) workspaceList(ev event.Event, _ ws.Meta) error {
	var q struct {
		WorkspaceID string `json:"workspace_id"`
		Dir         string `json:"dir"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	if q.WorkspaceID == "" {
		q.WorkspaceID = "default"
	}
	w, err := a.workspaces.Get(q.WorkspaceID)
	if err != nil {
		return err
	}
	files, err := w.ListFiles(q.Dir)
	if err != nil {
		return err
	}
	return ev.Encode(files)
}
func (a *App) workspaceRead(ev event.Event, _ ws.Meta) error {
	var q struct {
		WorkspaceID string `json:"workspace_id"`
		Path        string `json:"path"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	if q.Path == "" {
		return fmt.Errorf("path is required")
	}
	if q.WorkspaceID == "" {
		q.WorkspaceID = "default"
	}
	w, err := a.workspaces.Get(q.WorkspaceID)
	if err != nil {
		return err
	}
	content, err := w.ReadFile(q.Path)
	if err != nil {
		return err
	}
	return ev.Encode(content)
}
func (a *App) workspaceWrite(ev event.Event, _ ws.Meta) error {
	var q struct {
		WorkspaceID string `json:"workspace_id"`
		Path        string `json:"path"`
		Content     string `json:"content"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	if q.Path == "" {
		return fmt.Errorf("path is required")
	}
	if q.WorkspaceID == "" {
		q.WorkspaceID = "default"
	}
	w, err := a.workspaces.Get(q.WorkspaceID)
	if err != nil {
		return err
	}
	if err := w.WriteFile(q.Path, q.Content); err != nil {
		return err
	}
	return ev.Encode(map[string]bool{"ok": true})
}
func (a *App) workspaceOpen(ev event.Event, _ ws.Meta) error {
	var q struct {
		ID   string `json:"id"`
		Path string `json:"path"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	entry, err := a.workspaces.Open(q.ID, q.Path)
	if err != nil {
		return err
	}
	if a.store != nil {
		if err := a.persistWorkspace(models.Workspace{ID: entry.ID, Name: filepath.Base(entry.Path), FolderPath: entry.Path}); err != nil {
			return err
		}
	}
	return ev.Encode(a.workspaceView(entry))
}
func (a *App) workspaceCreate(ev event.Event, _ ws.Meta) error {
	var q struct {
		Name       string `json:"name"`
		FolderPath string `json:"folderPath"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	if q.FolderPath == "" {
		return fmt.Errorf("folderPath is required")
	}
	// Folder display names are not IDs. Always allocate the next free ID so
	// directories such as /projects/a/app and /projects/b/app can coexist.
	entry, err := a.workspaces.OpenNext(q.FolderPath)
	if err != nil {
		return err
	}
	if a.store != nil {
		if err := a.persistWorkspace(models.Workspace{ID: entry.ID, Name: filepath.Base(entry.Path), FolderPath: entry.Path}); err != nil {
			return err
		}
	}
	return ev.Encode(a.workspaceView(entry))
}
func (a *App) workspaceGet(ev event.Event, _ ws.Meta) error {
	var q struct {
		ID string `json:"id"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	if q.ID == "" {
		return fmt.Errorf("workspace id is required")
	}
	entry, err := a.workspaces.Entry(q.ID)
	if err != nil {
		return err
	}
	return ev.Encode(a.workspaceView(entry))
}
func (a *App) workspacePick(ev event.Event, meta ws.Meta) error {
	if a.picker == nil {
		return fmt.Errorf("native folder picker is unavailable")
	}
	path, err := a.picker.Directory(meta.Context())
	if err != nil {
		return err
	}
	entry, err := a.workspaces.OpenNext(path)
	if err != nil {
		return err
	}
	if a.store != nil {
		if err := a.persistWorkspace(models.Workspace{ID: entry.ID, Name: filepath.Base(entry.Path), FolderPath: entry.Path}); err != nil {
			return err
		}
	}
	return ev.Encode(a.workspaceView(entry))
}
func (a *App) workspacePickStart(ev event.Event, _ ws.Meta) error {
	if a.picker == nil {
		return fmt.Errorf("native folder picker is unavailable")
	}
	id := entityID("workspace-pick")
	a.pickMu.Lock()
	a.pickJobs[id] = &pickJob{}
	a.pickMu.Unlock()
	go func() {
		path, err := a.picker.Directory(context.Background())
		var result any
		var message string
		if err != nil {
			message = err.Error()
		} else if entry, openErr := a.workspaces.OpenNext(path); openErr != nil {
			message = openErr.Error()
		} else {
			if a.store != nil {
				if saveErr := a.persistWorkspace(models.Workspace{ID: entry.ID, Name: filepath.Base(entry.Path), FolderPath: entry.Path}); saveErr != nil {
					message = saveErr.Error()
				}
			}
			result = a.workspaceView(entry)
		}
		a.pickMu.Lock()
		if job := a.pickJobs[id]; job != nil {
			job.done, job.result, job.errText = true, result, message
		}
		a.pickMu.Unlock()
	}()
	return ev.Encode(map[string]string{"operationId": id})
}
func (a *App) workspacePickStatus(ev event.Event, _ ws.Meta) error {
	var q struct {
		OperationID string `json:"operationId"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	a.pickMu.RLock()
	job := a.pickJobs[q.OperationID]
	if job == nil {
		a.pickMu.RUnlock()
		return fmt.Errorf("pick operation not found")
	}
	done, result, message := job.done, job.result, job.errText
	a.pickMu.RUnlock()
	if !done {
		return ev.Encode(map[string]any{"status": "pending"})
	}
	a.pickMu.Lock()
	delete(a.pickJobs, q.OperationID)
	a.pickMu.Unlock()
	if message != "" {
		return ev.Encode(map[string]any{"status": "error", "message": message})
	}
	return ev.Encode(map[string]any{"status": "completed", "workspace": result})
}
func (a *App) workspaceClose(ev event.Event, _ ws.Meta) error {
	var q struct {
		ID string `json:"id"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	if q.ID == "" {
		return fmt.Errorf("workspace id is required")
	}
	if a.store != nil {
		if conversations, err := a.store.WorkspaceConversations(q.ID); err != nil {
			return err
		} else {
			for _, conversation := range conversations {
				_ = a.dialogs.Delete(dialog.SessionKey(conversation.ProfileID, conversation.ID))
				_ = a.memory.Delete(conversation.ProfileID, q.ID, conversation.ID)
			}
		}
		profile, err := a.activeProfile()
		if err != nil {
			return err
		}
		if err := a.store.WorkspaceDelete(profile, q.ID); err != nil {
			return err
		}
	}
	if err := a.workspaces.Close(q.ID); err != nil {
		return err
	}
	return ev.Encode(map[string]bool{"ok": true})
}
func (a *App) workspaceListOpen(ev event.Event, _ ws.Meta) error {
	entries := a.workspaces.List()
	if a.store != nil {
		profile, err := a.activeProfile()
		if err != nil {
			return err
		}
		saved, err := a.store.Workspaces(profile)
		if err != nil {
			return err
		}
		allowed := make(map[string]bool, len(saved))
		for _, item := range saved {
			allowed[item.ID] = true
		}
		filtered := entries[:0]
		for _, entry := range entries {
			if allowed[entry.ID] {
				filtered = append(filtered, entry)
			}
		}
		entries = filtered
	}
	items := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		items = append(items, a.workspaceView(entry))
	}
	return ev.Encode(items)
}

func (a *App) persistWorkspace(item models.Workspace) error {
	if a.store == nil {
		return nil
	}
	profile, err := a.activeProfile()
	if err != nil {
		return err
	}
	return a.store.WorkspaceUpsert(profile, item)
}

// workspaceView is the public Workspace model consumed by the React UI.
// Files themselves are deliberately loaded lazily through files.list.
func (a *App) workspaceView(entry workspace.Entry) map[string]any {
	name := filepath.Base(entry.Path)
	if name == "." || name == string(filepath.Separator) || name == "" {
		name = entry.ID
	}
	fileCount := 0
	if service, err := a.workspaces.Get(entry.ID); err == nil {
		if files, err := service.ListFileInfo(""); err == nil {
			fileCount = len(files)
		}
	}
	createdAt := ""
	if !entry.OpenedAt.IsZero() {
		createdAt = entry.OpenedAt.Format(time.RFC3339)
	}
	return map[string]any{
		"id": entry.ID, "name": name, "folderPath": entry.Path,
		"createdAt": createdAt, "files": []any{}, "fileCount": fileCount,
	}
}
func (a *App) skillsList(ev event.Event, _ ws.Meta) error {
	var q struct {
		Cursor string `json:"cursor"`
		Limit  int    `json:"limit"`
		Query  string `json:"query"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	profileID, err := a.activeProfile()
	if err != nil {
		return err
	}
	// Compatibility callers without paging still receive the full catalogue.
	// The settings UI explicitly sends limit=20 and uses the file-name cursor.
	if q.Limit > 0 || q.Cursor != "" || strings.TrimSpace(q.Query) != "" {
		if q.Limit <= 0 {
			q.Limit = 20
		}
		if q.Limit > 100 {
			q.Limit = 100
		}
		page, err := a.skills.SearchPage(profileID, q.Query, q.Cursor, q.Limit)
		if err != nil {
			return err
		}
		return ev.Encode(page)
	}
	items, err := a.skills.List(profileID)
	if err != nil {
		return err
	}
	return ev.Encode(items)
}
func (a *App) skillsReindex(ev event.Event, _ ws.Meta) error {
	profile, err := a.activeProfile()
	if err != nil {
		return err
	}
	count, err := a.skills.Reindex(profile)
	if err != nil {
		return err
	}
	return ev.Encode(map[string]int{"count": count})
}
func (a *App) skillsPickStart(ev event.Event, _ ws.Meta) error {
	if a.picker == nil {
		return fmt.Errorf("native folder picker is unavailable")
	}
	id := entityID("skills-pick")
	a.pickMu.Lock()
	a.pickJobs[id] = &pickJob{}
	a.pickMu.Unlock()
	go func() {
		path, err := a.picker.Directory(context.Background())
		a.pickMu.Lock()
		if job := a.pickJobs[id]; job != nil {
			job.done = true
			if err != nil {
				job.errText = err.Error()
			} else {
				job.result = path
			}
		}
		a.pickMu.Unlock()
	}()
	return ev.Encode(map[string]string{"operationId": id})
}
func (a *App) skillsPickStatus(ev event.Event, _ ws.Meta) error {
	var q struct {
		OperationID string `json:"operationId"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	a.pickMu.RLock()
	job := a.pickJobs[q.OperationID]
	if job == nil {
		a.pickMu.RUnlock()
		return fmt.Errorf("pick operation not found")
	}
	done, result, message := job.done, job.result, job.errText
	a.pickMu.RUnlock()
	if !done {
		return ev.Encode(map[string]any{"status": "pending"})
	}
	a.pickMu.Lock()
	delete(a.pickJobs, q.OperationID)
	a.pickMu.Unlock()
	if message != "" {
		return ev.Encode(map[string]any{"status": "error", "message": message})
	}
	return ev.Encode(map[string]any{"status": "completed", "path": result})
}
func (a *App) filesystemSkillsList(ev event.Event, _ ws.Meta) error {
	items, err := a.skills.FilesystemList()
	if err != nil {
		return err
	}
	return ev.Encode(items)
}
func (a *App) skillsGet(ev event.Event, _ ws.Meta) error {
	var q struct {
		Name      string `json:"name"`
		File      string `json:"file"`
		ListFiles bool   `json:"listFiles"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	if q.Name == "" {
		return fmt.Errorf("name is required")
	}
	profile, err := a.activeProfile()
	if err != nil {
		return err
	}
	if q.ListFiles {
		files, err := a.skills.Files(profile, q.Name)
		if err != nil {
			return err
		}
		return ev.Encode(files)
	}
	var content string
	if q.File == "" {
		content, err = a.skills.Get(profile, q.Name)
	} else {
		content, err = a.skills.ReadFile(profile, q.Name, q.File)
	}
	if err != nil {
		return err
	}
	return ev.Encode(content)
}
func (a *App) dialogHistory(ev event.Event, _ ws.Meta) error {
	var q struct {
		WorkspaceID string `json:"workspace_id"`
		DialogID    string `json:"dialog_id"`
		Limit       int    `json:"limit"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	if q.DialogID == "" {
		return fmt.Errorf("dialog_id is required")
	}
	if q.Limit == 0 {
		q.Limit = a.dialogs.HistoryLimit
	}
	profile, err := a.activeProfile()
	if err != nil {
		return err
	}
	history, err := a.dialogs.History(dialog.SessionKey(profile, q.DialogID), q.Limit)
	if err != nil {
		return err
	}
	return ev.Encode(history)
}

type chatSendQuery struct {
	DialogID       string   `json:"dialog_id"`
	Content        string   `json:"content"`
	Provider       string   `json:"provider"`
	Model          string   `json:"model"`
	AgentID        string   `json:"agentId"`
	Skills         []string `json:"skills"`
	WorkspaceID    string   `json:"workspace_id"`
	Resume         bool     `json:"resume"`
	ErrorMessageID string   `json:"errorMessageId"`
	AsGoal         bool     `json:"asGoal"`
}

func (a *App) chatSend(ev event.Event, meta ws.Meta) error {
	var q chatSendQuery
	if err := ev.Decode(&q); err != nil {
		return err
	}
	return a.runChatSend(q, meta.Context(), meta.ConnectID(), func(value any) error { return ev.Encode(value) })
}

func (a *App) runChatSend(q chatSendQuery, ctx context.Context, connectionID string, encode func(any) error) error {
	startedAt := time.Now()
	if q.DialogID == "" || (!q.Resume && q.Content == "") {
		return fmt.Errorf("dialog_id and content are required")
	}
	if q.WorkspaceID == "" {
		q.WorkspaceID = "default"
	}
	if _, e := a.workspaces.Get(q.WorkspaceID); e != nil {
		return e
	}
	publicDialogID := q.DialogID
	if a.store == nil {
		return fmt.Errorf("configuration store unavailable")
	}
	profileID, e := a.activeProfile()
	if e != nil {
		return e
	}
	q.DialogID = dialog.SessionKey(profileID, publicDialogID)
	var resumedGoal *dialog.Goal
	if q.Resume {
		resumedGoal, e = a.dialogs.Goal(q.DialogID)
		if e != nil {
			return fmt.Errorf("load dialog goal for continuation: %w", e)
		}
		if resumedGoal != nil {
			resumedGoal.Status = "running"
			for index := range resumedGoal.Tasks {
				if resumedGoal.Tasks[index].Status == "failed" {
					resumedGoal.Tasks[index].Status = "pending"
				}
			}
			if saveErr := a.dialogs.SaveGoal(q.DialogID, *resumedGoal); saveErr != nil {
				return fmt.Errorf("reopen dialog goal for continuation: %w", saveErr)
			}
		}
	}
	// A goal represents one concrete agent run. A fresh user request replaces a
	// completed/abandoned goal only when the run actually starts using tools.
	if !q.Resume {
		if clearErr := a.dialogs.DeleteGoal(q.DialogID); clearErr != nil {
			return fmt.Errorf("clear previous dialog goal: %w", clearErr)
		}
		if a.publish != nil {
			a.publish(StreamMessage{Type: "goal.clear", Payload: map[string]string{"dialog_id": publicDialogID}})
		}
	}
	if q.Resume && q.ErrorMessageID != "" {
		if resolveErr := a.dialogs.ResolveError(q.DialogID, q.ErrorMessageID); resolveErr != nil {
			return fmt.Errorf("resolve previous error in history: %w", resolveErr)
		}
	}
	existingHistory, historyErr := a.dialogs.History(q.DialogID, 1)
	if historyErr != nil {
		return historyErr
	}
	newConversation := len(existingHistory) == 0
	if q.Resume {
		fullHistory, readErr := a.dialogs.History(q.DialogID, 0)
		if readErr != nil {
			return readErr
		}
		for index := len(fullHistory) - 1; index >= 0; index-- {
			if fullHistory[index].Role == "user" && strings.TrimSpace(fullHistory[index].Content) != "" {
				q.Content = fullHistory[index].Content
				break
			}
		}
		if q.Content == "" {
			return fmt.Errorf("dialog has no user message to continue")
		}
	}
	conversationID := publicDialogID
	if _, e = a.store.ConversationUpsert(models.Conversation{ID: conversationID, ProfileID: profileID, WorkspaceID: q.WorkspaceID, AgentID: q.AgentID, Title: truncateTitle(q.Content)}); e != nil {
		return e
	}
	conversation, e := a.store.Conversation(profileID, conversationID)
	if e != nil {
		return e
	}
	if q.Resume && q.Model != "" {
		conversation.ActiveModel = q.Model
		if e = a.store.ConversationSetActiveModel(profileID, conversationID, q.Model); e != nil {
			return e
		}
	}
	profileProvider, apiKey, e := a.store.DefaultProfileProvider(profileID)
	if q.Provider != "" {
		profileProvider, apiKey, e = a.store.ProfileProviderSecret(profileID, q.Provider)
	}
	if e != nil {
		return fmt.Errorf("provider is not configured: %w", e)
	}
	system := prompts.DefaultAgentSystem
	explicitSkills := make(map[string]bool, len(q.Skills))
	for _, skillID := range q.Skills {
		explicitSkills[skillID] = true
	}
	model := ""
	mainModels := []string(nil)
	var selectedMCPIDs []string
	if q.AgentID != "" {
		if agents, x := a.store.Agents(profileID); x == nil {
			for _, candidate := range agents {
				if candidate.ID == q.AgentID {
					// Keep the built-in operating instructions and prepend the
					// agent-specific prompt as the first part of the system prompt.
					// An empty custom prompt must not add an empty message.
					if strings.TrimSpace(candidate.SystemPrompt) != "" {
						system = candidate.SystemPrompt + "\n\n" + system
					}
					mainModels = append(mainModels, candidate.MainModels...)
					groups, groupErr := a.skills.Groups(profileID)
					if groupErr != nil {
						return groupErr
					}
					allowedGroups := make(map[string]bool, len(candidate.SkillGroupIDs))
					for _, groupID := range candidate.SkillGroupIDs {
						allowedGroups[groupID] = true
					}
					for _, group := range groups {
						if allowedGroups[group.ID] {
							q.Skills = append(q.Skills, group.SkillIDs...)
						}
					}
					selectedMCPIDs = candidate.MCPIDs
					break
				}
			}
		}
	}
	// Explicit skill mentions are resolved before the model request. This makes
	// a skill reference authoritative instead of relying on the model to decide
	// whether it should call skills.get itself.
	managed, _ := a.skills.List(profileID)
	mentionedSkills := make([]models.Skill, 0)
	messageText := strings.ToLower(q.Content)
	for _, skill := range managed {
		if !skill.Enabled || skill.Name == "" {
			continue
		}
		name := strings.ToLower(skill.Name)
		if strings.Contains(messageText, "@"+name) || strings.Contains(messageText, name) {
			mentionedSkills = append(mentionedSkills, skill)
			q.Skills = append(q.Skills, skill.ID)
		}
	}
	extraTools := []toolexecutor.Tool(nil)
	mcpBindings := map[string]toolexecutor.MCPTool(nil)
	builtinAliases := map[string]string(nil)
	builtinAllowed := map[string]bool(nil)
	workspaceAccess := false
	if servers, mcpErr := a.store.ProfileMCP(profileID); mcpErr == nil {
		for _, server := range servers {
			for _, selected := range selectedMCPIDs {
				if selected == server.ID && server.Instructions != "" {
					system += prompts.MCPInstructions(server.Name, server.Instructions)
				}
			}
		}
		extraTools, mcpBindings = mcp.AgentTools(servers, selectedMCPIDs)
	}
	if builtins, builtinErr := a.builtinMCP(profileID); builtinErr == nil {
		selected := make(map[string]bool, len(selectedMCPIDs))
		for _, id := range selectedMCPIDs {
			selected[id] = true
		}
		builtinAliases, builtinAllowed = make(map[string]string), make(map[string]bool)
		for _, server := range builtins {
			// Built-in MCP servers are regular agent links. The global profile
			// enabled flag is not enough: a server must also be selected on the
			// agent, otherwise neither its prompt schema nor its executor binding
			// is exposed to this dialogue.
			// User interaction is a platform capability, not an optional model
			// tool: without it the model can only print choices as prose and the
			// UI cannot render a dialog. Always expose it to the agent.
			if !selected[server.ID] && server.BuiltinKey != "user" {
				continue
			}
			tools, aliases := builtinPromptTools(server)
			if server.BuiltinKey == "workspace" && len(tools) > 0 {
				workspaceAccess = true
			}
			extraTools = append(extraTools, tools...)
			for alias, target := range aliases {
				builtinAliases[alias], builtinAllowed[target] = target, true
			}
		}
	}
	if workspaceAccess {
		system += prompts.WorkspaceAccess
	}
	if a.tools != nil {
		system += a.tools.Prompt(extraTools)
	}
	if memory, memoryErr := a.memory.Get(profileID, q.WorkspaceID, publicDialogID); memoryErr == nil {
		system += prompts.SessionMemory(memory.Title, memory.Summary)
	}
	selectedSkillCatalog := make([]string, 0, len(q.Skills))
	for _, selected := range q.Skills {
		for _, skill := range managed {
			if skill.Enabled && (skill.ID == selected || skill.Name == selected) {
				selectedSkillCatalog = append(selectedSkillCatalog, fmt.Sprintf("%s (id: %s): %s", skill.Name, skill.ID, skill.Description))
				break
			}
		}
	}
	if len(selectedSkillCatalog) > 0 {
		system += prompts.SkillCatalog(selectedSkillCatalog)
	}
	for _, skill := range mentionedSkills {
		if content, readErr := a.skills.Get(profileID, skill.ID); readErr == nil && strings.TrimSpace(content) != "" {
			system += prompts.Skill(skill.Name, content)
		}
	}
	if newConversation {
		system += prompts.StartupIntroduction
	}
	mainModels = uniqueStrings(mainModels)
	if conversation.ActiveModel != "" && (len(mainModels) == 0 || containsString(mainModels, conversation.ActiveModel)) {
		model = conversation.ActiveModel
	} else if len(mainModels) > 0 {
		model = mainModels[0]
		if conversation.ActiveModel != "" {
			// Discard selections from the removed fallback-model format.
			_ = a.store.ConversationSetActiveModel(profileID, conversationID, "")
		}
	} else if len(profileProvider.Models) > 0 {
		model = profileProvider.Models[0]
	}
	// Agent model selections use the stable public form model@provider.
	// Resolve the provider here and pass only the model name to the LLM client.
	if at := strings.LastIndex(model, "@"); at > 0 && at < len(model)-1 {
		modelName, providerName := model[:at], model[at+1:]
		selected, selectedKey, lookupErr := a.store.ProfileProviderSecret(profileID, providerName)
		if lookupErr != nil {
			return fmt.Errorf("provider %q is not configured: %w", providerName, lookupErr)
		}
		profileProvider, apiKey, model = selected, selectedKey, modelName
	} else if slash := strings.Index(model, "/"); slash > 0 {
		// Accept the old provider/model form for agents saved by older builds.
		if selected, selectedKey, lookupErr := a.store.ProfileProviderSecret(profileID, model[:slash]); lookupErr == nil {
			profileProvider, apiKey, model = selected, selectedKey, model[slash+1:]
		}
	}
	var provider llm.Provider = llm.New(profileProvider.Kind, profileProvider.BaseURL, apiKey, model, a.providerProxy(profileID, profileProvider.ProxyID), llm.WithRPM(profileProvider.ID, profileProvider.RPM))
	activeRateLimitKey, activeRPM := profileProvider.ID, profileProvider.RPM
	activeGoal := resumedGoal
	publishGoal := func(goal *dialog.Goal) {
		if a.publish != nil && goal != nil {
			a.publish(StreamMessage{Type: "goal", Payload: map[string]any{"dialog_id": publicDialogID, "goal": goal}})
		}
	}
	if activeGoal != nil {
		publishGoal(activeGoal)
	}
	planningUsage := llm.Usage{}
	if q.AsGoal && !q.Resume {
		activeGoal, planningUsage = goalPlan(ctx, provider, publicDialogID, q.Content, extraTools)
		if saveErr := a.dialogs.SaveGoal(q.DialogID, *activeGoal); saveErr != nil {
			return fmt.Errorf("save dialog goal: %w", saveErr)
		}
		publishGoal(activeGoal)
	}
	if activeGoal != nil {
		tasks := make([]string, 0, len(activeGoal.Tasks))
		for index, task := range activeGoal.Tasks {
			tasks = append(tasks, fmt.Sprintf("%d. %s", index+1, task.Label))
		}
		system += prompts.GoalExecution(activeGoal.Goal, tasks)
	}
	engine := a.engines.Acquire(provider, appToolExecutor{registry: a.tools, scope: toolexecutor.Scope{WorkspaceID: q.WorkspaceID, ProfileID: profileID, DialogID: q.DialogID, MCPTools: mcpBindings, Aliases: builtinAliases, Allowed: builtinAllowed, MCPCall: a.callMCPTool, Request: func(ctx context.Context, args map[string]any) (any, error) {
		return a.requestUser(ctx, connectionID, publicDialogID, args)
	}}}, a.dialogs, system)
	engine.Model = model
	engine.ProviderName = profileProvider.Name
	engine.Resume = q.Resume
	engine.ToolDefinitions = make([]llm.ToolDefinition, 0, len(extraTools))
	for _, tool := range extraTools {
		engine.ToolDefinitions = append(engine.ToolDefinitions, llm.ToolDefinition{Name: tool.Name, Description: tool.Description, Parameters: tool.InputSchema})
	}
	if workspaceService, workspaceErr := a.workspaces.Get(q.WorkspaceID); workspaceErr == nil {
		if instructions, readErr := workspaceService.ReadFile("AGENTS.md"); readErr == nil && strings.TrimSpace(instructions) != "" {
			engine.SystemContext = "Repository instructions from the active workspace root (AGENTS.md):\n\n" + instructions
		}
	}
	defer a.engines.Release(engine)
	engine.OnTool = func(toolModel, toolProvider, name string, args map[string]any, result string, x error) {
		toolLabel := name
		if description := a.tools.Description(name); description != "" {
			toolLabel = description
		} else {
			for _, definition := range extraTools {
				if definition.Name == name && definition.Description != "" {
					toolLabel = definition.Description
					break
				}
			}
		}
		if result == "" && x == nil {
			if activeGoal == nil {
				activeGoal = &dialog.Goal{ID: dialog.NewMessageID(), DialogID: publicDialogID, Goal: goalTitle(q.Content), Status: "running", Tasks: []dialog.GoalTask{}}
			}
			started := false
			for index := range activeGoal.Tasks {
				task := &activeGoal.Tasks[index]
				if task.Status != "pending" || (len(task.Tools) > 0 && !containsString(task.Tools, name)) {
					continue
				}
				task.Status = "running"
				// Planned tasks may have been created from the model's technical
				// tool name. Replace it with the registered human-readable purpose
				// whenever the tool actually starts, including goals restored from
				// an older history.
				task.Label = toolLabel
				task.Attempts++
				task.LastTool = name
				task.Arguments = cloneToolArguments(args)
				task.Error = ""
				task.StartedAt = time.Now().UTC()
				started = true
				break
			}
			if !started {
				activeGoal.Tasks = append(activeGoal.Tasks, dialog.GoalTask{ID: dialog.NewMessageID(), Label: toolLabel, Tools: []string{name}, Attempts: 1, MaxAttempts: 3, Status: "running", StartedAt: time.Now().UTC()})
			}
			if saveErr := a.dialogs.SaveGoal(q.DialogID, *activeGoal); saveErr != nil {
				logx.Error("failed to save dialog goal", "dialog_id", publicDialogID, "err", saveErr)
			} else {
				publishGoal(activeGoal)
			}
		} else if activeGoal != nil {
			for index := len(activeGoal.Tasks) - 1; index >= 0; index-- {
				task := &activeGoal.Tasks[index]
				if task.Status != "running" {
					continue
				}
				if x != nil {
					task.Status = "failed"
					task.Error = x.Error()
					if task.MaxAttempts > task.Attempts {
						task.Status = "pending"
					}
				} else {
					task.Status = "done"
				}
				task.LastResult = truncateGoalResult(result)
				task.FinishedAt = time.Now().UTC()
				break
			}
			if saveErr := a.dialogs.SaveGoal(q.DialogID, *activeGoal); saveErr != nil {
				logx.Error("failed to update dialog goal", "dialog_id", publicDialogID, "err", saveErr)
			} else {
				publishGoal(activeGoal)
			}
		}
		if a.publish != nil {
			phase := "done"
			if result == "" && x == nil {
				phase = "start"
			}
			payload := map[string]any{"dialog_id": publicDialogID, "name": name, "arguments": args, "result": result, "phase": phase, "model": toolModel, "provider": toolProvider}
			if x != nil {
				payload["error"] = x.Error()
			}
			a.publish(StreamMessage{Type: "tool_call", Payload: payload})
		}
	}
	engine.OnReasoning = func(text string) {
		if a.publish != nil {
			a.publish(StreamMessage{Type: "reasoning", Payload: map[string]any{"dialog_id": publicDialogID, "text": text, "model": engine.Model, "provider": engine.ProviderName}})
		}
	}
	replayGoalEvidence := func() error {
		if activeGoal == nil {
			return nil
		}
		for index := range activeGoal.Tasks {
			task := &activeGoal.Tasks[index]
			if task.Status != "pending" || task.LastTool == "" || strings.HasPrefix(task.LastTool, "user.") {
				continue
			}
			if _, replayErr := engine.ExecuteTool(ctx, q.DialogID, llm.ToolCall{Name: task.LastTool, Arguments: cloneToolArguments(task.Arguments)}); replayErr != nil {
				return replayErr
			}
		}
		return nil
	}
	requestProseChoice := func(content string) (bool, error) {
		if activeGoal == nil || goalAllDone(activeGoal) {
			return false, nil
		}
		request, inferred := interaction.InferChoice(content)
		if !inferred {
			return false, nil
		}
		requestID := streamMessageID()
		request["reqId"] = requestID
		call := llm.ToolCall{Name: "user.choice", Arguments: cloneToolArguments(request)}
		engine.OnTool(engine.Model, engine.ProviderName, call.Name, call.Arguments, "", nil)
		if appendErr := a.dialogs.Append(q.DialogID, dialog.Message{Role: "tool_call", Tool: call.Name, Arguments: call.Arguments, Model: engine.Model, Provider: engine.ProviderName}); appendErr != nil {
			return true, appendErr
		}
		answer, requestErr := a.requestUser(ctx, connectionID, publicDialogID, request)
		if requestErr != nil {
			return true, requestErr
		}
		encoded, marshalErr := json.Marshal(map[string]any{"selection": answer})
		if marshalErr != nil {
			return true, marshalErr
		}
		result := string(encoded)
		engine.OnTool(engine.Model, engine.ProviderName, call.Name, call.Arguments, result, nil)
		if appendErr := a.dialogs.Append(q.DialogID, dialog.Message{Role: "tool_result", Tool: call.Name, Content: result, Model: engine.Model, Provider: engine.ProviderName}); appendErr != nil {
			return true, appendErr
		}
		return true, nil
	}
	streamed := false
	engine.OnChunk = func(text string) {
		streamed = true
		if a.publish != nil {
			a.publish(StreamMessage{Type: "chat_stream", Payload: map[string]string{"dialog_id": publicDialogID, "chunk": text}})
			a.publish(StreamMessage{Type: "chunk", Payload: map[string]string{"dialog_id": publicDialogID, "text": text}})
		}
	}
	if q.Resume && activeGoal != nil && goalHasExecutablePending(activeGoal) {
		// A previously interrupted or declined goal can be resumed after a page
		// or backend restart. Reuse the recorded evidence-gathering calls before
		// asking the model for its next turn.
		if replayErr := replayGoalEvidence(); replayErr != nil {
			return fmt.Errorf("replay goal evidence: %w", replayErr)
		}
	}
	if a.publish != nil {
		a.publish(StreamMessage{Type: "status", Payload: map[string]string{"dialog_id": publicDialogID, "agent_id": q.AgentID, "status": "thinking"}})
		a.publish(StreamMessage{Type: "msg.start", Payload: map[string]string{"dialog_id": publicDialogID, "agent_id": q.AgentID, "model": engine.Model, "provider": engine.ProviderName}})
	}
	out, usage, e := engine.Run(ctx, q.DialogID, q.Content)
	usage = addChatUsage(planningUsage, usage)
	if e == nil {
		if requested, requestErr := requestProseChoice(out); requestErr != nil {
			e = requestErr
		} else if requested {
			// The model produced a prose menu instead of user.choice. The
			// synthetic tool result is now in history, so continue the same goal
			// with the selected value rather than ending it at the question.
			engine.Resume = true
			var continuationUsage llm.Usage
			out, continuationUsage, e = engine.Run(ctx, q.DialogID, "")
			usage = addChatUsage(usage, continuationUsage)
		}
	}
	for retry := 0; e == nil && retry < 2 && activeGoal != nil && goalHasExecutablePending(activeGoal); retry++ {
		// Resume from the persisted conversation without adding a synthetic
		// user message. The goal instructions remain in the system prompt.
		engine.Resume = true
		_, retryUsage, retryErr := engine.Run(ctx, q.DialogID, "")
		usage = addChatUsage(usage, retryUsage)
		e = retryErr
	}
	completionApproved := false
	if e == nil && activeGoal != nil && goalAllDone(activeGoal) {
		for approvalAttempt := 0; approvalAttempt < 2; approvalAttempt++ {
			// A declined approval may require several model/tool turns before all
			// reopened tasks are actually completed. Never ask for approval again
			// while the persisted plan still contains unfinished work.
			if !goalAllDone(activeGoal) {
				break
			}
			approvalID := streamMessageID()
			activeGoal.Status = "awaiting_approval"
			activeGoal.Approval = &dialog.GoalApproval{
				ID: approvalID, Kind: "approval", Title: "Confirm goal completion",
				Detail: activeGoal.Goal, Command: "goal.complete", CreatedAt: time.Now().UTC(),
			}
			if saveErr := a.dialogs.SaveGoal(q.DialogID, *activeGoal); saveErr != nil {
				return fmt.Errorf("save goal approval: %w", saveErr)
			}
			publishGoal(activeGoal)
			if a.publish != nil {
				a.publish(StreamMessage{Type: "status", Payload: map[string]string{"dialog_id": publicDialogID, "agent_id": q.AgentID, "status": "awaiting_approval"}})
			}
			answer, requestErr := a.requestUser(ctx, connectionID, publicDialogID, map[string]any{
				"reqId": approvalID, "kind": "approval", "title": "Confirm goal completion", "detail": activeGoal.Goal,
				"command": "goal.complete",
			})
			if requestErr != nil {
				e = requestErr
				break
			}
			accepted, ok := answer.(bool)
			if ok && accepted {
				completionApproved = true
				activeGoal.Approval = nil
				_ = a.dialogs.SaveGoal(q.DialogID, *activeGoal)
				break
			}
			// A decline reopens every executable task and makes one verification
			// pass. The model must inspect the workspace again before asking for
			// completion approval a second time.
			for index := range activeGoal.Tasks {
				task := &activeGoal.Tasks[index]
				if len(task.Tools) == 0 {
					continue
				}
				task.Status, task.Error, task.LastResult = "pending", "", ""
				task.Attempts = 0
			}
			activeGoal.Status = "running"
			activeGoal.Approval = nil
			_ = a.dialogs.SaveGoal(q.DialogID, *activeGoal)
			publishGoal(activeGoal)
			engine.SystemPrompt += prompts.GoalVerification
			engine.Resume = true
			// Re-run the exact evidence-gathering operations from the completed
			// plan. Asking the model to remember which tools it used is unreliable
			// and previously left every task pending after a declined confirmation.
			if replayErr := replayGoalEvidence(); replayErr != nil {
				e = replayErr
				break
			}
			for verificationAttempt := 0; verificationAttempt < 3 && e == nil && !goalAllDone(activeGoal); verificationAttempt++ {
				_, verifyUsage, verifyErr := engine.Run(ctx, q.DialogID, "")
				usage = addChatUsage(usage, verifyUsage)
				e = verifyErr
				if e == nil && !goalAllDone(activeGoal) && !goalHasExecutablePending(activeGoal) {
					// There is no executable step left for another useful turn.
					break
				}
			}
		}
	}
	if activeGoal != nil {
		if e == nil && completionApproved {
			activeGoal.Status = "done"
			for index := range activeGoal.Tasks {
				if activeGoal.Tasks[index].Status == "running" {
					activeGoal.Tasks[index].Status = "done"
				} else if activeGoal.Tasks[index].Status == "pending" || activeGoal.Tasks[index].Status == "skipped" {
					activeGoal.Status = "incomplete"
				}
			}
		} else if e == nil {
			activeGoal.Status = "incomplete"
		} else if errors.Is(e, context.Canceled) {
			// Pause/stop commands persist their desired state before cancelling
			// the job; do not turn a paused goal into stopped here.
			if activeGoal.Status != "paused" && activeGoal.Status != "stopped" {
				activeGoal.Status = "stopped"
			}
			for index := range activeGoal.Tasks {
				if activeGoal.Tasks[index].Status == "running" {
					activeGoal.Tasks[index].Status = "skipped"
				}
			}
		} else {
			activeGoal.Status = "failed"
			for index := range activeGoal.Tasks {
				if activeGoal.Tasks[index].Status == "running" {
					activeGoal.Tasks[index].Status = "failed"
				}
			}
		}
		if saveErr := a.dialogs.SaveGoal(q.DialogID, *activeGoal); saveErr != nil {
			logx.Error("failed to finalize dialog goal", "dialog_id", publicDialogID, "err", saveErr)
		} else {
			publishGoal(activeGoal)
		}
	}
	if e != nil {
		if errors.Is(e, context.Canceled) {
			if a.publish != nil {
				a.publish(StreamMessage{Type: "status", Payload: map[string]string{"dialog_id": publicDialogID, "agent_id": q.AgentID, "status": "idle"}})
				a.publish(StreamMessage{Type: "done", Payload: map[string]any{"dialog_id": publicDialogID, "tokens": usage.TotalTokens, "contextTokens": usage.LastPromptTokens + usage.LastCompletionTokens, "latency": time.Since(startedAt).Milliseconds(), "rpm": llm.RequestsPerMinute(activeRateLimitKey), "rpmLimit": activeRPM, "stopped": true}})
			}
			return nil
		}
		errorID := dialog.NewMessageID()
		if appendErr := a.dialogs.Append(q.DialogID, dialog.Message{ID: errorID, Role: "assistant", Content: e.Error(), Model: engine.Model, Provider: engine.ProviderName, Error: true}); appendErr != nil {
			logx.Error("failed to persist chat error", "dialog_id", publicDialogID, "err", appendErr)
			errorID = ""
		}
		if a.publish != nil {
			a.publish(StreamMessage{Type: "status", Payload: map[string]string{"dialog_id": publicDialogID, "agent_id": q.AgentID, "status": "idle"}})
			a.publish(StreamMessage{Type: "error", Payload: map[string]string{"dialog_id": publicDialogID, "message": e.Error(), "message_id": errorID, "model": engine.Model, "provider": engine.ProviderName}})
		}
		return e
	}
	if a.publish != nil {
		if !streamed {
			a.publish(StreamMessage{Type: "chat_stream", Payload: map[string]string{"dialog_id": publicDialogID, "chunk": out}})
			a.publish(StreamMessage{Type: "chunk", Payload: map[string]string{"dialog_id": publicDialogID, "text": out}})
		}
		a.publish(StreamMessage{Type: "status", Payload: map[string]string{"dialog_id": publicDialogID, "agent_id": q.AgentID, "status": "idle"}})
		a.publish(StreamMessage{Type: "done", Payload: map[string]any{"dialog_id": publicDialogID, "content": out, "model": engine.Model, "provider": engine.ProviderName, "tokens": usage.TotalTokens, "contextTokens": usage.LastPromptTokens + usage.LastCompletionTokens, "latency": time.Since(startedAt).Milliseconds(), "rpm": llm.RequestsPerMinute(activeRateLimitKey), "rpmLimit": activeRPM}})
	}
	if encode != nil {
		return encode(map[string]string{"content": out})
	}
	return nil
}

func (a *App) providerProxy(profileID, id string) *llm.ProxyConfig {
	if a.proxies == nil || id == "" {
		return nil
	}
	p, err := a.proxies.Secret(profileID, id)
	if err != nil {
		return nil
	}
	return &llm.ProxyConfig{Type: p.Type, Host: p.Host, Port: p.Port, Username: p.Username, Password: p.Password, InsecureSkipVerify: p.InsecureSkipVerify}
}
func truncateTitle(value string) string {
	runes := []rune(value)
	if len(runes) > 48 {
		return string(runes[:48]) + "…"
	}
	return value
}

func goalTitle(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	runes := []rune(value)
	if len(runes) > 140 {
		return string(runes[:140]) + "…"
	}
	return value
}

func goalPlan(ctx context.Context, provider llm.Provider, dialogID, request string, availableTools []toolexecutor.Tool) (*dialog.Goal, llm.Usage) {
	goal := defaultGoal(dialogID, request)
	toolNames := make([]string, 0, len(availableTools))
	for _, tool := range availableTools {
		toolNames = append(toolNames, tool.Name)
	}
	planningRequest := prompts.GoalPlanningRequest(request)
	if len(toolNames) > 0 {
		planningRequest += "\nAvailable tools (use exact names in task tools):\n" + strings.Join(toolNames, ", ")
	}
	response, err := provider.ChatCompletion(ctx, prompts.GoalPlanningSystem, []llm.Message{{Role: "user", Content: planningRequest}}, false)
	if err != nil {
		logx.Error("goal planning request failed", "dialog_id", dialogID, "err", err)
		return goal, llm.Usage{}
	}
	if err := applyGoalPlan(goal, response.Content); err != nil {
		logx.Error("goal planning response is not valid JSON", "dialog_id", dialogID, "err", err)
	}
	return goal, response.Usage
}

func defaultGoal(dialogID, request string) *dialog.Goal {
	title := goalTitle(request)
	return &dialog.Goal{
		ID:       dialog.NewMessageID(),
		DialogID: dialogID,
		Goal:     title,
		Status:   "running",
		Tasks:    []dialog.GoalTask{{ID: dialog.NewMessageID(), Label: title, Status: "pending"}},
	}
}

// applyGoalPlan accepts a JSON object even when a provider wraps it in a
// Markdown fence or adds a short sentence before it. It intentionally keeps
// the safe fallback task when no useful task labels were returned.
func applyGoalPlan(goal *dialog.Goal, content string) error {
	start, end := strings.Index(content, "{"), strings.LastIndex(content, "}")
	if start < 0 || end <= start {
		return fmt.Errorf("goal plan does not contain a JSON object")
	}
	var plan struct {
		Goal  string `json:"goal"`
		Tasks []struct {
			Label     string   `json:"label"`
			Tools     []string `json:"tools"`
			DependsOn []string `json:"dependsOn"`
		} `json:"tasks"`
	}
	if err := json.Unmarshal([]byte(content[start:end+1]), &plan); err != nil {
		return err
	}
	if strings.TrimSpace(plan.Goal) != "" {
		goal.Goal = goalTitle(plan.Goal)
	}
	planned := make([]dialog.GoalTask, 0, min(len(plan.Tasks), 8))
	for _, task := range plan.Tasks {
		label := strings.Join(strings.Fields(task.Label), " ")
		if label == "" || len(planned) >= 8 {
			continue
		}
		tools := uniqueStrings(task.Tools)
		planned = append(planned, dialog.GoalTask{ID: dialog.NewMessageID(), Label: label, Tools: tools, DependsOn: task.DependsOn, MaxAttempts: 3, Status: "pending"})
	}
	if len(planned) > 0 {
		goal.Tasks = planned
	}
	if len(goal.Tasks) == 0 {
		goal.Tasks = append(goal.Tasks, dialog.GoalTask{ID: dialog.NewMessageID(), Label: goal.Goal, Status: "pending"})
	}
	return nil
}

func addChatUsage(first, second llm.Usage) llm.Usage {
	second.PromptTokens += first.PromptTokens
	second.CompletionTokens += first.CompletionTokens
	second.TotalTokens += first.TotalTokens
	if second.TotalTokens == 0 {
		second.TotalTokens = second.PromptTokens + second.CompletionTokens
	}
	return second
}

func truncateGoalResult(value string) string {
	runes := []rune(value)
	if len(runes) > 2000 {
		return string(runes[:2000]) + "…"
	}
	return value
}

func cloneToolArguments(value map[string]any) map[string]any {
	if len(value) == 0 {
		return map[string]any{}
	}
	// Tool arguments originate from JSON, so a marshal round trip is a small,
	// reliable deep copy for nested arrays and objects as well.
	encoded, err := json.Marshal(value)
	if err != nil {
		return map[string]any{}
	}
	copy := make(map[string]any, len(value))
	if err := json.Unmarshal(encoded, &copy); err != nil {
		return map[string]any{}
	}
	return copy
}

func goalHasExecutablePending(goal *dialog.Goal) bool {
	for _, task := range goal.Tasks {
		if task.Status == "pending" && len(task.Tools) > 0 {
			return true
		}
	}
	return false
}

func goalAllDone(goal *dialog.Goal) bool {
	if len(goal.Tasks) == 0 {
		return false
	}
	for _, task := range goal.Tasks {
		if task.Status != "done" {
			return false
		}
	}
	return true
}
func (a *App) mcpList(ev event.Event, _ ws.Meta) error {
	if a.store != nil {
		profileID, err := a.activeProfile()
		if err != nil {
			return err
		}
		items, err := a.store.ProfileMCP(profileID)
		if err != nil {
			return err
		}
		builtins, err := a.builtinMCP(profileID)
		if err != nil {
			return err
		}
		items = append(items, builtins...)
		return ev.Encode(items)
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	out := append([]mcp.Config(nil), a.mcp...)
	sort.Slice(out, func(i, j int) bool { return out[i].Order < out[j].Order })
	return ev.Encode(out)
}
func (a *App) profileMCPUpsert(ev event.Event, _ ws.Meta) error {
	var request struct {
		ID    string          `json:"id"`
		Patch json.RawMessage `json:"patch"`
	}
	if err := ev.Decode(&request); err != nil {
		return err
	}
	var q models.MCPServer
	if request.ID != "" {
		profileID, err := a.activeProfile()
		if err != nil {
			return err
		}
		items, err := a.store.ProfileMCP(profileID)
		if err != nil {
			return err
		}
		builtins, err := a.builtinMCP(profileID)
		if err != nil {
			return err
		}
		items = append(items, builtins...)
		for _, item := range items {
			if item.ID == request.ID {
				q = item
				break
			}
		}
		if q.ID == "" {
			return fmt.Errorf("MCP server %q not found", request.ID)
		}
		if err = applyPatch(&q, request.Patch); err != nil {
			return err
		}
	} else if err := ev.Decode(&q); err != nil {
		return err
	}
	if q.ProfileID == "" {
		id, err := a.activeProfile()
		if err != nil {
			return err
		}
		q.ProfileID = id
	}
	if q.ID == "" {
		q.ID = entityID("mcp")
	}
	if strings.HasPrefix(q.ID, builtinMCPIDPrefix) {
		item, err := a.updateBuiltinMCP(q.ProfileID, q)
		if err != nil {
			return err
		}
		return ev.Encode(item)
	}
	if err := a.validateMCPToolAliases(q); err != nil {
		return err
	}
	item, err := a.store.ProfileMCPUpsert(q)
	if err != nil {
		return err
	}
	return ev.Encode(item)
}

func (a *App) validateMCPToolAliases(candidate models.MCPServer) error {
	if a.store == nil {
		return nil
	}
	servers, err := a.store.ProfileMCP(candidate.ProfileID)
	if err != nil {
		return err
	}
	if builtins, builtinErr := a.builtinMCP(candidate.ProfileID); builtinErr != nil {
		return builtinErr
	} else {
		servers = append(servers, builtins...)
	}
	used := make(map[string]string)
	for _, server := range servers {
		if server.ID == candidate.ID {
			continue
		}
		for _, tool := range server.Tools {
			alias := strings.TrimSpace(tool.Alias)
			if alias == "" {
				alias = strings.TrimSpace(tool.Name)
			}
			if alias != "" {
				used[alias] = server.Name
			}
		}
	}
	conflicts := make([]string, 0)
	local := make(map[string]bool)
	for _, tool := range candidate.Tools {
		alias := strings.TrimSpace(tool.Alias)
		if alias == "" {
			alias = strings.TrimSpace(tool.Name)
		}
		if alias == "" {
			continue
		}
		if local[alias] {
			conflicts = append(conflicts, alias+" (duplicate in this server)")
			continue
		}
		local[alias] = true
		if server, exists := used[alias]; exists {
			conflicts = append(conflicts, alias+" (already used by "+server+")")
		}
	}
	if len(conflicts) > 0 {
		sort.Strings(conflicts)
		return fmt.Errorf("MCP tool aliases must be unique within the profile: %s", strings.Join(conflicts, ", "))
	}
	return nil
}
func (a *App) mcpSet(ev event.Event, _ ws.Meta) error {
	var q models.MCPConfig
	if err := ev.Decode(&q); err != nil {
		return err
	}
	if q.Name == "" || q.Type == "" || q.Endpoint == "" {
		return fmt.Errorf("name, type and endpoint are required")
	}
	if q.Type != "http" && q.Type != "stdio" {
		return fmt.Errorf("unsupported mcp type")
	}
	if a.store == nil {
		return fmt.Errorf("configuration store unavailable")
	}
	item, err := a.store.MCPUpsert(q)
	if err != nil {
		return err
	}
	return ev.Encode(item)
}
func (a *App) mcpDelete(ev event.Event, _ ws.Meta) error {
	var q struct {
		Name string `json:"name"`
		ID   string `json:"id"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	if strings.HasPrefix(q.ID, builtinMCPIDPrefix) {
		return fmt.Errorf("system MCP server cannot be deleted")
	}
	if q.Name == "" && q.ID == "" {
		return fmt.Errorf("id or name is required")
	}
	if a.store == nil {
		return fmt.Errorf("configuration store unavailable")
	}
	if q.ID != "" {
		profileID, err := a.activeProfile()
		if err != nil {
			return err
		}
		if err = a.store.ProfileMCPDelete(profileID, q.ID); err != nil {
			return err
		}
		return ev.Encode(map[string]bool{"ok": true})
	}
	if err := a.store.MCPDelete(q.Name); err != nil {
		return err
	}
	return ev.Encode(map[string]bool{"ok": true})
}
func (a *App) mcpConfig(p json.RawMessage) (mcp.Config, error) {
	var q struct {
		Name string `json:"name"`
	}
	if e := decode(p, &q); e != nil {
		return mcp.Config{}, e
	}
	if a.store != nil {
		if profileID, err := a.activeProfile(); err == nil {
			if items, err := a.store.ProfileMCP(profileID); err == nil {
				for _, item := range items {
					if item.ID == q.Name || item.Name == q.Name {
						endpoint := item.URL
						if item.Transport == "stdio" {
							endpoint = item.Command
						}
						return mcp.Config{Name: item.Name, Type: item.Transport, Endpoint: endpoint, Prefix: item.Prefix}, nil
					}
				}
			}
		}
		items, e := a.store.MCPList()
		if e != nil {
			return mcp.Config{}, e
		}
		for _, x := range items {
			if x.Name == q.Name {
				return mcp.Config{Name: x.Name, Type: x.Type, Endpoint: x.Endpoint, Prefix: x.Prefix, Order: x.Order}, nil
			}
		}
		return mcp.Config{}, fmt.Errorf("mcp server %q not found", q.Name)
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	for _, c := range a.mcp {
		if c.Name == q.Name {
			return c, nil
		}
	}
	return mcp.Config{}, fmt.Errorf("mcp server %q not found", q.Name)
}
func (a *App) mcpConfigFor(name string) (mcp.Config, error) {
	if a.store != nil {
		if profileID, err := a.activeProfile(); err == nil {
			if items, err := a.store.ProfileMCP(profileID); err == nil {
				for _, item := range items {
					if !item.Enabled || (item.ID != name && item.Name != name && item.Prefix != name) {
						continue
					}
					endpoint := item.URL
					if item.Transport == "stdio" {
						endpoint = item.Command
					}
					return mcp.Config{Name: item.Name, Type: item.Transport, Endpoint: endpoint, Prefix: item.Prefix}, nil
				}
			}
		}
		items, e := a.store.MCPList()
		if e != nil {
			return mcp.Config{}, e
		}
		for _, x := range items {
			if x.Name == name || x.Prefix == name {
				return mcp.Config{Name: x.Name, Type: x.Type, Endpoint: x.Endpoint, Prefix: x.Prefix, Order: x.Order}, nil
			}
		}
	}
	b, _ := json.Marshal(map[string]string{"name": name})
	return a.mcpConfig(b)
}
func (a *App) mcpHealth(ev event.Event, meta ws.Meta) error {
	var q struct {
		Name string `json:"name"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	c, err := a.mcpConfig(mustJSON(q))
	if err != nil {
		return err
	}
	if err = a.mcpManager.Health(meta.Context(), c); err != nil {
		return err
	}
	return ev.Encode(map[string]bool{"healthy": true})
}
func (a *App) mcpTools(ev event.Event, meta ws.Meta) error {
	var q struct {
		Name      string `json:"name"`
		Transport string `json:"transport"`
		Command   string `json:"command"`
		URL       string `json:"url"`
		Prefix    string `json:"prefix"`
	}
	if e := ev.Decode(&q); e != nil {
		return e
	}
	c := mcp.Config{}
	var e error
	if q.Transport != "" {
		endpoint := q.URL
		if q.Transport == "stdio" {
			endpoint = q.Command
		}
		c = mcp.Config{Name: q.Name, Type: q.Transport, Endpoint: endpoint, Prefix: q.Prefix}
	} else {
		c, e = a.mcpConfig(mustJSON(q))
	}
	if e != nil {
		return e
	}
	info, e := a.mcpManager.Initialize(meta.Context(), c)
	if e != nil {
		return e
	}
	tools, e := a.mcpManager.ListTools(meta.Context(), c)
	if e != nil {
		return e
	}
	out := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		out = append(out, map[string]any{"name": tool.Name, "desc": tool.Description, "inputSchema": tool.InputSchema, "alias": tool.Name, "enabled": true})
	}
	if q.Name != "" && a.store != nil {
		if profileID, profileErr := a.activeProfile(); profileErr == nil {
			if servers, listErr := a.store.ProfileMCP(profileID); listErr == nil {
				for _, server := range servers {
					if server.Name != q.Name {
						continue
					}
					server.Instructions = info.Instructions
					server.Tools = make([]models.MCPTool, 0, len(tools))
					for _, tool := range tools {
						server.Tools = append(server.Tools, models.MCPTool{Name: tool.Name, Alias: tool.Name, Description: tool.Description, InputSchema: tool.InputSchema, Enabled: true})
					}
					_, _ = a.store.ProfileMCPUpsert(server)
					break
				}
			}
		}
	}
	return ev.Encode(map[string]any{"tools": out})
}
func (a *App) providersList(ev event.Event, _ ws.Meta) error {
	if a.store == nil {
		return ev.Encode([]any{})
	}
	profileID, err := a.activeProfile()
	if err != nil {
		return err
	}
	items, err := a.store.ProfileProviders(profileID)
	if err != nil {
		return err
	}
	return ev.Encode(items)
}
func (a *App) profileProviderUpsert(ev event.Event, _ ws.Meta) error {
	var request struct {
		ID          string          `json:"id"`
		Patch       json.RawMessage `json:"patch"`
		APIKey      string          `json:"apiKey"`
		ClearAPIKey bool            `json:"clearApiKey"`
	}
	if err := ev.Decode(&request); err != nil {
		return err
	}
	var q struct {
		models.Provider
		APIKey string `json:"apiKey"`
	}
	var previousModels []string
	var previousProviderName string
	if request.ID != "" {
		profileID, err := a.activeProfile()
		if err != nil {
			return err
		}
		items, err := a.store.ProfileProviders(profileID)
		if err != nil {
			return err
		}
		for _, item := range items {
			if item.ID == request.ID {
				q.Provider = item
				previousModels = append(previousModels, item.Models...)
				previousProviderName = item.Name
				break
			}
		}
		if q.ID == "" {
			return fmt.Errorf("provider %q not found", request.ID)
		}
		if err = applyPatch(&q.Provider, request.Patch); err != nil {
			return err
		}
		q.APIKey = request.APIKey
		if q.APIKey == "" {
			var patchSecret struct {
				APIKey string `json:"apiKey"`
			}
			if err := json.Unmarshal(request.Patch, &patchSecret); err != nil {
				return err
			}
			q.APIKey = patchSecret.APIKey
		}
	} else if err := ev.Decode(&q); err != nil {
		return err
	}
	if q.ProfileID == "" {
		id, err := a.activeProfile()
		if err != nil {
			return err
		}
		q.ProfileID = id
	}
	if q.ID == "" {
		q.ID = entityID("provider")
	}
	if err := validateProfileProvider(q.Provider); err != nil {
		return err
	}
	item, err := a.store.ProfileProviderUpsert(q.Provider, q.APIKey)
	if err != nil {
		return err
	}
	if request.ClearAPIKey && request.ID != "" {
		if err := a.store.ProfileProviderClearAPIKey(q.ProfileID, q.ID); err != nil {
			return err
		}
		item.HasAPIKey = false
	}
	if request.ID != "" {
		current := make(map[string]struct{}, len(item.Models))
		for _, model := range item.Models {
			current[model] = struct{}{}
		}
		removed := make(map[string]struct{})
		for _, model := range previousModels {
			qualifiedModel := model + "@" + item.Name
			previousQualifiedModel := model + "@" + previousProviderName
			// Disabling a provider makes every model belonging to it
			// unavailable to agents, even if its model list was unchanged.
			if !item.Enabled {
				removed[model] = struct{}{}
				removed[qualifiedModel] = struct{}{}
				removed[previousQualifiedModel] = struct{}{}
			} else if _, exists := current[model]; !exists {
				removed[model] = struct{}{}
				removed[qualifiedModel] = struct{}{}
				removed[previousQualifiedModel] = struct{}{}
			}
		}
		if err := a.store.RemoveProviderModels(item.ProfileID, removed); err != nil {
			return err
		}
	}
	return ev.Encode(item)
}

func validateProfileProvider(p models.Provider) error {
	if strings.TrimSpace(p.Name) == "" {
		return fmt.Errorf("provider name is required")
	}
	if strings.TrimSpace(p.Kind) == "" {
		return fmt.Errorf("provider kind is required")
	}
	if p.RPM < 0 {
		return fmt.Errorf("provider RPM must be zero or greater")
	}
	allowed := map[string]bool{"openai": true, "anthropic": true, "ollama": true, "openrouter": true, "deepseek": true, "mistral": true, "groq": true, "xai": true, "google": true, "custom": true}
	if !allowed[strings.ToLower(strings.TrimSpace(p.Kind))] {
		return fmt.Errorf("unsupported provider kind %q", p.Kind)
	}
	u, err := url.Parse(strings.TrimSpace(p.BaseURL))
	if err != nil || u.Scheme == "" || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return fmt.Errorf("provider base URL must be a valid http or https URL")
	}
	if u.User != nil {
		return fmt.Errorf("provider base URL must not contain credentials")
	}
	return nil
}
func (a *App) providerModels(ev event.Event, meta ws.Meta) error {
	var q struct {
		Name    string              `json:"name"`
		ID      string              `json:"id"`
		Kind    string              `json:"kind"`
		BaseURL string              `json:"baseUrl"`
		APIKey  string              `json:"apiKey"`
		Proxy   *models.ProxyConfig `json:"proxy"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	if strings.TrimSpace(q.BaseURL) == "" && strings.TrimSpace(q.Name) == "" && strings.TrimSpace(q.ID) == "" {
		return fmt.Errorf("baseUrl is required when fetching models for a provider that has not been saved")
	}
	// The UI intentionally never receives provider secrets. When an existing
	// provider is edited, an empty API key means "keep the stored key".
	if q.ID != "" && q.APIKey == "" && a.store != nil {
		if profileID, err := a.activeProfile(); err == nil {
			if provider, key, err := a.store.ProfileProviderSecret(profileID, q.ID); err == nil {
				q.APIKey = key
				if q.Kind == "" {
					q.Kind = provider.Kind
				}
				if q.BaseURL == "" {
					q.BaseURL = provider.BaseURL
				}
			}
		}
	}
	if q.BaseURL != "" {
		var models []string
		var err error
		models, err = llm.New(q.Kind, q.BaseURL, q.APIKey, "", modelProxy(q.Proxy)).ListModels(meta.Context())
		if err != nil {
			return err
		}
		return ev.Encode(map[string]any{"models": models})
	}
	if q.Name == "" {
		q.Name = "default"
	}
	if a.store == nil {
		return fmt.Errorf("configuration store unavailable")
	}
	if profileID, err := a.activeProfile(); err == nil {
		lookup := q.ID
		if lookup == "" {
			lookup = q.Name
		}
		if lookup != "" {
			if provider, key, err := a.store.ProfileProviderSecret(profileID, lookup); err == nil {
				models, err := llm.New(provider.Kind, provider.BaseURL, key, firstModel(provider.Models), a.providerProxy(profileID, provider.ProxyID), llm.WithRPM(provider.ID, provider.RPM)).ListModels(meta.Context())
				if err != nil {
					return err
				}
				return ev.Encode(map[string]any{"models": models})
			}
		}
	}
	pc, e := a.store.Provider(q.Name)
	if e != nil {
		return e
	}
	models, err := llm.New(pc.Type, pc.BaseURL, pc.APIKey, pc.Model, nil).ListModels(meta.Context())
	if err != nil {
		return err
	}
	return ev.Encode(map[string]any{"models": models})
}

func (a *App) providerContextWindow(ev event.Event, meta ws.Meta) error {
	var q struct{ Name, ID, Kind, BaseURL, Model, APIKey string }
	if err := ev.Decode(&q); err != nil {
		return err
	}
	if q.Model == "" {
		return fmt.Errorf("model is required")
	}
	var proxyConfig *llm.ProxyConfig
	rateLimitKey, rpm := q.ID, 0
	if q.ID != "" {
		if a.store == nil {
			return fmt.Errorf("configuration store unavailable")
		}
		profileID, err := a.activeProfile()
		if err != nil {
			return err
		}
		provider, key, err := a.store.ProfileProviderSecret(profileID, q.ID)
		if err != nil {
			return fmt.Errorf("read provider credentials: %w", err)
		}
		if q.APIKey == "" {
			q.APIKey = key
		}
		if q.Kind == "" {
			q.Kind = provider.Kind
		}
		if q.BaseURL == "" {
			q.BaseURL = provider.BaseURL
		}
		proxyConfig = a.providerProxy(profileID, provider.ProxyID)
		rateLimitKey, rpm = provider.ID, provider.RPM
	}
	window, err := llm.New(q.Kind, q.BaseURL, q.APIKey, q.Model, proxyConfig, llm.WithRPM(rateLimitKey, rpm)).ContextWindow(meta.Context())
	if err != nil {
		return err
	}
	return ev.Encode(map[string]int{"contextWindow": window})
}
func modelProxy(p *models.ProxyConfig) *llm.ProxyConfig {
	if p == nil || p.Host == "" {
		return nil
	}
	return &llm.ProxyConfig{Type: p.Type, Host: p.Host, Port: p.Port, Username: p.Username, Password: p.Password}
}

// providerCheckConnection performs the connectivity check on the server. The
// client only supplies the saved provider id; credentials and proxy settings
// are resolved from the profile store and never have to cross the UI again.
func (a *App) providerCheckConnection(ev event.Event, meta ws.Meta) error {
	var q struct {
		ID string `json:"id"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	if strings.TrimSpace(q.ID) == "" {
		return fmt.Errorf("provider id is required")
	}
	started := time.Now()
	profileID, err := a.activeProfile()
	if err != nil {
		return err
	}
	provider, key, err := a.store.ProfileProviderSecret(profileID, q.ID)
	if err != nil {
		return err
	}
	if _, err = llm.New(provider.Kind, provider.BaseURL, key, firstModel(provider.Models), a.providerProxy(profileID, provider.ProxyID), llm.WithRPM(provider.ID, provider.RPM)).ListModels(meta.Context()); err != nil {
		return err
	}
	return ev.Encode(map[string]any{"ok": true, "latency": time.Since(started).Milliseconds()})
}
func firstModel(models []string) string {
	if len(models) > 0 {
		return models[0]
	}
	return ""
}
func (a *App) providerSet(ev event.Event, _ ws.Meta) error {
	var q models.ProviderConfig
	if e := ev.Decode(&q); e != nil {
		return e
	}
	if q.Name == "" || q.Type == "" || q.BaseURL == "" {
		return fmt.Errorf("name, type and base_url are required")
	}
	if a.store == nil {
		return fmt.Errorf("configuration store unavailable")
	}
	out, e := a.store.ProviderUpsert(q)
	if e != nil {
		return e
	}
	out.APIKey = ""
	return ev.Encode(out)
}
func (a *App) providerDelete(ev event.Event, _ ws.Meta) error {
	var q struct {
		Name string `json:"name"`
		ID   string `json:"id"`
	}
	if e := ev.Decode(&q); e != nil {
		return e
	}
	if q.Name == "" && q.ID == "" {
		return fmt.Errorf("id or name is required")
	}
	if a.store == nil {
		return fmt.Errorf("configuration store unavailable")
	}
	if q.ID != "" {
		profileID, err := a.activeProfile()
		if err != nil {
			return err
		}
		if err = a.store.ProfileProviderDelete(profileID, q.ID); err != nil {
			return err
		}
		return ev.Encode(map[string]bool{"ok": true})
	}
	if e := a.store.ProviderDelete(q.Name); e != nil {
		return e
	}
	return ev.Encode(map[string]bool{"ok": true})
}
