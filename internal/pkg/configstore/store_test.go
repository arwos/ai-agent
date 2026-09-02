package configstore

import (
	"context"
	"path/filepath"
	"testing"

	"go.osspkg.com/goppy/v3/plugins/orm"
	"go.osspkg.com/goppy/v3/plugins/orm/clients/sqlite"

	"github.com/arwos/ai-agent/internal/pkg/models"
)

func TestPersistence(t *testing.T) {
	database := testORM(t)
	s := New(database)
	if err := s.Set("mode", "dark"); err != nil {
		t.Fatal(err)
	}
	if value, err := s.Get("mode"); err != nil || value != "dark" {
		t.Fatalf("%q %v", value, err)
	}
	mcp, err := s.MCPUpsert(models.MCPConfig{Name: "tools", Type: "http", Endpoint: "http://example", Order: 2})
	if err != nil || mcp.ID == 0 {
		t.Fatalf("%#v %v", mcp, err)
	}
	provider, err := s.ProviderUpsert(models.ProviderConfig{Name: "openai", Type: "openai", BaseURL: "http://example", APIKey: "secret"})
	if err != nil || provider.ID == 0 {
		t.Fatalf("%#v %v", provider, err)
	}
	if got, err := s.Providers(); err != nil || len(got) != 1 || got[0].APIKey != "secret" {
		t.Fatalf("%#v %v", got, err)
	}
}

func TestProfileProviderProxySecretRoundTrip(t *testing.T) {
	s := New(testORM(t))
	_, err := s.ProfileProviderUpsert(models.Provider{
		ID: "provider-1", ProfileID: DefaultProfileID, Name: "proxied", Kind: "openai",
		BaseURL: "http://provider.invalid", Enabled: true,
		ProxyID: "proxy-1", RPM: 24,
	}, "api-secret")
	if err != nil {
		t.Fatal(err)
	}

	public, err := s.ProfileProviders(DefaultProfileID)
	if err != nil {
		t.Fatal(err)
	}
	if len(public) != 1 || public[0].ProxyID != "proxy-1" || public[0].RPM != 24 {
		t.Fatalf("public provider response contains incorrect proxy secret state: %#v", public)
	}
	private, apiKey, err := s.ProfileProviderSecret(DefaultProfileID, "provider-1")
	if err != nil {
		t.Fatal(err)
	}
	if apiKey != "api-secret" || private.ProxyID != "proxy-1" || private.RPM != 24 {
		t.Fatalf("runtime provider secret was not restored: %#v %q", private, apiKey)
	}
}

func TestAgentMainModelsRoundTrip(t *testing.T) {
	s := New(testORM(t))
	input := models.Agent{
		ID: "agent-1", ProfileID: DefaultProfileID, Name: "Agent",
		MainModels:      []string{"first@provider", "second@provider", "first@provider"},
		CompactionLevel: "epic",
	}
	if _, err := s.AgentUpsert(input); err != nil {
		t.Fatal(err)
	}
	agents, err := s.Agents(DefaultProfileID)
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 1 || len(agents[0].MainModels) != 2 || agents[0].MainModels[0] != "first@provider" || agents[0].MainModels[1] != "second@provider" || agents[0].CompactionLevel != "epic" {
		t.Fatalf("unexpected main models: %#v", agents)
	}
}

func TestAgentCompactionLevelDefaultsToBalanced(t *testing.T) {
	s := New(testORM(t))
	if _, err := s.AgentUpsert(models.Agent{ID: "agent-1", ProfileID: DefaultProfileID, Name: "Agent", CompactionLevel: "unknown"}); err != nil {
		t.Fatal(err)
	}
	agents, err := s.Agents(DefaultProfileID)
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 1 || agents[0].CompactionLevel != "balanced" {
		t.Fatalf("unexpected compaction level: %#v", agents)
	}
}

func testORM(t *testing.T) orm.ORM {
	t.Helper()
	database := orm.New(context.Background())
	t.Cleanup(database.Close)
	config := &sqlite.ConfigGroup{Pool: []sqlite.Config{{Tags: databaseTag, File: filepath.Join(t.TempDir(), "agent.db"), Mode: "rwc", Journal: "WAL", LockingMode: "NORMAL", OtherParams: "_busy_timeout=10000"}}}
	if err := database.ApplyConfig(sqlite.Name, config); err != nil {
		t.Fatal(err)
	}
	if err := database.Tag(databaseTag).Tx(context.Background(), "test.schema", func(tx orm.Tx) {
		tx.Exec(func(q orm.Executor) {
			q.SQL(`CREATE TABLE app_settings (key TEXT PRIMARY KEY, value TEXT NOT NULL DEFAULT '')`)
		})
		tx.Exec(func(q orm.Executor) {
			q.SQL(`CREATE TABLE proxies (id TEXT PRIMARY KEY,profile_id TEXT NOT NULL DEFAULT 'default',name TEXT,description TEXT,type TEXT,host TEXT,port INTEGER,username TEXT,password TEXT,insecure_skip_verify INTEGER NOT NULL DEFAULT 0)`)
		})
		tx.Exec(func(q orm.Executor) {
			q.SQL(`CREATE TABLE mcp_configs (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL UNIQUE, type TEXT NOT NULL, endpoint TEXT NOT NULL, prefix TEXT NOT NULL DEFAULT '', access_order INTEGER NOT NULL DEFAULT 0)`)
		})
		tx.Exec(func(q orm.Executor) {
			q.SQL(`CREATE TABLE provider_configs (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL UNIQUE, type TEXT NOT NULL, base_url TEXT NOT NULL, api_key TEXT NOT NULL DEFAULT '', model TEXT NOT NULL DEFAULT '')`)
		})
		tx.Exec(func(q orm.Executor) {
			q.SQL(`CREATE TABLE profiles (id TEXT PRIMARY KEY,name TEXT NOT NULL,role TEXT NOT NULL,accent TEXT NOT NULL,created_at TEXT NOT NULL,temperature REAL NOT NULL DEFAULT 0.7,top_p REAL NOT NULL DEFAULT 0.9)`)
		})
		tx.Exec(func(q orm.Executor) {
			q.SQL(`CREATE TABLE profile_state (profile_id TEXT PRIMARY KEY,active INTEGER NOT NULL)`)
		})
		tx.Exec(func(q orm.Executor) {
			q.SQL(`CREATE TABLE agents (id TEXT PRIMARY KEY,profile_id TEXT,name TEXT,description TEXT,system_prompt TEXT,icon_key TEXT,accent TEXT,compaction_level TEXT NOT NULL DEFAULT 'balanced')`)
		})
		tx.Exec(func(q orm.Executor) {
			q.SQL(`CREATE TABLE agents_links (agent_id TEXT,profile_id TEXT,link_type TEXT,other_id TEXT)`)
		})
		tx.Exec(func(q orm.Executor) {
			q.SQL(`CREATE TABLE profile_providers_model (model_id TEXT PRIMARY KEY,provider_id TEXT,profile_id TEXT,model_name TEXT)`)
		})
		tx.Exec(func(q orm.Executor) {
			q.SQL(`CREATE TABLE presets (id TEXT PRIMARY KEY,profile_id TEXT,title TEXT,text TEXT,agent_id TEXT)`)
		})
		tx.Exec(func(q orm.Executor) {
			q.SQL(`CREATE TABLE profile_providers (id TEXT PRIMARY KEY,profile_id TEXT,name TEXT,kind TEXT,base_url TEXT,api_key TEXT,enabled INTEGER,proxy_id TEXT NOT NULL DEFAULT '',proxy_type TEXT NOT NULL DEFAULT '',proxy_host TEXT NOT NULL DEFAULT '',proxy_port INTEGER NOT NULL DEFAULT 0,proxy_username TEXT NOT NULL DEFAULT '',proxy_password TEXT NOT NULL DEFAULT '',rpm INTEGER NOT NULL DEFAULT 0)`)
		})
		tx.Exec(func(q orm.Executor) {
			q.SQL(`CREATE TABLE profile_mcp_servers (id TEXT PRIMARY KEY,profile_id TEXT,name TEXT,transport TEXT,command TEXT,url TEXT,headers TEXT,prefix TEXT,enabled INTEGER,tools TEXT,instructions TEXT NOT NULL DEFAULT '')`)
		})
		tx.Exec(func(q orm.Executor) {
			q.SQL(`CREATE TABLE conversations (id TEXT PRIMARY KEY,profile_id TEXT,workspace_id TEXT,agent_id TEXT,title TEXT,updated_at TEXT,active_model TEXT NOT NULL DEFAULT '')`)
		})
	}); err != nil {
		t.Fatal(err)
	}
	return database
}
