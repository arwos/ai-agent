/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package configstore

import (
	"context"
	"encoding/json"

	"go.osspkg.com/goppy/v3/plugins/orm"

	"github.com/arwos/ai-agent/internal/pkg/models"
)

func (s *Store) BuiltinMCPSettings(profileID string) ([]models.BuiltinMCPSettings, error) {
	out := make([]models.BuiltinMCPSettings, 0)
	err := s.db.Query(context.Background(), "configstore.builtin_mcp.list", func(q orm.Querier) {
		q.SQL(`SELECT profile_id,builtin_key,enabled,tools FROM profile_builtin_mcp_settings WHERE profile_id=?`, profileID)
		q.Bind(func(row orm.Scanner) error {
			var item models.BuiltinMCPSettings
			var enabled int
			var tools string
			if err := row.Scan(&item.ProfileID, &item.BuiltinKey, &enabled, &tools); err != nil {
				return err
			}
			item.Enabled = enabled != 0
			_ = json.Unmarshal([]byte(tools), &item.Tools)
			out = append(out, item)
			return nil
		})
	})
	return out, err
}
func (s *Store) BuiltinMCPUpsert(item models.BuiltinMCPSettings) error {
	tools, _ := json.Marshal(item.Tools)
	return s.db.Exec(context.Background(), "configstore.builtin_mcp.upsert", func(q orm.Executor) {
		q.SQL(`INSERT INTO profile_builtin_mcp_settings(profile_id,builtin_key,enabled,tools) VALUES(?,?,?,?) ON CONFLICT(profile_id,builtin_key) DO UPDATE SET enabled=excluded.enabled,tools=excluded.tools`, item.ProfileID, item.BuiltinKey, item.Enabled, string(tools))
	})
}
