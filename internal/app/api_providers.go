/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package app

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"go.osspkg.com/goppy/v3/plugins/ws"
	"go.osspkg.com/goppy/v3/plugins/ws/event"

	"github.com/arwos/ai-agent/internal/pkg/llm"
	"github.com/arwos/ai-agent/internal/pkg/models"
)

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
