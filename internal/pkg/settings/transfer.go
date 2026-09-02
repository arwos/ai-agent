/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package settings

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"
)

const Format = "arwos-settings"
const Version = 1

// Bundle is deliberately profile-neutral: profile identifiers never cross the
// import/export boundary. Collections are raw JSON so this package remains a
// transport/validation layer and the ORM stays in configstore.
type Bundle struct {
	Format      string            `json:"format"`
	Version     int               `json:"version"`
	CreatedAt   string            `json:"createdAt"`
	AppSettings json.RawMessage   `json:"appSettings,omitempty"`
	Providers   []json.RawMessage `json:"providers,omitempty"`
	MCP         []json.RawMessage `json:"mcp,omitempty"`
	Proxies     []json.RawMessage `json:"proxies,omitempty"`
	Agents      []json.RawMessage `json:"agents,omitempty"`
	Presets     []json.RawMessage `json:"presets,omitempty"`
	Skills      []json.RawMessage `json:"skills,omitempty"`
	KB          []json.RawMessage `json:"kb,omitempty"`
	Notes       []json.RawMessage `json:"notes,omitempty"`
	Topics      []json.RawMessage `json:"topics,omitempty"`
}

func New() Bundle {
	return Bundle{Format: Format, Version: Version, CreatedAt: time.Now().UTC().Format(time.RFC3339), AppSettings: json.RawMessage(`[]`)}
}

func Validate(b Bundle) error {
	if b.Format != Format {
		return fmt.Errorf("unsupported settings format %q", b.Format)
	}
	if b.Version != Version {
		return fmt.Errorf("unsupported settings version %d", b.Version)
	}
	return nil
}

func EncodeSecret(value string) string { return base64.StdEncoding.EncodeToString([]byte(value)) }
func DecodeSecret(value string) (string, error) {
	b, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return "", fmt.Errorf("invalid encoded secret: %w", err)
	}
	return string(b), nil
}

func Marshal(b Bundle) ([]byte, error) {
	if err := Validate(b); err != nil {
		return nil, err
	}
	return json.MarshalIndent(b, "", "  ")
}
func Unmarshal(data []byte) (Bundle, error) {
	var b Bundle
	if err := json.Unmarshal(data, &b); err != nil {
		return b, fmt.Errorf("invalid settings export: %w", err)
	}
	if err := Validate(b); err != nil {
		return b, err
	}
	return b, nil
}
