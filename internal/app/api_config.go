/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package app

import (
	"fmt"

	"go.osspkg.com/goppy/v3/plugins/ws"
	"go.osspkg.com/goppy/v3/plugins/ws/event"
)

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
