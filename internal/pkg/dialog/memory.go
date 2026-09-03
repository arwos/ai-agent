/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package dialog

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/mapping"
	"github.com/blevesearch/bleve/v2/search/query"
)

// Memory is the compact, durable context shown for a conversation.
type Memory struct {
	Title     string    `json:"title"`
	Summary   string    `json:"summary"`
	Topics    []string  `json:"topics"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type MemoryStore struct {
	Root            string
	CollectionsRoot string
	mu              sync.Mutex
}

// LongTermNote is a durable, user-editable memory scoped to a profile or workspace.
type LongTermNote struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Content   string    `json:"content"`
	Tags      []string  `json:"tags"`
	UpdatedAt time.Time `json:"updatedAt"`
	Scope     string    `json:"scope,omitempty"`
}

// TopicMemory is a generated or edited Markdown summary for a named topic.
type TopicMemory struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Summary   string    `json:"summary"`
	Tags      []string  `json:"tags"`
	UpdatedAt time.Time `json:"updatedAt"`
	Scope     string    `json:"scope,omitempty"`
}

type RelevantMemory struct {
	Kind    string   `json:"kind"`
	ID      string   `json:"id"`
	Title   string   `json:"title"`
	Content string   `json:"content"`
	Tags    []string `json:"tags"`
}

// MemoryPage is a cursor page ordered by the JSON file name on disk.
type MemoryPage[T any] struct {
	Items      []T    `json:"items"`
	NextCursor string `json:"nextCursor"`
	HasMore    bool   `json:"hasMore"`
}

func NewMemoryStore(root string) *MemoryStore {
	return &MemoryStore{Root: root, CollectionsRoot: filepath.Join(filepath.Dir(root), "memory")}
}

func safeMemoryPart(value, label string) error {
	if value == "" || value == "." || value == ".." || filepath.Base(value) != value || strings.ContainsAny(value, `/\\`) {
		return fmt.Errorf("invalid %s", label)
	}
	return nil
}

func (s *MemoryStore) path(profileID, _ string, dialogID string) (string, error) {
	if err := safeMemoryPart(profileID, "profile id"); err != nil {
		return "", err
	}
	if err := safeMemoryPart(dialogID, "dialog id"); err != nil {
		return "", err
	}
	return filepath.Join(s.Root, profileID, dialogID, "memory.json"), nil
}

// collectionPath keeps durable memories in one profile-scoped collection.
// The original ID stays inside JSON; its hash prevents it from becoming a path.
func (s *MemoryStore) collectionPath(profileID, collection, id string) (string, error) {
	if err := safeMemoryPart(profileID, "profile id"); err != nil {
		return "", err
	}
	if err := safeMemoryPart(id, "memory id"); err != nil {
		return "", err
	}
	prefix := "note"
	if collection == "topics" {
		prefix = "topic"
	}
	digest := sha256.Sum256([]byte(id))
	return filepath.Join(s.CollectionsRoot, profileID, collection, fmt.Sprintf("%s-%x.json", prefix, digest[:8])), nil
}

func (s *MemoryStore) Get(profileID, workspaceID, dialogID string) (Memory, error) {
	path, err := s.path(profileID, workspaceID, dialogID)
	if err != nil {
		return Memory{}, err
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Memory{Topics: []string{}}, nil
	}
	if err != nil {
		return Memory{}, err
	}
	var memory Memory
	if err := json.Unmarshal(b, &memory); err != nil {
		return Memory{}, err
	}
	if memory.Topics == nil {
		memory.Topics = []string{}
	}
	return memory, nil
}

func (s *MemoryStore) Save(profileID, workspaceID, dialogID string, memory Memory) error {
	path, err := s.path(profileID, workspaceID, dialogID)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if memory.UpdatedAt.IsZero() {
		memory.UpdatedAt = time.Now().UTC()
	}
	if memory.Topics == nil {
		memory.Topics = []string{}
	}
	b, err := json.MarshalIndent(memory, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".memory-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) //nolint:errcheck // cleanup errors cannot be returned from this scope
	if err = tmp.Chmod(0644); err == nil {
		_, err = tmp.Write(append(b, '\n'))
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func (s *MemoryStore) Delete(profileID, workspaceID, dialogID string) error {
	path, err := s.path(profileID, workspaceID, dialogID)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	// The dialog directory belongs only to this session memory. Remove it when empty.
	_ = os.Remove(filepath.Dir(path))
	return nil
}

// DeleteProfile removes durable notes, topics, and their derived indexes for a
// profile. Session memory remains under dialogs and is removed by Store.
func (s *MemoryStore) DeleteProfile(profileID string) error {
	if err := safeMemoryPart(profileID, "profile id"); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return os.RemoveAll(filepath.Join(s.CollectionsRoot, profileID))
}

// CleanupOrphanProfiles removes memory collections belonging to profiles that
// no longer exist. Notes and topics of an existing profile are intentionally
// retained because they are independent durable data.
func (s *MemoryStore) CleanupOrphanProfiles(valid map[string]struct{}) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.CollectionsRoot)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, entry := range entries {
		if !entry.IsDir() || safeMemoryPart(entry.Name(), "profile id") != nil {
			continue
		}
		if _, exists := valid[entry.Name()]; exists {
			continue
		}
		if err := os.RemoveAll(filepath.Join(s.CollectionsRoot, entry.Name())); err != nil {
			return removed, err
		}
		removed++
	}
	return removed, nil
}

func (s *MemoryStore) SaveNote(profileID, _ string, note LongTermNote) error {
	if note.ID == "" || note.Title == "" {
		return fmt.Errorf("note id and title are required")
	}
	path, err := s.collectionPath(profileID, "note", note.ID)
	if err != nil {
		return err
	}
	if err := s.saveJSON(path, &note); err != nil {
		return err
	}
	return s.indexMemory(profileID, "note", note.ID, note.Title, note.Tags, note.Content)
}

func (s *MemoryStore) SaveTopic(profileID, _ string, topic TopicMemory) error {
	if topic.ID == "" || topic.Title == "" {
		return fmt.Errorf("topic id and title are required")
	}
	path, err := s.collectionPath(profileID, "topics", topic.ID)
	if err != nil {
		return err
	}
	if err := s.saveJSON(path, &topic); err != nil {
		return err
	}
	return s.indexMemory(profileID, "topics", topic.ID, topic.Title, topic.Tags, topic.Summary)
}

func (s *MemoryStore) DeleteNote(profileID, _ string, id string) error {
	return s.deleteCollection(profileID, "note", id)
}
func (s *MemoryStore) DeleteTopic(profileID, _ string, id string) error {
	return s.deleteCollection(profileID, "topics", id)
}

func (s *MemoryStore) deleteCollection(profileID, collection, id string) error {
	path, err := s.collectionPath(profileID, collection, id)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return s.deleteIndexedMemory(profileID, collection, id)
}

func (s *MemoryStore) Notes(profileID, _ string) ([]LongTermNote, error) {
	out := make([]LongTermNote, 0)
	err := s.readCollection(profileID, "note", func(b []byte) error {
		var x LongTermNote
		if err := json.Unmarshal(b, &x); err != nil {
			return err
		}
		x.Scope = "profile"
		out = append(out, x)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out, nil
}

func (s *MemoryStore) NotesPage(profileID, cursor string, limit int) (MemoryPage[LongTermNote], error) {
	files, nextCursor, hasMore, err := s.collectionPage(profileID, "note", cursor, limit)
	if err != nil {
		return MemoryPage[LongTermNote]{Items: []LongTermNote{}}, err
	}
	items := make([]LongTermNote, 0, len(files))
	for _, file := range files {
		var note LongTermNote
		if err := json.Unmarshal(file, &note); err != nil {
			return MemoryPage[LongTermNote]{Items: []LongTermNote{}}, err
		}
		note.Scope = "profile"
		items = append(items, note)
	}
	return MemoryPage[LongTermNote]{Items: items, NextCursor: nextCursor, HasMore: hasMore}, nil
}

func (s *MemoryStore) Topics(profileID, _ string) ([]TopicMemory, error) {
	out := make([]TopicMemory, 0)
	err := s.readCollection(profileID, "topics", func(b []byte) error {
		var x TopicMemory
		if err := json.Unmarshal(b, &x); err != nil {
			return err
		}
		x.Scope = "profile"
		out = append(out, x)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out, nil
}

func (s *MemoryStore) TopicsPage(profileID, cursor string, limit int) (MemoryPage[TopicMemory], error) {
	files, nextCursor, hasMore, err := s.collectionPage(profileID, "topics", cursor, limit)
	if err != nil {
		return MemoryPage[TopicMemory]{Items: []TopicMemory{}}, err
	}
	items := make([]TopicMemory, 0, len(files))
	for _, file := range files {
		var topic TopicMemory
		if err := json.Unmarshal(file, &topic); err != nil {
			return MemoryPage[TopicMemory]{Items: []TopicMemory{}}, err
		}
		topic.Scope = "profile"
		items = append(items, topic)
	}
	return MemoryPage[TopicMemory]{Items: items, NextCursor: nextCursor, HasMore: hasMore}, nil
}

// Relevant returns the best matching file-backed notes and topic memories.
func (s *MemoryStore) Relevant(profileID, _ string, query string, limit int) ([]RelevantMemory, error) {
	if limit <= 0 {
		limit = 5
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	type rankedMemory struct {
		memory RelevantMemory
		score  float64
	}
	var ranked []rankedMemory
	for _, collection := range []string{"note", "topics"} {
		index, err := s.openMemoryIndex(profileID, collection)
		if err != nil {
			return nil, err
		}
		request := bleve.NewSearchRequest(memoryQuery(query))
		request.Size = limit
		response, searchErr := index.Search(request)
		closeErr := index.Close()
		if searchErr != nil {
			return nil, searchErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		for _, hit := range response.Hits {
			memory, err := s.readIndexedMemory(profileID, collection, hit.ID)
			if err != nil {
				return nil, err
			}
			if memory.ID != "" {
				ranked = append(ranked, rankedMemory{memory: memory, score: hit.Score})
			}
		}
	}
	sort.SliceStable(ranked, func(i, j int) bool { return ranked[i].score > ranked[j].score })
	if len(ranked) > limit {
		ranked = ranked[:limit]
	}
	results := make([]RelevantMemory, 0, len(ranked))
	for _, item := range ranked {
		results = append(results, item.memory)
	}
	return results, nil
}

// Reindex rebuilds the separate Bleve indexes for long-term notes and topics.
func (s *MemoryStore) Reindex(profileID string) (notes, topics int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, collection := range []string{"note", "topics"} {
		path, pathErr := s.memoryIndexPath(profileID, collection)
		if pathErr != nil {
			return 0, 0, pathErr
		}
		if removeErr := os.RemoveAll(path); removeErr != nil {
			return 0, 0, removeErr
		}
		index, openErr := s.openMemoryIndex(profileID, collection)
		if openErr != nil {
			return 0, 0, openErr
		}
		count, countErr := index.DocCount()
		closeErr := index.Close()
		if countErr != nil {
			return 0, 0, countErr
		}
		if closeErr != nil {
			return 0, 0, closeErr
		}
		if collection == "note" {
			notes = int(count)
		} else {
			topics = int(count)
		}
	}
	return notes, topics, nil
}

func memoryIndexMapping() mapping.IndexMapping {
	mapping := bleve.NewIndexMapping()
	document := bleve.NewDocumentMapping()
	for _, field := range []string{"title", "tags", "content"} {
		text := bleve.NewTextFieldMapping()
		text.Store = true
		document.AddFieldMappingsAt(field, text)
	}
	mapping.DefaultMapping = document
	return mapping
}

func memoryQuery(value string) query.Query {
	value = strings.TrimSpace(value)
	if value == "" {
		return bleve.NewMatchAllQuery()
	}
	title := bleve.NewMatchQuery(value)
	title.SetField("title")
	title.SetBoost(3)
	tags := bleve.NewMatchQuery(value)
	tags.SetField("tags")
	tags.SetBoost(2)
	content := bleve.NewMatchQuery(value)
	content.SetField("content")
	return bleve.NewDisjunctionQuery(title, tags, content)
}

func (s *MemoryStore) memoryIndexPath(profileID, collection string) (string, error) {
	if err := safeMemoryPart(profileID, "profile id"); err != nil {
		return "", err
	}
	if collection != "note" && collection != "topics" {
		return "", fmt.Errorf("unknown memory collection %q", collection)
	}
	return filepath.Join(s.CollectionsRoot, profileID, collection, "index"), nil
}

func (s *MemoryStore) openMemoryIndex(profileID, collection string) (bleve.Index, error) {
	path, err := s.memoryIndexPath(profileID, collection)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		index, err := bleve.New(path, memoryIndexMapping())
		if err != nil {
			return nil, err
		}
		if err := s.rebuildMemoryIndex(index, profileID, collection); err != nil {
			_ = index.Close()
			return nil, err
		}
		return index, nil
	} else if err != nil {
		return nil, err
	}
	return bleve.Open(path)
}

func (s *MemoryStore) rebuildMemoryIndex(index bleve.Index, profileID, collection string) error {
	err := s.readCollection(profileID, collection, func(data []byte) error {
		var id, title, content string
		var tags []string
		if collection == "note" {
			var note LongTermNote
			if err := json.Unmarshal(data, &note); err != nil {
				return err
			}
			id, title, content, tags = note.ID, note.Title, note.Content, note.Tags
		} else {
			var topic TopicMemory
			if err := json.Unmarshal(data, &topic); err != nil {
				return err
			}
			id, title, content, tags = topic.ID, topic.Title, topic.Summary, topic.Tags
		}
		return index.Index(id, map[string]any{"title": title, "tags": tags, "content": content})
	})
	return err
}

func (s *MemoryStore) indexMemory(profileID, collection, id, title string, tags []string, content string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	index, err := s.openMemoryIndex(profileID, collection)
	if err != nil {
		return err
	}
	defer index.Close() //nolint:errcheck // cleanup errors cannot be returned from this scope
	return index.Index(id, map[string]any{"title": title, "tags": tags, "content": content})
}

func (s *MemoryStore) deleteIndexedMemory(profileID, collection, id string) error {
	index, err := s.openMemoryIndex(profileID, collection)
	if err != nil {
		return err
	}
	defer index.Close() //nolint:errcheck // cleanup errors cannot be returned from this scope
	return index.Delete(id)
}

func (s *MemoryStore) readIndexedMemory(profileID, collection, id string) (RelevantMemory, error) {
	path, err := s.collectionPath(profileID, collection, id)
	if err != nil {
		return RelevantMemory{}, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return RelevantMemory{}, nil
	}
	if err != nil {
		return RelevantMemory{}, err
	}
	if collection == "note" {
		var note LongTermNote
		if err := json.Unmarshal(data, &note); err != nil {
			return RelevantMemory{}, err
		}
		return RelevantMemory{Kind: "note", ID: note.ID, Title: note.Title, Content: note.Content, Tags: note.Tags}, nil
	}
	var topic TopicMemory
	if err := json.Unmarshal(data, &topic); err != nil {
		return RelevantMemory{}, err
	}
	return RelevantMemory{Kind: "topic", ID: topic.ID, Title: topic.Title, Content: topic.Summary, Tags: topic.Tags}, nil
}

func (s *MemoryStore) readCollection(profileID, collection string, add func([]byte) error) error {
	if err := safeMemoryPart(profileID, "profile id"); err != nil {
		return err
	}
	dir := filepath.Join(s.CollectionsRoot, profileID, collection)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return err
		}
		if err = add(b); err != nil {
			return err
		}
	}
	return nil
}

func (s *MemoryStore) collectionPage(profileID, collection, cursor string, limit int) ([][]byte, string, bool, error) {
	if err := safeMemoryPart(profileID, "profile id"); err != nil {
		return nil, "", false, err
	}
	if cursor != "" && (filepath.Base(cursor) != cursor || filepath.Ext(cursor) != ".json") {
		return nil, "", false, fmt.Errorf("invalid memory cursor")
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	dir := filepath.Join(s.CollectionsRoot, profileID, collection)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return [][]byte{}, "", false, nil
	}
	if err != nil {
		return nil, "", false, err
	}
	items := make([][]byte, 0, limit)
	nextCursor := ""
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" || (cursor != "" && entry.Name() <= cursor) {
			continue
		}
		if len(items) == limit {
			return items, nextCursor, true, nil
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, "", false, err
		}
		items = append(items, data)
		nextCursor = entry.Name()
	}
	return items, nextCursor, false, nil
}

func (s *MemoryStore) saveJSON(path string, value any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if withTime, ok := value.(*LongTermNote); ok {
		if withTime.UpdatedAt.IsZero() {
			withTime.UpdatedAt = time.Now().UTC()
		}
		if withTime.Tags == nil {
			withTime.Tags = []string{}
		}
	}
	if withTime, ok := value.(*TopicMemory); ok {
		if withTime.UpdatedAt.IsZero() {
			withTime.UpdatedAt = time.Now().UTC()
		}
		if withTime.Tags == nil {
			withTime.Tags = []string{}
		}
	}
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".memory-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name) //nolint:errcheck // cleanup errors cannot be returned from this scope
	if _, err = tmp.Write(append(b, '\n')); err == nil {
		err = tmp.Close()
	} else {
		_ = tmp.Close()
	}
	if err != nil {
		return err
	}
	return os.Rename(name, path)
}
