/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package dialog

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/arwos/ai-agent/internal/pkg/goal"
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
	DecodeError bool           `json:"-"`
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

type historyIndexEntry struct {
	ID       string
	Start    int64
	End      int64
	Role     string
	Resolves string
	Tokens   int
	Invalid  bool
}

const historyIndexEntrySize int64 = 32

type historyMeta struct {
	FileSize    int64 `json:"fileSize"`
	TotalBytes  int64 `json:"totalBytes"`
	TotalTokens int   `json:"totalTokens"`
}

func (s *Store) historyIndexPath(id string) string { return s.historyPath(id) + ".index" }
func (s *Store) historyMetaPath(id string) string  { return s.historyPath(id) + ".meta.json" }

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
	if _, e := os.Stat(path); e == nil {
		indexInfo, indexErr := os.Stat(s.historyIndexPath(id))
		if os.IsNotExist(indexErr) || (indexErr == nil && indexInfo.Size()%historyIndexEntrySize != 0) {
			if e = s.buildHistoryIndex(id); e != nil {
				return e
			}
		}
	} else if !os.IsNotExist(e) {
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
	start, e := f.Seek(0, io.SeekEnd)
	if e != nil {
		return e
	}
	line := append(b, '\n')
	if _, e = f.Write(line); e != nil {
		return e
	}
	if e = s.appendHistoryIndex(id, historyIndexEntry{ID: m.ID, Start: start, End: start + int64(len(line)), Role: m.Role, Resolves: m.Resolves, Tokens: m.Tokens}); e != nil {
		return e
	}
	return nil
}

func (s *Store) appendHistoryIndex(id string, entry historyIndexEntry) error {
	path := s.historyIndexPath(id)
	indexFile, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0644) //nolint:gosec // path is derived from validated session parts
	if err != nil {
		return err
	}
	defer func() { _ = indexFile.Close() }()
	if entry.Role == "message_delete" {
		if entry.Resolves != "" {
			if err = removeHistoryIndexEntry(indexFile, entry.Resolves); err != nil {
				return err
			}
		}
	} else {
		if _, err = indexFile.Seek(0, io.SeekEnd); err != nil {
			return err
		}
		if err = writeHistoryIndexEntry(indexFile, entry); err != nil {
			return err
		}
	}
	stat, err := os.Stat(s.historyPath(id))
	if err != nil {
		return err
	}
	meta := historyMeta{FileSize: stat.Size(), TotalBytes: stat.Size()}
	if old, readErr := os.ReadFile(s.historyMetaPath(id)); readErr == nil {
		_ = json.Unmarshal(old, &meta)
	}
	meta.FileSize = stat.Size()
	meta.TotalBytes = stat.Size()
	meta.TotalTokens += entry.Tokens
	return writeHistoryMeta(s.historyMetaPath(id), meta)
}

func removeHistoryIndexEntry(indexFile *os.File, id string) error {
	key := historyIDKey(id)
	var record [historyIndexEntrySize]byte
	for offset := int64(0); ; offset += historyIndexEntrySize {
		if _, err := indexFile.ReadAt(record[:], offset); err == io.EOF {
			return nil
		} else if err != nil {
			return err
		}
		if bytes.Equal(record[:16], key[:]) {
			_, err := indexFile.WriteAt(make([]byte, historyIndexEntrySize), offset)
			return err
		}
	}
}

func historyIDKey(id string) [16]byte {
	var key [16]byte
	clean := strings.ReplaceAll(id, "-", "")
	if len(clean) == 32 {
		if decoded, err := hex.DecodeString(clean); err == nil {
			copy(key[:], decoded)
			return key
		}
	}
	sum := sha256.Sum256([]byte(id))
	copy(key[:], sum[:16])
	return key
}

func writeHistoryIndexEntry(w io.Writer, entry historyIndexEntry) error {
	var record [historyIndexEntrySize]byte
	key := historyIDKey(entry.ID)
	copy(record[:16], key[:])
	binary.LittleEndian.PutUint64(record[16:24], uint64(entry.Start))
	binary.LittleEndian.PutUint64(record[24:32], uint64(entry.End))
	_, err := w.Write(record[:])
	return err
}

func writeHistoryMeta(path string, meta historyMeta) error {
	b, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0644)
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
	if limit == 0 {
		limit = -1
	}
	items, err := s.HistoryPage(id, "", "", limit)
	return items.Messages, err
}

type HistoryPageResult struct {
	Messages []Message
	HasOlder bool
	HasNewer bool
	OlderID  string
	NewerID  string
}

func (s *Store) HistoryPage(id, beforeID, afterID string, limit int) (HistoryPageResult, error) {
	if limit == 0 {
		limit = 20
	}
	if limit > 0 {
		return s.indexedHistoryPage(id, beforeID, afterID, limit)
	}
	f, e := os.Open(s.historyPath(id))
	if os.IsNotExist(e) {
		return HistoryPageResult{Messages: []Message{}}, nil
	}
	if e != nil {
		return HistoryPageResult{}, e
	}
	defer func() { _ = f.Close() }()
	var all []Message
	deleted := make(map[string]struct{})
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 100<<20)
	line := 0
	for sc.Scan() {
		line++
		var m Message
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			all = append(all, Message{ID: fmt.Sprintf("%s-invalid-%d", id, line), DecodeError: true})
			continue
		}
		if m.Role == "message_delete" && m.Resolves != "" {
			deleted[m.Resolves] = struct{}{}
			continue
		}
		all = append(all, m)
	}
	if scanErr := sc.Err(); scanErr != nil {
		all = append(all, Message{ID: fmt.Sprintf("%s-invalid-%d", id, line+1), DecodeError: true})
	}
	filtered := all[:0]
	for _, m := range all {
		if _, ok := deleted[m.ID]; !ok {
			filtered = append(filtered, m)
		}
	}
	all = filtered
	start, end := 0, len(all)
	if limit < 0 {
		return HistoryPageResult{Messages: all}, nil
	}
	if beforeID != "" {
		for index, message := range all {
			if message.ID == beforeID {
				end = index
				break
			}
		}
		if end > limit {
			start = end - limit
		}
	} else if afterID != "" {
		for index, message := range all {
			if message.ID == afterID {
				start = index + 1
				break
			}
		}
		if end-start > limit {
			end = start + limit
		}
	} else if end > limit {
		start = end - limit
	}
	page := all[start:end]
	result := HistoryPageResult{Messages: page, HasOlder: start > 0, HasNewer: end < len(all)}
	if result.HasOlder {
		result.OlderID = all[start].ID
	}
	if result.HasNewer {
		result.NewerID = all[end-1].ID
	}
	return result, nil
}

func (s *Store) indexedHistoryPage(id, beforeID, afterID string, limit int) (HistoryPageResult, error) {
	if _, err := os.Stat(s.historyPath(id)); os.IsNotExist(err) {
		return HistoryPageResult{Messages: []Message{}}, nil
	} else if err != nil {
		return HistoryPageResult{}, err
	}
	index, err := os.Open(s.historyIndexPath(id))
	if os.IsNotExist(err) {
		if err = s.buildHistoryIndex(id); err != nil {
			return HistoryPageResult{}, err
		}
		index, err = os.Open(s.historyIndexPath(id))
	}
	if err != nil {
		return HistoryPageResult{}, err
	}
	defer func() { _ = index.Close() }()
	stat, err := index.Stat()
	if err != nil {
		return HistoryPageResult{}, err
	}
	if stat.Size()%historyIndexEntrySize != 0 {
		if err = s.buildHistoryIndex(id); err != nil {
			return HistoryPageResult{}, err
		}
		return s.indexedHistoryPage(id, beforeID, afterID, limit)
	}
	slots := stat.Size() / historyIndexEntrySize
	from, step := slots-1, int64(-1)
	if afterID != "" {
		cursor, found, readErr := findHistoryIndexSlot(index, historyIDKey(afterID), slots)
		if readErr != nil {
			return HistoryPageResult{}, readErr
		}
		if found {
			from, step = cursor+1, 1
		}
	} else if beforeID != "" {
		cursor, found, readErr := findHistoryIndexSlot(index, historyIDKey(beforeID), slots)
		if readErr != nil {
			return HistoryPageResult{}, readErr
		}
		if found {
			from = cursor - 1
		}
	}
	entries, hasMore, err := readHistoryIndexPage(index, from, step, slots, limit)
	if err != nil {
		return HistoryPageResult{}, err
	}
	if step < 0 {
		for left, right := 0, len(entries)-1; left < right; left, right = left+1, right-1 {
			entries[left], entries[right] = entries[right], entries[left]
		}
	}
	page := make([]Message, 0, len(entries))
	for _, entry := range entries {
		message, readErr := s.readIndexedMessage(id, entry)
		if readErr != nil {
			return HistoryPageResult{}, readErr
		}
		page = append(page, message)
	}
	result := HistoryPageResult{Messages: page}
	if len(entries) > 0 {
		result.OlderID = messageIndexID(page[0], entries[0])
		result.NewerID = messageIndexID(page[len(page)-1], entries[len(entries)-1])
	}
	if step < 0 {
		result.HasOlder = hasMore
		result.HasNewer = beforeID != ""
	} else {
		result.HasNewer = hasMore
		result.HasOlder = afterID != ""
	}
	return result, nil
}

func findHistoryIndexSlot(index *os.File, key [16]byte, slots int64) (int64, bool, error) {
	var record [historyIndexEntrySize]byte
	for slot := int64(0); slot < slots; slot++ {
		if _, err := index.ReadAt(record[:], slot*historyIndexEntrySize); err != nil {
			return 0, false, err
		}
		if bytes.Equal(record[:16], key[:]) {
			return slot, true, nil
		}
	}
	return 0, false, nil
}

func readHistoryIndexPage(index *os.File, from, step, slots int64, limit int) ([]historyIndexEntry, bool, error) {
	entries := make([]historyIndexEntry, 0, limit)
	var record [historyIndexEntrySize]byte
	for slot := from; slot >= 0 && slot < slots; slot += step {
		if _, err := index.ReadAt(record[:], slot*historyIndexEntrySize); err != nil {
			return nil, false, err
		}
		if !bytes.Equal(record[:16], make([]byte, 16)) {
			entries = append(entries, historyIndexEntry{Start: int64(binary.LittleEndian.Uint64(record[16:24])), End: int64(binary.LittleEndian.Uint64(record[24:32]))})
			if len(entries) == limit {
				for slot += step; slot >= 0 && slot < slots; slot += step {
					if _, err := index.ReadAt(record[:], slot*historyIndexEntrySize); err != nil {
						return nil, false, err
					}
					if !bytes.Equal(record[:16], make([]byte, 16)) {
						return entries, true, nil
					}
				}
				return entries, false, nil
			}
		}
	}
	return entries, false, nil
}

func messageIndexID(message Message, entry historyIndexEntry) string {
	if message.ID != "" {
		return message.ID
	}
	return fmt.Sprintf("%d-%d", entry.Start, entry.End)
}

func (s *Store) historyIndex(id string) ([]historyIndexEntry, error) {
	path := s.historyIndexPath(id)
	if info, err := os.Stat(path); os.IsNotExist(err) || (err == nil && info.Size()%historyIndexEntrySize != 0) {
		if err := s.buildHistoryIndex(id); err != nil {
			return nil, err
		}
	}
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return []historyIndexEntry{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	stat, err := f.Stat()
	if err != nil {
		return nil, err
	}
	entries := make([]historyIndexEntry, 0, stat.Size()/historyIndexEntrySize)
	var record [historyIndexEntrySize]byte
	for offset := int64(0); offset < stat.Size(); offset += historyIndexEntrySize {
		if _, err := f.ReadAt(record[:], offset); err != nil {
			return nil, err
		}
		entry := historyIndexEntry{
			Start: int64(binary.LittleEndian.Uint64(record[16:24])),
			End:   int64(binary.LittleEndian.Uint64(record[24:32])),
		}
		message, err := s.readIndexedMessage(id, entry)
		if err != nil {
			return nil, err
		}
		entry.ID, entry.Role, entry.Resolves, entry.Tokens, entry.Invalid = message.ID, message.Role, message.Resolves, message.Tokens, message.DecodeError
		if entry.ID == "" {
			entry.ID = hex.EncodeToString(record[:16])
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func (s *Store) buildHistoryIndex(id string) error {
	history, err := os.Open(s.historyPath(id))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer func() { _ = history.Close() }()
	index, err := os.OpenFile(s.historyIndexPath(id), os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0644) //nolint:gosec // derived session path
	if err != nil {
		return err
	}
	defer func() { _ = index.Close() }()
	sc := bufio.NewScanner(history)
	sc.Buffer(make([]byte, 64*1024), 100<<20)
	var offset int64
	var totalTokens int
	line := 0
	for sc.Scan() {
		line++
		data := sc.Bytes()
		var message Message
		entry := historyIndexEntry{Start: offset, End: offset + int64(len(data)+1)}
		if err := json.Unmarshal(data, &message); err != nil {
			entry.ID = fmt.Sprintf("%s-invalid-%d", id, line)
			entry.Invalid = true
		} else {
			entry.ID, entry.Role, entry.Resolves, entry.Tokens = message.ID, message.Role, message.Resolves, message.Tokens
			totalTokens += message.Tokens
		}
		if entry.Role == "message_delete" {
			if err = removeHistoryIndexEntry(index, entry.Resolves); err != nil {
				return err
			}
		} else if err = writeHistoryIndexEntry(index, entry); err != nil {
			return err
		}
		offset += int64(len(data) + 1)
	}
	if err := sc.Err(); err != nil {
		return err
	}
	return writeHistoryMeta(s.historyMetaPath(id), historyMeta{FileSize: offset, TotalBytes: offset, TotalTokens: totalTokens})
}

func (s *Store) readIndexedMessage(id string, entry historyIndexEntry) (Message, error) {
	f, err := os.Open(s.historyPath(id))
	if err != nil {
		return Message{}, err
	}
	defer func() { _ = f.Close() }()
	data := make([]byte, entry.End-entry.Start)
	if _, err = f.ReadAt(data, entry.Start); err != nil && err != io.EOF {
		return Message{}, err
	}
	var message Message
	if entry.Invalid || json.Unmarshal(bytes.TrimSpace(data), &message) != nil {
		return Message{ID: entry.ID, DecodeError: true}, nil
	}
	return message, nil
}

func (s *Store) recentHistoryPage(id string, limit int) (HistoryPageResult, error) {
	f, err := os.Open(s.historyPath(id))
	if os.IsNotExist(err) {
		return HistoryPageResult{Messages: []Message{}}, nil
	}
	if err != nil {
		return HistoryPageResult{}, err
	}
	defer func() { _ = f.Close() }()

	page := make([]Message, 0, limit)
	deleted := make(map[string]struct{})
	hasOlder := false
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 100<<20)
	line := 0
	appendMessage := func(message Message) {
		if _, ok := deleted[message.ID]; ok {
			return
		}
		if len(page) == limit {
			hasOlder = true
			page = page[1:]
		}
		page = append(page, message)
	}
	for sc.Scan() {
		line++
		var message Message
		if err := json.Unmarshal(sc.Bytes(), &message); err != nil {
			appendMessage(Message{ID: fmt.Sprintf("%s-invalid-%d", id, line), DecodeError: true})
			continue
		}
		if message.Role == "message_delete" && message.Resolves != "" {
			deleted[message.Resolves] = struct{}{}
			for index := range page {
				if page[index].ID == message.Resolves {
					page = append(page[:index], page[index+1:]...)
					break
				}
			}
			continue
		}
		appendMessage(message)
	}
	if sc.Err() != nil {
		line++
		appendMessage(Message{ID: fmt.Sprintf("%s-invalid-%d", id, line), DecodeError: true})
	}
	result := HistoryPageResult{Messages: page, HasOlder: hasOlder}
	if len(page) > 0 {
		result.OlderID = page[0].ID
		result.NewerID = page[len(page)-1].ID
	}
	return result, nil
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
	if err := os.WriteFile(path, nil, 0644); err != nil {
		return err
	}
	_ = os.Remove(s.historyIndexPath(id))
	_ = os.Remove(s.historyMetaPath(id))
	return nil
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
		_ = os.Remove(s.historyIndexPath(id))
		_ = os.Remove(s.historyMetaPath(id))
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

func (s *Store) goalStore() *goal.Store {
	return goal.NewStore(s.Root)
}

func (s *Store) Goals(id string) ([]goal.Goal, error)     { return s.goalStore().Goals(id) }
func (s *Store) Goal(id string) (*goal.Goal, error)       { return s.goalStore().Goal(id) }
func (s *Store) SaveGoal(id string, item goal.Goal) error { return s.goalStore().SaveGoal(id, item) }
func (s *Store) DeleteGoal(id string) error               { return s.goalStore().DeleteGoal(id) }
func (s *Store) DeleteGoals(id string) error              { return s.goalStore().DeleteGoals(id) }

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
