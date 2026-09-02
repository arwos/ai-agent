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

func (s *Store) Presets(profileID string) ([]models.Preset, error) {
	out := make([]models.Preset, 0)
	err := s.db.Query(context.Background(), "configstore.presets", func(q orm.Querier) {
		q.SQL(`SELECT id,profile_id,title,text,agent_id FROM presets WHERE profile_id=? ORDER BY title`, profileID)
		q.Bind(func(row orm.Scanner) error {
			var x models.Preset
			if e := row.Scan(&x.ID, &x.ProfileID, &x.Title, &x.Text, &x.AgentID); e != nil {
				return e
			}
			out = append(out, x)
			return nil
		})
	})
	return out, err
}
func (s *Store) PresetUpsert(x models.Preset) (models.Preset, error) {
	if x.ID == "" || x.ProfileID == "" || x.Title == "" {
		return x, fmt.Errorf("preset id, profile id and title are required")
	}
	e := s.db.Exec(context.Background(), "configstore.preset.upsert", func(q orm.Executor) {
		q.SQL(`INSERT INTO presets(id,profile_id,title,text,agent_id) VALUES(?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET title=excluded.title,text=excluded.text,agent_id=excluded.agent_id`, x.ID, x.ProfileID, x.Title, x.Text, x.AgentID)
	})
	return x, e
}
func (s *Store) PresetDelete(profileID, id string) error {
	return s.db.Exec(context.Background(), "configstore.preset.delete", func(q orm.Executor) { q.SQL(`DELETE FROM presets WHERE profile_id=? AND id=?`, profileID, id) })
}
