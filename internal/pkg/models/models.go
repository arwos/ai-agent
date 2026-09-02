/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package models

type AppSettings struct {
	Key   string `db:"key" json:"key"`
	Value string `db:"value" json:"value"`
}
type LocalLLMSettings struct {
	ID         string            `json:"id"`
	ProfileID  string            `json:"profileId"`
	Runtime    string            `json:"runtime"`
	Enabled    bool              `json:"enabled"`
	BinaryPath string            `json:"binaryPath"`
	LaunchArgs []string          `json:"launchArgs"`
	ModelsPath string            `json:"modelsPath"`
	Env        map[string]string `json:"env"`
	UpdatedAt  string            `json:"updatedAt"`
}
type Workspace struct {
	ProfileID  string `json:"profileId,omitempty"`
	ID         string `json:"id"`
	Name       string `json:"name"`
	FolderPath string `json:"folderPath"`
	OpenedAt   string `json:"openedAt"`
}
type MCPConfig struct {
	ID       int64  `db:"id" json:"id"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Endpoint string `json:"endpoint"`
	Prefix   string `json:"prefix"`
	Order    int    `json:"order"`
}
type ProviderConfig struct {
	ID      int64  `db:"id" json:"id"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	BaseURL string `json:"base_url"`
	APIKey  string `json:"api_key,omitempty"`
	Model   string `json:"model"`
}

// Profile The types below are the persisted public contracts used by the React client.
// API keys and header values are deliberately omitted by read endpoints.
type Profile struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Role        string  `json:"role"`
	Accent      string  `json:"accent"`
	CreatedAt   string  `json:"createdAt"`
	Temperature float64 `json:"temperature"`
	TopP        float64 `json:"topP"`
}
type Agent struct {
	ID              string   `json:"id"`
	ProfileID       string   `json:"profileId"`
	Name            string   `json:"name"`
	Description     string   `json:"description"`
	SystemPrompt    string   `json:"systemPrompt"`
	MainModels      []string `json:"mainModels"`
	ToolModel       string   `json:"toolModel"`
	CompactionModel string   `json:"compactionModel"`
	CompactionLevel string   `json:"compactionLevel"`
	MemoryModel     string   `json:"memoryModel"`
	IconKey         string   `json:"iconKey"`
	Accent          string   `json:"accent"`
	SkillGroupIDs   []string `json:"skillGroupIds"`
	MCPIDs          []string `json:"mcpIds"`
}
type SkillGroup struct {
	ID          string   `json:"id" yaml:"id"`
	ProfileID   string   `json:"profileId" yaml:"profileId"`
	Name        string   `json:"name" yaml:"name"`
	Description string   `json:"description" yaml:"description,omitempty"`
	ApplyAuto   bool     `json:"applyAuto" yaml:"applyAuto"`
	SkillIDs    []string `json:"skillIds" yaml:"skillIds"`
}
type Preset struct {
	ID        string  `json:"id"`
	ProfileID string  `json:"profileId"`
	Title     string  `json:"title"`
	Text      string  `json:"text"`
	AgentID   *string `json:"agentId"`
}
type Skill struct {
	ID          string   `json:"id"`
	ProfileID   string   `json:"profileId"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Content     string   `json:"content"`
	Icon        string   `json:"icon"`
	Accent      string   `json:"accent"`
	Source      string   `json:"source"`
	SourceRef   string   `json:"sourceRef"`
	Enabled     bool     `json:"enabled"`
	Files       []string `json:"files,omitempty"`
}

// SkillPage is ordered by skill directory name. Cursor is the last returned
// directory name, never an absolute filesystem path.
type SkillPage struct {
	Items      []Skill `json:"items"`
	NextCursor string  `json:"nextCursor"`
	HasMore    bool    `json:"hasMore"`
	Total      int     `json:"total"`
}
type Provider struct {
	ID        string   `json:"id"`
	ProfileID string   `json:"profileId"`
	Name      string   `json:"name"`
	Kind      string   `json:"kind"`
	BaseURL   string   `json:"baseUrl"`
	HasAPIKey bool     `json:"hasApiKey"`
	Models    []string `json:"models"`
	Enabled   bool     `json:"enabled"`
	ProxyID   string   `json:"proxyId,omitempty"`
	RPM       int      `json:"rpm"`
}
type ProxyConfig struct {
	Type        string `json:"type"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	Username    string `json:"username,omitempty"`
	HasPassword bool   `json:"hasPassword,omitempty"`
	Password    string `json:"password,omitempty"`
}
type Proxy struct {
	ID                 string `json:"id"`
	Name               string `json:"name"`
	Description        string `json:"description"`
	Type               string `json:"type"`
	Host               string `json:"host"`
	Port               int    `json:"port"`
	Username           string `json:"username,omitempty"`
	HasPassword        bool   `json:"hasPassword,omitempty"`
	InsecureSkipVerify bool   `json:"insecureSkipVerify,omitempty"`
	Password           string `json:"password,omitempty"`
}
type MCPHeader struct {
	ID string `json:"id"`
	K  string `json:"k"`
	V  string `json:"v,omitempty"`
}
type MCPTool struct {
	Name        string         `json:"name"`
	Alias       string         `json:"alias"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"inputSchema,omitempty"`
	Enabled     bool           `json:"enabled"`
}
type MCPServer struct {
	ID           string      `json:"id"`
	ProfileID    string      `json:"profileId"`
	Name         string      `json:"name"`
	Transport    string      `json:"transport"`
	Command      string      `json:"command"`
	URL          string      `json:"url"`
	Prefix       string      `json:"prefix"`
	Headers      []MCPHeader `json:"headers"`
	Enabled      bool        `json:"enabled"`
	Instructions string      `json:"instructions,omitempty"`
	Tools        []MCPTool   `json:"tools"`
	BuiltinKey   string      `json:"builtinKey,omitempty"`
	System       bool        `json:"system,omitempty"`
}
type BuiltinMCPSettings struct {
	ProfileID  string    `json:"profileId"`
	BuiltinKey string    `json:"builtinKey"`
	Enabled    bool      `json:"enabled"`
	Tools      []MCPTool `json:"tools"`
}
type KBDoc struct {
	ID        string   `json:"id"`
	ProfileID string   `json:"profileId"`
	Title     string   `json:"title"`
	Source    string   `json:"source"`
	Kind      string   `json:"kind"`
	Content   string   `json:"content"`
	UpdatedAt string   `json:"updatedAt"`
	Tags      []string `json:"tags"`
	Size      int      `json:"size"`
}
type Conversation struct {
	ID          string `json:"id"`
	ProfileID   string `json:"profileId"`
	WorkspaceID string `json:"workspaceId"`
	AgentID     string `json:"agentId"`
	Title       string `json:"title"`
	UpdatedAt   string `json:"updatedAt"`
	ActiveModel string `json:"activeModel"`
	Count       int    `json:"count"`
}
