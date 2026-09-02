/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package proxy

import (
	"context"
	"fmt"

	"go.osspkg.com/goppy/v3/plugins/orm"

	"github.com/arwos/ai-agent/internal/pkg/models"
)

type Store struct{ db orm.Stmt }

func New(database orm.ORM) *Store { return &Store{db: database.Tag("master")} }
func (s *Store) List(profileID string) ([]models.Proxy, error) {
	out := []models.Proxy{}
	err := s.db.Query(context.Background(), "proxy.list", func(q orm.Querier) {
		q.SQL(`SELECT id,name,description,type,host,port,username,password,insecure_skip_verify FROM proxies WHERE profile_id=? ORDER BY name`, profileID)
		q.Bind(func(r orm.Scanner) error {
			var p models.Proxy
			var password string
			var insecure int
			if err := r.Scan(&p.ID, &p.Name, &p.Description, &p.Type, &p.Host, &p.Port, &p.Username, &password, &insecure); err != nil {
				return err
			}
			p.HasPassword = password != ""
			p.InsecureSkipVerify = insecure != 0
			out = append(out, p)
			return nil
		})
	})
	return out, err
}
func (s *Store) Upsert(profileID string, p models.Proxy) (models.Proxy, error) {
	if p.ID == "" || p.Name == "" || p.Host == "" {
		return p, fmt.Errorf("proxy name and host are required")
	}
	if p.Type != "http" && p.Type != "https" && p.Type != "socks5" {
		return p, fmt.Errorf("unsupported proxy type %q", p.Type)
	}
	if p.Port < 1 || p.Port > 65535 {
		return p, fmt.Errorf("proxy port must be between 1 and 65535")
	}
	// Names are the stable identity for a proxy inside one profile. Reuse the
	// existing row ID so imports update it instead of violating the name key.
	var existingID string
	_ = s.db.Query(context.Background(), "proxy.find_by_name", func(q orm.Querier) {
		q.SQL(`SELECT id FROM proxies WHERE profile_id=? AND name=? LIMIT 1`, profileID, p.Name)
		q.Bind(func(r orm.Scanner) error { return r.Scan(&existingID) })
	})
	if existingID != "" {
		p.ID = existingID
	}
	err := s.db.Exec(context.Background(), "proxy.upsert", func(q orm.Executor) {
		q.SQL(`INSERT INTO proxies(id,profile_id,name,description,type,host,port,username,password,insecure_skip_verify) VALUES(?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name,description=excluded.description,type=excluded.type,host=excluded.host,port=excluded.port,username=excluded.username,password=CASE WHEN excluded.password='' THEN proxies.password ELSE excluded.password END,insecure_skip_verify=excluded.insecure_skip_verify WHERE proxies.profile_id=excluded.profile_id`, p.ID, profileID, p.Name, p.Description, p.Type, p.Host, p.Port, p.Username, p.Password, p.InsecureSkipVerify)
	})
	p.HasPassword = p.HasPassword || p.Password != ""
	p.Password = ""
	return p, err
}
func (s *Store) Delete(profileID, id string) error {
	return s.db.Exec(context.Background(), "proxy.delete", func(q orm.Executor) { q.SQL(`DELETE FROM proxies WHERE profile_id=? AND id=?`, profileID, id) })
}
func (s *Store) ResetPassword(profileID, id string) error {
	if id == "" {
		return fmt.Errorf("proxy id is required")
	}
	return s.db.Exec(context.Background(), "proxy.reset_password", func(q orm.Executor) {
		q.SQL(`UPDATE proxies SET password='' WHERE profile_id=? AND id=?`, profileID, id)
	})
}
func (s *Store) Secret(profileID, id string) (models.Proxy, error) {
	var p models.Proxy
	err := s.db.Query(context.Background(), "proxy.secret", func(q orm.Querier) {
		q.SQL(`SELECT id,name,description,type,host,port,username,password,insecure_skip_verify FROM proxies WHERE profile_id=? AND id=?`, profileID, id)
		q.Bind(func(r orm.Scanner) error {
			var insecure int
			err := r.Scan(&p.ID, &p.Name, &p.Description, &p.Type, &p.Host, &p.Port, &p.Username, &p.Password, &insecure)
			p.InsecureSkipVerify = insecure != 0
			return err
		})
	})
	return p, err
}
