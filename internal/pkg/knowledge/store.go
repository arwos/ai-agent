/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package knowledge

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/search/query"

	"github.com/arwos/ai-agent/internal/pkg/models"
)

type Store struct {
	root        string
	pageSize    int
	searchLimit int
	mu          sync.Mutex
	indexes     map[string]bleve.Index
}

func New(root string, limits ...int) *Store {
	page, search := 10, 4
	if len(limits) > 0 && limits[0] > 0 {
		page = limits[0]
	}
	if len(limits) > 1 && limits[1] > 0 {
		search = limits[1]
	}
	return &Store{root: root, pageSize: page, searchLimit: search, indexes: make(map[string]bleve.Index)}
}
func (s *Store) PageSize() int    { return s.pageSize }
func (s *Store) SearchLimit() int { return s.searchLimit }

// NewAt is useful for isolated tests and custom embedding applications.
func NewAt(root string) *Store { return &Store{root: root, indexes: make(map[string]bleve.Index)} }

func (s *Store) dir(profile string) string      { return filepath.Join(s.root, profile, "documents") }
func (s *Store) file(profile, id string) string { return filepath.Join(s.dir(profile), id+".json") }
func safe(v string) bool {
	return v != "" && filepath.Base(v) == v && v != "." && v != ".." && !strings.ContainsAny(v, `/\\`)
}

func (s *Store) List(profile, cursor, query string, tags []string, limit int) ([]models.KBDoc, int, error) {
	if !safe(profile) {
		return nil, 0, fmt.Errorf("invalid profile id")
	}
	if limit <= 0 {
		limit = s.pageSize
	}
	entries, err := os.ReadDir(s.dir(profile))
	if err != nil && !os.IsNotExist(err) {
		return nil, 0, err
	}
	items := make([]models.KBDoc, 0)
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		b, e := os.ReadFile(filepath.Join(s.dir(profile), entry.Name()))
		if e != nil {
			return nil, 0, e
		}
		var d models.KBDoc
		if json.Unmarshal(b, &d) != nil {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(d.Title+" "+d.Source+" "+d.Content), strings.ToLower(query)) {
			continue
		}
		if !matchTags(d.Tags, tags) {
			continue
		}
		items = append(items, d)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID > items[j].ID })
	total := len(items)
	start := 0
	for i := range items {
		if items[i].ID == cursor {
			start = i + 1
			break
		}
	}
	end := start + limit
	if end > len(items) {
		end = len(items)
	}
	return items[start:end], total, nil
}
func matchTags(have, wanted []string) bool {
	for _, w := range wanted {
		found := false
		for _, h := range have {
			if h == w {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func (s *Store) Upsert(d models.KBDoc) (models.KBDoc, error) {
	if !safe(d.ProfileID) || d.Title == "" {
		return d, fmt.Errorf("profile id and title are required")
	}
	if d.ID == "" {
		sum := sha256.Sum256([]byte(d.ProfileID + "\x00" + d.Title + "\x00" + time.Now().String()))
		d.ID = "kb-" + fmt.Sprintf("%x", sum[:8])
	}
	d.Size = len(d.Content)
	d.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if d.Tags == nil {
		d.Tags = []string{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.dir(d.ProfileID), 0755); err != nil {
		return d, err
	}
	b, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return d, err
	}
	tmp, err := os.CreateTemp(s.dir(d.ProfileID), ".kb-*.tmp")
	if err != nil {
		return d, err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err = tmp.Write(append(b, '\n')); err == nil {
		err = tmp.Close()
	}
	if err != nil {
		return d, err
	}
	if err = os.Rename(name, s.file(d.ProfileID, d.ID)); err != nil {
		return d, err
	}
	return d, s.indexDocument(d)
}
func (s *Store) Delete(profile, id string) error {
	if !safe(profile) || !safe(id) {
		return fmt.Errorf("invalid document id")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.Remove(s.file(profile, id)); err != nil && !os.IsNotExist(err) {
		return err
	}
	if idx := s.indexes[profile]; idx != nil {
		_ = idx.Delete(id)
	}
	return nil
}

// DeleteProfile removes all profile-owned documents and the corresponding
// Bleve index. It never touches a workspace selected by the user.
func (s *Store) DeleteProfile(profile string) error {
	if !safe(profile) {
		return fmt.Errorf("invalid profile id")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if idx := s.indexes[profile]; idx != nil {
		if err := idx.Close(); err != nil {
			return err
		}
		delete(s.indexes, profile)
	}
	return os.RemoveAll(filepath.Join(s.root, profile))
}

// CleanupOrphanProfiles removes profile directories and their cached indexes
// only when the profile is absent from the configuration store.
func (s *Store) CleanupOrphanProfiles(valid map[string]struct{}) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.root)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, entry := range entries {
		if !entry.IsDir() || !safe(entry.Name()) {
			continue
		}
		if _, exists := valid[entry.Name()]; exists {
			continue
		}
		if idx := s.indexes[entry.Name()]; idx != nil {
			if err := idx.Close(); err != nil {
				return removed, err
			}
			delete(s.indexes, entry.Name())
		}
		if err := os.RemoveAll(filepath.Join(s.root, entry.Name())); err != nil {
			return removed, err
		}
		removed++
	}
	return removed, nil
}
func (s *Store) Tags(profile string) ([]string, error) {
	docs, _, err := s.List(profile, "", "", nil, 100000)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, d := range docs {
		for _, tag := range d.Tags {
			if tag = strings.TrimSpace(tag); tag != "" {
				seen[tag] = true
			}
		}
	}
	out := make([]string, 0, len(seen))
	for tag := range seen {
		out = append(out, tag)
	}
	sort.Strings(out)
	return out, nil
}
func (s *Store) Search(profile, text string, limit int) ([]models.KBDoc, error) {
	if limit <= 0 {
		limit = 4
	}
	s.mu.Lock()
	idx, err := s.index(profile)
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	q := query.NewMatchQuery(text)
	request := bleve.NewSearchRequestOptions(q, limit, 0, false)
	result, err := idx.Search(request)
	if err != nil {
		return nil, err
	}
	items := make([]models.KBDoc, 0, len(result.Hits))
	for _, hit := range result.Hits {
		b, e := os.ReadFile(s.file(profile, hit.ID))
		if e != nil {
			continue
		}
		var d models.KBDoc
		if json.Unmarshal(b, &d) == nil {
			items = append(items, d)
		}
	}
	return items, nil
}
func (s *Store) Reindex(profile string) (int, error) {
	docs, _, err := s.List(profile, "", "", nil, 100000)
	if err != nil {
		return 0, err
	}
	for _, d := range docs {
		if err := s.indexDocument(d); err != nil {
			return 0, err
		}
	}
	return len(docs), nil
}
func (s *Store) index(profile string) (bleve.Index, error) {
	if idx := s.indexes[profile]; idx != nil {
		return idx, nil
	}
	dir := filepath.Join(s.root, profile, "index")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	var idx bleve.Index
	var err error
	_, statErr := os.Stat(filepath.Join(dir, "index_meta.json"))
	fresh := statErr != nil
	if fresh {
		idx, err = bleve.New(dir, bleve.NewIndexMapping())
	} else {
		idx, err = bleve.Open(dir)
	}
	if err != nil {
		return nil, err
	}
	// Rebuild the newly created index from the durable documents directory.
	if fresh {
		if entries, readErr := os.ReadDir(s.dir(profile)); readErr == nil {
			for _, entry := range entries {
				if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
					continue
				}
				if b, readErr := os.ReadFile(filepath.Join(s.dir(profile), entry.Name())); readErr == nil {
					var d models.KBDoc
					if json.Unmarshal(b, &d) == nil {
						_ = idx.Index(d.ID, map[string]any{"title": d.Title, "source": d.Source, "content": d.Content, "tags": strings.Join(d.Tags, " ")})
					}
				}
			}
		}
	}
	s.indexes[profile] = idx
	return idx, nil
}
func (s *Store) indexDocument(d models.KBDoc) error {
	idx, err := s.index(d.ProfileID)
	if err != nil {
		return err
	}
	return idx.Index(d.ID, map[string]any{"title": d.Title, "source": d.Source, "content": d.Content, "tags": strings.Join(d.Tags, " ")})
}
