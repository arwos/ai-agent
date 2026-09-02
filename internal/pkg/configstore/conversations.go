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

func (s *Store) Conversations(profileID, workspaceID string) ([]models.Conversation, error) {
	out := make([]models.Conversation, 0)
	e := s.db.Query(context.Background(), "configstore.conversations", func(q orm.Querier) {
		q.SQL(`SELECT id,profile_id,workspace_id,agent_id,title,updated_at,active_model FROM conversations WHERE profile_id=? AND workspace_id=? ORDER BY updated_at DESC`, profileID, workspaceID)
		q.Bind(func(r orm.Scanner) error {
			var x models.Conversation
			if e := r.Scan(&x.ID, &x.ProfileID, &x.WorkspaceID, &x.AgentID, &x.Title, &x.UpdatedAt, &x.ActiveModel); e != nil {
				return e
			}
			out = append(out, x)
			return nil
		})
	})
	return out, e
}

// ProfileConversations returns all conversations for lifecycle cleanup such as
// deleting a profile. Normal UI queries should continue using Conversations.
func (s *Store) ProfileConversations(profileID string) ([]models.Conversation, error) {
	out := make([]models.Conversation, 0)
	e := s.db.Query(context.Background(), "configstore.profile.conversations", func(q orm.Querier) {
		q.SQL(`SELECT id,profile_id,workspace_id,agent_id,title,updated_at,active_model FROM conversations WHERE profile_id=?`, profileID)
		q.Bind(func(r orm.Scanner) error {
			var x models.Conversation
			if e := r.Scan(&x.ID, &x.ProfileID, &x.WorkspaceID, &x.AgentID, &x.Title, &x.UpdatedAt, &x.ActiveModel); e != nil {
				return e
			}
			out = append(out, x)
			return nil
		})
	})
	return out, e
}

func (s *Store) Conversation(profileID, id string) (models.Conversation, error) {
	var out models.Conversation
	err := s.db.Query(context.Background(), "configstore.conversation", func(q orm.Querier) {
		q.SQL(`SELECT id,profile_id,workspace_id,agent_id,title,updated_at,active_model FROM conversations WHERE profile_id=? AND id=?`, profileID, id)
		q.Bind(func(r orm.Scanner) error {
			return r.Scan(&out.ID, &out.ProfileID, &out.WorkspaceID, &out.AgentID, &out.Title, &out.UpdatedAt, &out.ActiveModel)
		})
	})
	if err == nil && out.ID == "" {
		err = fmt.Errorf("conversation %q not found", id)
	}
	return out, err
}
func (s *Store) ConversationUpsert(x models.Conversation) (models.Conversation, error) {
	if x.ID == "" || x.ProfileID == "" || x.WorkspaceID == "" {
		return x, fmt.Errorf("conversation id, profile id and workspace id are required")
	}
	if x.Title == "" {
		x.Title = "New conversation"
	}
	x.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	e := s.db.Exec(context.Background(), "configstore.conversation.upsert", func(q orm.Executor) {
		q.SQL(`INSERT INTO conversations(id,profile_id,workspace_id,agent_id,title,updated_at,active_model) VALUES(?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET agent_id=excluded.agent_id,title=excluded.title,updated_at=excluded.updated_at,active_model=CASE WHEN excluded.active_model='' THEN conversations.active_model ELSE excluded.active_model END`, x.ID, x.ProfileID, x.WorkspaceID, x.AgentID, x.Title, x.UpdatedAt, x.ActiveModel)
	})
	return x, e
}

func (s *Store) ConversationSetActiveModel(profileID, id, model string) error {
	return s.db.Exec(context.Background(), "configstore.conversation.set_active_model", func(q orm.Executor) {
		q.SQL(`UPDATE conversations SET active_model=?,updated_at=? WHERE profile_id=? AND id=?`, model, time.Now().UTC().Format(time.RFC3339), profileID, id)
	})
}
func (s *Store) ConversationDelete(profileID, id string) error {
	return s.db.Exec(context.Background(), "configstore.conversation.delete", func(q orm.Executor) { q.SQL(`DELETE FROM conversations WHERE profile_id=? AND id=?`, profileID, id) })
}
