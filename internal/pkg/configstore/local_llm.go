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

func (s *Store) LocalLLMList(profileID string) ([]models.LocalLLMSettings, error) {
	items := make([]models.LocalLLMSettings, 0)
	err := s.db.Query(context.Background(), "configstore.local_llm.list", func(q orm.Querier) {
		q.SQL(`SELECT id,profile_id,runtime,enabled,binary_path,launch_args,models_path,env,updated_at FROM local_llm_settings WHERE profile_id=? ORDER BY runtime`, profileID)
		q.Bind(func(row orm.Scanner) error {
			var item models.LocalLLMSettings
			var args, env string
			if err := row.Scan(&item.ID, &item.ProfileID, &item.Runtime, &item.Enabled, &item.BinaryPath, &args, &item.ModelsPath, &env, &item.UpdatedAt); err != nil {
				return err
			}
			if err := json.Unmarshal([]byte(args), &item.LaunchArgs); err != nil {
				return err
			}
			if err := json.Unmarshal([]byte(env), &item.Env); err != nil {
				return err
			}
			items = append(items, item)
			return nil
		})
	})
	return items, err
}

func (s *Store) LocalLLMUpsert(item models.LocalLLMSettings) (models.LocalLLMSettings, error) {
	if item.ID == "" {
		item.ID = item.ProfileID + "-" + item.Runtime
	}
	args, err := json.Marshal(item.LaunchArgs)
	if err != nil {
		return item, err
	}
	env, err := json.Marshal(item.Env)
	if err != nil {
		return item, err
	}
	err = s.db.Exec(context.Background(), "configstore.local_llm.upsert", func(q orm.Executor) {
		q.SQL(`INSERT INTO local_llm_settings(id,profile_id,runtime,enabled,binary_path,launch_args,models_path,env,updated_at) VALUES(?,?,?,?,?,?,?, ?,CURRENT_TIMESTAMP) ON CONFLICT(profile_id,runtime) DO UPDATE SET enabled=excluded.enabled,binary_path=excluded.binary_path,launch_args=excluded.launch_args,models_path=excluded.models_path,env=excluded.env,updated_at=CURRENT_TIMESTAMP`, item.ID, item.ProfileID, item.Runtime, item.Enabled, item.BinaryPath, string(args), item.ModelsPath, string(env))
	})
	return item, err
}

func (s *Store) LocalLLMDelete(profileID, runtime string) error {
	return s.db.Exec(context.Background(), "configstore.local_llm.delete", func(q orm.Executor) {
		q.SQL(`DELETE FROM local_llm_settings WHERE profile_id=? AND runtime=?`, profileID, runtime)
	})
}
