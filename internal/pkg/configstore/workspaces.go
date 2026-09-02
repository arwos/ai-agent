/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package configstore

import (
	"context"
	"fmt"
	"time"

	"go.osspkg.com/goppy/v3/plugins/orm"

	"github.com/arwos/ai-agent/internal/pkg/models"
)

func (s *Store) Workspaces(profileID string) ([]models.Workspace, error) {
	out := []models.Workspace{}
	err := s.db.Query(context.Background(), "configstore.workspaces", func(q orm.Querier) {
		q.SQL(`SELECT id,name,folder_path,opened_at FROM workspaces WHERE profile_id=? ORDER BY name`, profileID)
		q.Bind(func(r orm.Scanner) error {
			var x models.Workspace
			if err := r.Scan(&x.ID, &x.Name, &x.FolderPath, &x.OpenedAt); err != nil {
				return err
			}
			out = append(out, x)
			return nil
		})
	})
	return out, err
}
func (s *Store) WorkspaceUpsert(profileID string, x models.Workspace) error {
	if x.ID == "" || x.Name == "" || x.FolderPath == "" {
		return fmt.Errorf("workspace id, name and folder path are required")
	}
	if x.OpenedAt == "" {
		x.OpenedAt = time.Now().UTC().Format(time.RFC3339)
	}
	return s.db.Exec(context.Background(), "configstore.workspace.upsert", func(q orm.Executor) {
		q.SQL(`INSERT INTO workspaces(id,profile_id,name,folder_path,opened_at) VALUES(?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name,folder_path=excluded.folder_path,opened_at=excluded.opened_at WHERE workspaces.profile_id=excluded.profile_id`, x.ID, profileID, x.Name, x.FolderPath, x.OpenedAt)
	})
}
func (s *Store) WorkspaceConversations(workspaceID string) ([]models.Conversation, error) {
	out := make([]models.Conversation, 0)
	err := s.db.Query(context.Background(), "configstore.workspace.conversations", func(q orm.Querier) {
		q.SQL(`SELECT id,profile_id,workspace_id,agent_id,title,updated_at FROM conversations WHERE workspace_id=?`, workspaceID)
		q.Bind(func(r orm.Scanner) error {
			var x models.Conversation
			if err := r.Scan(&x.ID, &x.ProfileID, &x.WorkspaceID, &x.AgentID, &x.Title, &x.UpdatedAt); err != nil {
				return err
			}
			out = append(out, x)
			return nil
		})
	})
	return out, err
}

func (s *Store) WorkspaceDelete(profileID, id string) error {
	return s.db.Tx(context.Background(), "configstore.workspace.delete", func(tx orm.Tx) {
		tx.Exec(func(q orm.Executor) {
			q.SQL(`DELETE FROM conversations WHERE profile_id=? AND workspace_id=?`, profileID, id)
		})
		tx.Exec(func(q orm.Executor) { q.SQL(`DELETE FROM workspaces WHERE profile_id=? AND id=?`, profileID, id) })
	})
}
