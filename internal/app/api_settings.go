/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package app

import (
	"encoding/json"
	"fmt"

	"go.osspkg.com/goppy/v3/plugins/ws"
	"go.osspkg.com/goppy/v3/plugins/ws/event"
	"go.osspkg.com/logx"

	"github.com/arwos/ai-agent/internal/pkg/dialog"
	"github.com/arwos/ai-agent/internal/pkg/llm"
	"github.com/arwos/ai-agent/internal/pkg/models"
	settingspkg "github.com/arwos/ai-agent/internal/pkg/settings"
)

func (a *App) settingsExport(ev event.Event, _ ws.Meta) error { //nolint:gocyclo // settings serialization dispatch
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
func (a *App) settingsImport(ev event.Event, _ ws.Meta) error { //nolint:gocyclo // settings deserialization dispatch
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
func (a *App) settingsCleanup(ev event.Event, _ ws.Meta) error {
	profiles, _, err := a.store.Profiles()
	if err != nil {
		return err
	}
	validProfiles := make(map[string]struct{}, len(profiles))
	validDialogs := make(map[string]map[string]struct{}, len(profiles))
	for _, profile := range profiles {
		validProfiles[profile.ID] = struct{}{}
		conversations, listErr := a.dialogs.ProfileConversations(profile.ID)
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
