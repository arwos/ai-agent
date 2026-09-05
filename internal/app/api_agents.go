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

	"github.com/arwos/ai-agent/internal/pkg/models"
)

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
