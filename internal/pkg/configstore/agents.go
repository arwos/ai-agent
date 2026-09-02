/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package configstore

import (
	"context"
	"fmt"

	"go.osspkg.com/goppy/v3/plugins/orm"

	"github.com/arwos/ai-agent/internal/pkg/models"
)

func (s *Store) Agents(profileID string) ([]models.Agent, error) {
	out := make([]models.Agent, 0)
	err := s.db.Query(context.Background(), "configstore.agents", func(q orm.Querier) {
		q.SQL(`SELECT id,profile_id,name,description,system_prompt,icon_key,accent,compaction_level FROM agents WHERE profile_id=? ORDER BY name`, profileID)
		q.Bind(func(row orm.Scanner) error {
			var a models.Agent
			if e := row.Scan(&a.ID, &a.ProfileID, &a.Name, &a.Description, &a.SystemPrompt, &a.IconKey, &a.Accent, &a.CompactionLevel); e != nil {
				return e
			}
			out = append(out, a)
			return nil
		})
	})
	if err != nil {
		return out, err
	}
	byID := make(map[string]*models.Agent, len(out))
	for i := range out {
		byID[out[i].ID] = &out[i]
	}
	err = s.db.Query(context.Background(), "configstore.agent.links", func(q orm.Querier) {
		q.SQL(`SELECT agent_id,link_type,other_id FROM agents_links WHERE profile_id=? ORDER BY agent_id,link_type,other_id`, profileID)
		q.Bind(func(row orm.Scanner) error {
			var id, kind, other string
			if e := row.Scan(&id, &kind, &other); e != nil {
				return e
			}
			a := byID[id]
			if a == nil {
				return nil
			}
			switch kind {
			case "mainModel":
				a.MainModels = append(a.MainModels, other)
			case "memModel":
				a.MemoryModel = other
			case "toolModel":
				a.ToolModel = other
			case "compactionModel":
				a.CompactionModel = other
			case "skillGroupId":
				a.SkillGroupIDs = append(a.SkillGroupIDs, other)
			case "mcpId":
				a.MCPIDs = append(a.MCPIDs, other)
			}
			return nil
		})
	})
	return out, err
}
func (s *Store) AgentUpsert(a models.Agent) (models.Agent, error) {
	if a.ID == "" || a.ProfileID == "" || a.Name == "" {
		return a, fmt.Errorf("agent id, profile id and name are required")
	}
	a.CompactionLevel = compactionLevel(a.CompactionLevel)
	err := s.db.Tx(context.Background(), "configstore.agent.upsert", func(tx orm.Tx) {
		tx.Exec(func(q orm.Executor) {
			q.SQL(`INSERT INTO agents(id,profile_id,name,description,system_prompt,icon_key,accent,compaction_level) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name,description=excluded.description,system_prompt=excluded.system_prompt,icon_key=excluded.icon_key,accent=excluded.accent,compaction_level=excluded.compaction_level`, a.ID, a.ProfileID, a.Name, a.Description, a.SystemPrompt, a.IconKey, a.Accent, a.CompactionLevel)
		})
		tx.Exec(func(q orm.Executor) {
			q.SQL(`DELETE FROM agents_links WHERE agent_id=? AND profile_id=?`, a.ID, a.ProfileID)
		})
		add := func(kind, id string) {
			if id != "" {
				tx.Exec(func(q orm.Executor) {
					q.SQL(`INSERT INTO agents_links(agent_id,profile_id,link_type,other_id) VALUES(?,?,?,?)`, a.ID, a.ProfileID, kind, id)
				})
			}
		}
		for _, id := range uniqueModels(a.MainModels) {
			add("mainModel", id)
		}
		add("toolModel", a.ToolModel)
		add("compactionModel", a.CompactionModel)
		add("memModel", a.MemoryModel)
		for _, id := range uniqueModels(a.SkillGroupIDs) {
			add("skillGroupId", id)
		}
		for _, id := range uniqueModels(a.MCPIDs) {
			add("mcpId", id)
		}
	})
	return a, err
}

func compactionLevel(value string) string {
	switch value {
	case "brief", "balanced", "detailed", "comprehensive", "epic":
		return value
	default:
		return "balanced"
	}
}

func uniqueModels(items []string) []string {
	out := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		if item == "" {
			continue
		}
		if _, exists := seen[item]; exists {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

// RemoveProviderModels detaches models that are no longer available from all
// agents in the profile. Empty special-model fields mean "use the main model".
func (s *Store) RemoveProviderModels(profileID string, removed map[string]struct{}) error {
	if len(removed) == 0 {
		return nil
	}
	agents, err := s.Agents(profileID)
	if err != nil {
		return err
	}
	for _, agent := range agents {
		changed := false
		main := agent.MainModels[:0]
		for _, model := range agent.MainModels {
			if _, drop := removed[model]; drop {
				changed = true
				continue
			}
			main = append(main, model)
		}
		agent.MainModels = main
		specialModels := []*string{&agent.ToolModel, &agent.CompactionModel, &agent.MemoryModel}
		for _, field := range specialModels {
			if _, drop := removed[*field]; drop {
				*field = ""
				changed = true
			}
		}
		if changed {
			if _, err := s.AgentUpsert(agent); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Store) AgentDelete(profileID, id string) error {
	return s.db.Exec(context.Background(), "configstore.agent.delete", func(q orm.Executor) { q.SQL(`DELETE FROM agents WHERE profile_id=? AND id=?`, profileID, id) })
}
