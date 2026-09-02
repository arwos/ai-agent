/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package configstore

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"

	"go.osspkg.com/goppy/v3/plugins/orm"

	"github.com/arwos/ai-agent/internal/pkg/models"
)

func (s *Store) ProfileProviders(profileID string) ([]models.Provider, error) {
	out := make([]models.Provider, 0)
	e := s.db.Query(context.Background(), "configstore.profile.providers", func(q orm.Querier) {
		q.SQL(`SELECT p.id,p.profile_id,p.name,p.kind,p.base_url,p.api_key,COALESCE((SELECT json_group_array(model_name) FROM profile_providers_model m WHERE m.provider_id=p.id AND m.profile_id=p.profile_id),'[]'),p.enabled,p.proxy_id,p.proxy_type,p.proxy_host,p.proxy_port,p.proxy_username,p.proxy_password,p.rpm FROM profile_providers p WHERE p.profile_id=? ORDER BY p.name`, profileID)
		q.Bind(func(r orm.Scanner) error {
			var x models.Provider
			var key, mods, proxyID, proxyType, proxyHost, proxyUser, proxyPassword string
			var proxyPort int
			var enabled int
			if e := r.Scan(&x.ID, &x.ProfileID, &x.Name, &x.Kind, &x.BaseURL, &key, &mods, &enabled, &proxyID, &proxyType, &proxyHost, &proxyPort, &proxyUser, &proxyPassword, &x.RPM); e != nil {
				return e
			}
			x.HasAPIKey = key != ""
			x.ProxyID = proxyID
			x.Enabled = enabled != 0
			_ = json.Unmarshal([]byte(mods), &x.Models)
			out = append(out, x)
			return nil
		})
	})
	return out, e
}
func (s *Store) ProfileProviderUpsert(x models.Provider, apiKey string) (models.Provider, error) {
	if x.ID == "" || x.ProfileID == "" || x.Name == "" || x.Kind == "" || x.BaseURL == "" {
		return x, fmt.Errorf("provider id, profile id, name, kind and base URL are required")
	}
	e := s.db.Tx(context.Background(), "configstore.profile.provider.upsert", func(tx orm.Tx) {
		tx.Exec(func(q orm.Executor) {
			q.SQL(`INSERT INTO profile_providers(id,profile_id,name,kind,base_url,api_key,enabled,proxy_id,rpm) VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name,kind=excluded.kind,base_url=excluded.base_url,api_key=CASE WHEN excluded.api_key='' THEN profile_providers.api_key ELSE excluded.api_key END,enabled=excluded.enabled,proxy_id=excluded.proxy_id,rpm=excluded.rpm`, x.ID, x.ProfileID, x.Name, x.Kind, x.BaseURL, apiKey, x.Enabled, x.ProxyID, x.RPM)
		})
		tx.Exec(func(q orm.Executor) {
			q.SQL(`DELETE FROM profile_providers_model WHERE provider_id=? AND profile_id=?`, x.ID, x.ProfileID)
		})
		for _, model := range x.Models {
			id := providerModelID(x.ID, x.ProfileID, model)
			tx.Exec(func(q orm.Executor) {
				q.SQL(`INSERT INTO profile_providers_model(model_id,provider_id,profile_id,model_name) VALUES(?,?,?,?)`, id, x.ID, x.ProfileID, model)
			})
		}
	})
	x.HasAPIKey = apiKey != "" || x.HasAPIKey
	return x, e
}

func providerModelID(providerID, profileID, modelName string) string {
	sum := sha256.Sum256([]byte(providerID + "\x00" + profileID + "\x00" + modelName))
	return "model-" + fmt.Sprintf("%x", sum[:12])
}
func (s *Store) ProfileProviderDelete(profileID, id string) error {
	return s.db.Exec(context.Background(), "configstore.profile.provider.delete", func(q orm.Executor) {
		q.SQL(`DELETE FROM profile_providers WHERE profile_id=? AND id=?`, profileID, id)
	})
}
func (s *Store) ProfileProviderClearAPIKey(profileID, id string) error {
	return s.db.Exec(context.Background(), "configstore.profile.provider.clear-api-key", func(q orm.Executor) {
		q.SQL(`UPDATE profile_providers SET api_key='' WHERE profile_id=? AND id=?`, profileID, id)
	})
}
func (s *Store) ProfileProviderSecret(profileID, idOrName string) (models.Provider, string, error) {
	var x models.Provider
	var key, mods string
	var enabled int
	err := s.db.Query(context.Background(), "configstore.profile.provider.secret", func(q orm.Querier) {
		q.SQL(`SELECT p.id,p.profile_id,p.name,p.kind,p.base_url,p.api_key,COALESCE((SELECT json_group_array(model_name) FROM profile_providers_model m WHERE m.provider_id=p.id AND m.profile_id=p.profile_id),'[]'),p.enabled,p.proxy_id,COALESCE(x.type,p.proxy_type),COALESCE(x.host,p.proxy_host),COALESCE(x.port,p.proxy_port),COALESCE(x.username,p.proxy_username),COALESCE(x.password,p.proxy_password),p.rpm FROM profile_providers p LEFT JOIN proxies x ON x.id=p.proxy_id AND x.profile_id=p.profile_id WHERE p.profile_id=? AND (p.id=? OR p.name=?) LIMIT 1`, profileID, idOrName, idOrName)
		q.Bind(func(r orm.Scanner) error {
			var proxyID, proxyType, proxyHost, proxyUser, proxyPassword string
			var proxyPort int
			if err := r.Scan(&x.ID, &x.ProfileID, &x.Name, &x.Kind, &x.BaseURL, &key, &mods, &enabled, &proxyID, &proxyType, &proxyHost, &proxyPort, &proxyUser, &proxyPassword, &x.RPM); err != nil {
				return err
			}
			x.ProxyID = proxyID
			return nil
		})
	})
	if err == nil {
		_ = json.Unmarshal([]byte(mods), &x.Models)
		x.Enabled = enabled != 0
		x.HasAPIKey = key != ""
	}
	return x, key, err
}
func (s *Store) DefaultProfileProvider(profileID string) (models.Provider, string, error) {
	items, err := s.ProfileProviders(profileID)
	if err != nil {
		return models.Provider{}, "", err
	}
	for _, item := range items {
		if item.Enabled {
			return s.ProfileProviderSecret(profileID, item.ID)
		}
	}
	return models.Provider{}, "", fmt.Errorf("no enabled provider")
}

func (s *Store) ProfileMCP(profileID string) ([]models.MCPServer, error) {
	out := make([]models.MCPServer, 0)
	e := s.db.Query(context.Background(), "configstore.profile.mcp", func(q orm.Querier) {
		q.SQL(`SELECT id,profile_id,name,transport,command,url,headers,prefix,enabled,tools,instructions FROM profile_mcp_servers WHERE profile_id=? ORDER BY name`, profileID)
		q.Bind(func(r orm.Scanner) error {
			var x models.MCPServer
			var headers, tools string
			var enabled int
			if e := r.Scan(&x.ID, &x.ProfileID, &x.Name, &x.Transport, &x.Command, &x.URL, &headers, &x.Prefix, &enabled, &tools, &x.Instructions); e != nil {
				return e
			}
			_ = json.Unmarshal([]byte(headers), &x.Headers)
			_ = json.Unmarshal([]byte(tools), &x.Tools)
			x.Enabled = enabled != 0
			for i := range x.Headers {
				x.Headers[i].V = ""
			}
			out = append(out, x)
			return nil
		})
	})
	return out, e
}
func (s *Store) ProfileMCPUpsert(x models.MCPServer) (models.MCPServer, error) {
	if x.ID == "" || x.ProfileID == "" || x.Name == "" || x.Transport == "" {
		return x, fmt.Errorf("MCP id, profile id, name and transport are required")
	}
	headers, _ := json.Marshal(x.Headers)
	tools, _ := json.Marshal(x.Tools)
	e := s.db.Exec(context.Background(), "configstore.profile.mcp.upsert", func(q orm.Executor) {
		q.SQL(`INSERT INTO profile_mcp_servers(id,profile_id,name,transport,command,url,headers,prefix,enabled,tools,instructions) VALUES(?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name,transport=excluded.transport,command=excluded.command,url=excluded.url,headers=excluded.headers,prefix=excluded.prefix,enabled=excluded.enabled,tools=excluded.tools,instructions=excluded.instructions`, x.ID, x.ProfileID, x.Name, x.Transport, x.Command, x.URL, string(headers), x.Prefix, x.Enabled, string(tools), x.Instructions)
	})
	for i := range x.Headers {
		x.Headers[i].V = ""
	}
	return x, e
}
func (s *Store) ProfileMCPDelete(profileID, id string) error {
	return s.db.Exec(context.Background(), "configstore.profile.mcp.delete", func(q orm.Executor) {
		q.SQL(`DELETE FROM profile_mcp_servers WHERE profile_id=? AND id=?`, profileID, id)
	})
}
