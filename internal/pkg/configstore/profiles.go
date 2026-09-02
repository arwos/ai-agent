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

const DefaultProfileID = "default"

func (s *Store) Profiles() ([]models.Profile, string, error) {
	profiles := make([]models.Profile, 0)
	active := DefaultProfileID
	err := s.db.Tx(context.Background(), "configstore.profiles", func(tx orm.Tx) {
		tx.Exec(func(q orm.Executor) {
			q.SQL(`INSERT OR IGNORE INTO profiles(id,name,role,accent,created_at) VALUES(?,?,?,?,?)`, DefaultProfileID, "Default", "", "indigo", time.Now().UTC().Format(time.RFC3339))
		})
		tx.Exec(func(q orm.Executor) {
			q.SQL(`INSERT OR IGNORE INTO profile_state(profile_id,active) VALUES(?,1)`, DefaultProfileID)
		})
		tx.Query(func(q orm.Querier) {
			q.SQL(`SELECT id,name,role,accent,created_at,temperature,top_p FROM profiles ORDER BY name`)
			q.Bind(func(row orm.Scanner) error {
				var p models.Profile
				if err := row.Scan(&p.ID, &p.Name, &p.Role, &p.Accent, &p.CreatedAt, &p.Temperature, &p.TopP); err != nil {
					return err
				}
				profiles = append(profiles, p)
				return nil
			})
		})
		tx.Query(func(q orm.Querier) {
			q.SQL(`SELECT profile_id FROM profile_state WHERE active=1 LIMIT 1`)
			q.Bind(func(row orm.Scanner) error { return row.Scan(&active) })
		})
	})
	return profiles, active, err
}

func (s *Store) ProfileCreate(profile models.Profile) (models.Profile, error) {
	if profile.ID == "" || profile.Name == "" {
		return models.Profile{}, fmt.Errorf("profile id and name are required")
	}
	if profile.Accent == "" {
		profile.Accent = "indigo"
	}
	if profile.Temperature == 0 {
		profile.Temperature = 0.1
	}
	if profile.TopP == 0 {
		profile.TopP = 0.1
	}
	if profile.CreatedAt == "" {
		profile.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	err := s.db.Tx(context.Background(), "configstore.profile.create", func(tx orm.Tx) {
		tx.Exec(func(q orm.Executor) {
			q.SQL(`INSERT INTO profiles(id,name,role,accent,created_at,temperature,top_p) VALUES(?,?,?,?,?,?,?)`, profile.ID, profile.Name, profile.Role, profile.Accent, profile.CreatedAt, profile.Temperature, profile.TopP)
		})
		tx.Exec(func(q orm.Executor) { q.SQL(`INSERT INTO profile_state(profile_id,active) VALUES(?,0)`, profile.ID) })
	})
	return profile, err
}
func (s *Store) ProfileUpdate(profile models.Profile) error {
	return s.db.Exec(context.Background(), "configstore.profile.update", func(q orm.Executor) {
		q.SQL(`UPDATE profiles SET name=?,role=?,accent=?,temperature=?,top_p=? WHERE id=?`, profile.Name, profile.Role, profile.Accent, profile.Temperature, profile.TopP, profile.ID)
	})
}
func (s *Store) ProfileSetActive(id string) error {
	return s.db.Tx(context.Background(), "configstore.profile.active", func(tx orm.Tx) {
		tx.Exec(func(q orm.Executor) { q.SQL(`UPDATE profile_state SET active=0`) })
		tx.Exec(func(q orm.Executor) { q.SQL(`UPDATE profile_state SET active=1 WHERE profile_id=?`, id) })
	})
}
func (s *Store) ProfileDelete(id string) error {
	if id == "" {
		return fmt.Errorf("profile id is required")
	}
	// Not every historical profile-scoped table has a foreign key. Delete all
	// database state explicitly so a profile deletion is independent of the
	// SQLite foreign_keys pragma and leaves no reusable secrets or settings.
	return s.db.Tx(context.Background(), "configstore.profile.delete", func(tx orm.Tx) {
		tx.Exec(func(q orm.Executor) { q.SQL(`DELETE FROM profile_builtin_mcp_settings WHERE profile_id=?`, id) })
		tx.Exec(func(q orm.Executor) { q.SQL(`DELETE FROM proxies WHERE profile_id=?`, id) })
		tx.Exec(func(q orm.Executor) { q.SQL(`DELETE FROM workspaces WHERE profile_id=?`, id) })
		tx.Exec(func(q orm.Executor) { q.SQL(`DELETE FROM conversations WHERE profile_id=?`, id) })
		tx.Exec(func(q orm.Executor) { q.SQL(`DELETE FROM agents WHERE profile_id=?`, id) })
		tx.Exec(func(q orm.Executor) { q.SQL(`DELETE FROM presets WHERE profile_id=?`, id) })
		tx.Exec(func(q orm.Executor) { q.SQL(`DELETE FROM profile_providers WHERE profile_id=?`, id) })
		tx.Exec(func(q orm.Executor) { q.SQL(`DELETE FROM profile_mcp_servers WHERE profile_id=?`, id) })
		tx.Exec(func(q orm.Executor) { q.SQL(`DELETE FROM profile_state WHERE profile_id=?`, id) })
		tx.Exec(func(q orm.Executor) { q.SQL(`DELETE FROM profiles WHERE id=?`, id) })
		// If the deleted profile was active, pick a remaining profile. Do not
		// change the active profile when a non-active one was removed.
		tx.Exec(func(q orm.Executor) {
			q.SQL(`UPDATE profile_state SET active=1 WHERE profile_id=(SELECT id FROM profiles ORDER BY name LIMIT 1) AND NOT EXISTS (SELECT 1 FROM profile_state WHERE active=1)`)
		})
	})
}

// CleanupOrphanProfileData removes rows whose profile_id no longer resolves to
// a profile. It is intentionally idempotent and is used to repair databases
// created before all profile-scoped tables had foreign-key constraints.
func (s *Store) CleanupOrphanProfileData() error {
	return s.db.Tx(context.Background(), "configstore.profile.cleanup", func(tx orm.Tx) {
		for _, table := range []string{
			"profile_builtin_mcp_settings",
			"proxies",
			"workspaces",
			"conversations",
			"agents",
			"presets",
			"profile_providers",
			"profile_mcp_servers",
			"profile_state",
		} {
			tx.Exec(func(q orm.Executor) {
				q.SQL(`DELETE FROM ` + table + ` WHERE profile_id NOT IN (SELECT id FROM profiles)`)
			})
		}
	})
}
