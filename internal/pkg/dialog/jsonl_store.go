/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package dialog

import (
	"bufio"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Message struct {
	ID          string         `json:"id"`
	Timestamp   time.Time      `json:"timestamp"`
	Role        string         `json:"role"`
	Content     string         `json:"content"`
	Model       string         `json:"model,omitempty"`
	Provider    string         `json:"provider,omitempty"`
	ContextSize int            `json:"contextSize,omitempty"`
	Tokens      int            `json:"tokens,omitempty"`
	Compact     bool           `json:"compact,omitempty"`
	Error       bool           `json:"error,omitempty"`
	Resolves    string         `json:"resolves,omitempty"`
	ToolsUsed   []string       `json:"tools_used,omitempty"`
	Tool        string         `json:"tool,omitempty"`
	Arguments   map[string]any `json:"arguments,omitempty"`
}
type Store struct {
	Root         string
	HistoryLimit int
	mu           sync.Mutex
}

const sessionSeparator = "\x00"

// SessionKey identifies a dialog independently of its workspace and maps it to
// datasource/dialogs/{profileID}/{sessionID}/history.jsonl.
func SessionKey(profileID, sessionID string) string { return profileID + sessionSeparator + sessionID }

func (s *Store) historyPath(id string) string {
	parts := strings.SplitN(id, sessionSeparator, 2)
	if len(parts) == 2 && safePart(parts[0]) && safePart(parts[1]) {
		return filepath.Join(s.Root, parts[0], parts[1], "history.jsonl")
	}
	return filepath.Join(s.Root, filepath.Base(id)+".jsonl") // compatibility for legacy callers/files
}

func safePart(value string) bool {
	return value != "" && value != "." && value != ".." && filepath.Base(value) == value && !strings.ContainsAny(value, `/\\`)
}

func (s *Store) Append(id string, m Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if m.Timestamp.IsZero() {
		m.Timestamp = time.Now().UTC()
	}
	if m.ID == "" {
		m.ID = messageID()
	}
	if e := os.MkdirAll(s.Root, 0755); e != nil {
		return e
	}
	path := s.historyPath(id)
	if e := os.MkdirAll(filepath.Dir(path), 0755); e != nil {
		return e
	}
	f, e := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644) //nolint:gosec // validated input or bounded archive is required here
	if e != nil {
		return e
	}
	defer func() { _ = f.Close() }()
	b, e := json.Marshal(m)
	if e != nil {
		return e
	}
	_, e = f.Write(append(b, '\n'))
	return e
}

func messageID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("00000000-0000-4000-8000-%012x", time.Now().UnixNano())
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// NewMessageID returns a UUID for records that must be referenced by another
// component before they are appended to JSONL.
func NewMessageID() string { return messageID() }
func (s *Store) History(id string, limit int) ([]Message, error) {
	f, e := os.Open(s.historyPath(id))
	if os.IsNotExist(e) {
		return []Message{}, nil
	}
	if e != nil {
		return nil, e
	}
	defer func() { _ = f.Close() }()
	var all []Message
	deleted := make(map[string]struct{})
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var m Message
		if json.Unmarshal(sc.Bytes(), &m) == nil {
			if m.Role == "message_delete" && m.Resolves != "" {
				deleted[m.Resolves] = struct{}{}
				continue
			}
			all = append(all, m)
		}
	}
	filtered := all[:0]
	for _, m := range all {
		if _, ok := deleted[m.ID]; !ok {
			filtered = append(filtered, m)
		}
	}
	all = filtered
	if limit > 0 && len(all) > limit {
		all = all[len(all)-limit:]
	}
	return all, sc.Err()
}

// DeleteMessage appends a tombstone, preserving append-only JSONL writes.
func (s *Store) DeleteMessage(id, messageID string) error {
	if messageID == "" {
		return fmt.Errorf("message id is required")
	}
	return s.Append(id, Message{Role: "message_delete", Resolves: messageID})
}
func (s *Store) Clear(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := os.MkdirAll(s.Root, 0755); e != nil {
		return e
	}
	path := s.historyPath(id)
	if e := os.MkdirAll(filepath.Dir(path), 0755); e != nil {
		return e
	}
	return os.WriteFile(path, nil, 0644)
}

// ResolveError appends a tombstone instead of rewriting JSONL. Readers hide a
// resolved technical error while retaining the immutable audit trail.
func (s *Store) ResolveError(id, messageID string) error {
	if messageID == "" {
		return nil
	}
	return s.Append(id, Message{Role: "error_resolution", Resolves: messageID})
}

// Delete removes the JSONL history for a dialog. A missing file is already deleted.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	path := s.historyPath(id)
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err == nil {
		_ = os.Remove(filepath.Dir(path))
	}
	return err
}

// DeleteProfile removes every dialog history for one validated profile.
func (s *Store) DeleteProfile(profileID string) error {
	if !safePart(profileID) {
		return fmt.Errorf("invalid profile id")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return os.RemoveAll(filepath.Join(s.Root, profileID))
}

// CleanupOrphans removes legacy histories and profile/session directories that
// are no longer represented by the configuration store. It only operates
// below Store.Root and never follows caller-provided paths.
func (s *Store) CleanupOrphans(valid map[string]map[string]struct{}) (profiles, dialogs int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.Root)
	if os.IsNotExist(err) {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, err
	}
	for _, entry := range entries {
		fullPath := filepath.Join(s.Root, entry.Name())
		if !entry.IsDir() {
			if filepath.Ext(entry.Name()) == ".jsonl" {
				if err := os.Remove(fullPath); err != nil {
					return profiles, dialogs, err
				}
				dialogs++
			}
			continue
		}
		sessions, exists := valid[entry.Name()]
		if !safePart(entry.Name()) || !exists {
			if err := os.RemoveAll(fullPath); err != nil {
				return profiles, dialogs, err
			}
			profiles++
			continue
		}
		children, readErr := os.ReadDir(fullPath)
		if readErr != nil {
			return profiles, dialogs, readErr
		}
		for _, child := range children {
			if !child.IsDir() || !safePart(child.Name()) {
				continue
			}
			if _, found := sessions[child.Name()]; found {
				continue
			}
			if err := os.RemoveAll(filepath.Join(fullPath, child.Name())); err != nil {
				return profiles, dialogs, err
			}
			dialogs++
		}
	}
	return profiles, dialogs, nil
}
