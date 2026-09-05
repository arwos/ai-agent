/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package app

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"time"
)

func exportRaw(items any) ([]json.RawMessage, error) {
	b, err := json.Marshal(items)
	if err != nil {
		return nil, err
	}
	var values []map[string]any
	if err = json.Unmarshal(b, &values); err != nil {
		return nil, err
	}
	out := make([]json.RawMessage, 0, len(values))
	for _, value := range values {
		delete(value, "profileId")
		delete(value, "profile_id")
		raw, e := json.Marshal(value)
		if e != nil {
			return nil, e
		}
		out = append(out, raw)
	}
	return out, nil
}
func uniqueStrings(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
func containsString(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}
func mustJSON(value any) json.RawMessage { b, _ := json.Marshal(value); return b }
func entityID(prefix string) string {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
	}
	return prefix + "-" + hex.EncodeToString(buf)
}

var profileScopedID = regexp.MustCompile(`^(.+)-([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})-([0-9a-fA-F]{24})$`)
var entityIDPattern = regexp.MustCompile(`^(.+)-([0-9a-fA-F]{24})$`)

func applyPatch(target any, patch json.RawMessage) error {
	if len(patch) == 0 || string(patch) == "null" {
		return nil
	}
	base, err := json.Marshal(target)
	if err != nil {
		return err
	}
	var values map[string]json.RawMessage
	if err = json.Unmarshal(base, &values); err != nil {
		return err
	}
	var updates map[string]json.RawMessage
	if err = json.Unmarshal(patch, &updates); err != nil {
		return fmt.Errorf("invalid patch: %w", err)
	}
	for key, value := range updates {
		values[key] = value
	}
	merged, err := json.Marshal(values)
	if err != nil {
		return err
	}
	return json.Unmarshal(merged, target)
}
func decode(p json.RawMessage, v any) error {
	if len(p) == 0 || string(p) == "null" {
		return nil
	}
	if e := json.Unmarshal(p, v); e != nil {
		return fmt.Errorf("invalid params: %w", e)
	}
	return nil
}
