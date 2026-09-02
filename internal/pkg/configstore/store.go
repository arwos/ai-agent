/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package configstore

import (
	"context"
	"database/sql"

	"go.osspkg.com/goppy/v3/plugins/orm"

	"github.com/arwos/ai-agent/internal/pkg/models"
)

const databaseTag = "master"

// Store persists application configuration through goppy's configured ORM.
// Database lifecycle and schema migration are owned by the ORM plugins.
type Store struct{ db orm.Stmt }

func New(database orm.ORM) *Store { return &Store{db: database.Tag(databaseTag)} }

func (s *Store) Get(key string) (string, error) {
	var value string
	err := s.db.Query(context.Background(), "configstore.get", func(q orm.Querier) {
		q.SQL(`SELECT value FROM app_settings WHERE key=?`, key)
		q.Bind(func(row orm.Scanner) error { return row.Scan(&value) })
	})
	return value, err
}

func (s *Store) Set(key, value string) error {
	return s.db.Exec(context.Background(), "configstore.set", func(q orm.Executor) {
		q.SQL(`INSERT INTO app_settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	})
}

func (s *Store) Settings() ([]models.AppSettings, error) {
	result := make([]models.AppSettings, 0)
	err := s.db.Query(context.Background(), "configstore.settings", func(q orm.Querier) {
		q.SQL(`SELECT key,value FROM app_settings ORDER BY key`)
		q.Bind(func(row orm.Scanner) error {
			var item models.AppSettings
			if err := row.Scan(&item.Key, &item.Value); err != nil {
				return err
			}
			result = append(result, item)
			return nil
		})
	})
	return result, err
}

func (s *Store) MCPList() ([]models.MCPConfig, error) {
	result := make([]models.MCPConfig, 0)
	err := s.db.Query(context.Background(), "configstore.mcp.list", func(q orm.Querier) {
		q.SQL(`SELECT id,name,type,endpoint,prefix,access_order FROM mcp_configs ORDER BY access_order,name`)
		q.Bind(func(row orm.Scanner) error {
			var item models.MCPConfig
			if err := row.Scan(&item.ID, &item.Name, &item.Type, &item.Endpoint, &item.Prefix, &item.Order); err != nil {
				return err
			}
			result = append(result, item)
			return nil
		})
	})
	return result, err
}

func (s *Store) MCPUpsert(item models.MCPConfig) (models.MCPConfig, error) {
	err := s.db.Tx(context.Background(), "configstore.mcp.upsert", func(tx orm.Tx) {
		tx.Exec(func(q orm.Executor) {
			q.SQL(`INSERT INTO mcp_configs(name,type,endpoint,prefix,access_order) VALUES(?,?,?,?,?) ON CONFLICT(name) DO UPDATE SET type=excluded.type,endpoint=excluded.endpoint,prefix=excluded.prefix,access_order=excluded.access_order`, item.Name, item.Type, item.Endpoint, item.Prefix, item.Order)
		})
		tx.Query(func(q orm.Querier) {
			q.SQL(`SELECT id FROM mcp_configs WHERE name=?`, item.Name)
			q.Bind(func(row orm.Scanner) error { return row.Scan(&item.ID) })
		})
	})
	if err == nil && item.ID == 0 {
		err = sql.ErrNoRows
	}
	return item, err
}

func (s *Store) MCPDelete(name string) error {
	return s.db.Exec(context.Background(), "configstore.mcp.delete", func(q orm.Executor) { q.SQL(`DELETE FROM mcp_configs WHERE name=?`, name) })
}

func (s *Store) Providers() ([]models.ProviderConfig, error) {
	result := make([]models.ProviderConfig, 0)
	err := s.db.Query(context.Background(), "configstore.providers", func(q orm.Querier) {
		q.SQL(`SELECT id,name,type,base_url,api_key,model FROM provider_configs ORDER BY name`)
		q.Bind(func(row orm.Scanner) error {
			var item models.ProviderConfig
			if err := row.Scan(&item.ID, &item.Name, &item.Type, &item.BaseURL, &item.APIKey, &item.Model); err != nil {
				return err
			}
			result = append(result, item)
			return nil
		})
	})
	return result, err
}

func (s *Store) ProviderUpsert(item models.ProviderConfig) (models.ProviderConfig, error) {
	err := s.db.Tx(context.Background(), "configstore.provider.upsert", func(tx orm.Tx) {
		tx.Exec(func(q orm.Executor) {
			q.SQL(`INSERT INTO provider_configs(name,type,base_url,api_key,model) VALUES(?,?,?,?,?) ON CONFLICT(name) DO UPDATE SET type=excluded.type,base_url=excluded.base_url,api_key=excluded.api_key,model=excluded.model`, item.Name, item.Type, item.BaseURL, item.APIKey, item.Model)
		})
		tx.Query(func(q orm.Querier) {
			q.SQL(`SELECT id FROM provider_configs WHERE name=?`, item.Name)
			q.Bind(func(row orm.Scanner) error { return row.Scan(&item.ID) })
		})
	})
	if err == nil && item.ID == 0 {
		err = sql.ErrNoRows
	}
	return item, err
}

func (s *Store) ProviderDelete(name string) error {
	return s.db.Exec(context.Background(), "configstore.provider.delete", func(q orm.Executor) { q.SQL(`DELETE FROM provider_configs WHERE name=?`, name) })
}

func (s *Store) Provider(name string) (models.ProviderConfig, error) {
	var item models.ProviderConfig
	err := s.db.Query(context.Background(), "configstore.provider", func(q orm.Querier) {
		q.SQL(`SELECT id,name,type,base_url,api_key,model FROM provider_configs WHERE name=?`, name)
		q.Bind(func(row orm.Scanner) error {
			return row.Scan(&item.ID, &item.Name, &item.Type, &item.BaseURL, &item.APIKey, &item.Model)
		})
	})
	if err == nil && item.ID == 0 {
		err = sql.ErrNoRows
	}
	return item, err
}
