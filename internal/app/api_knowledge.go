/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package app

import (
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"go.osspkg.com/goppy/v3/plugins/ws"
	"go.osspkg.com/goppy/v3/plugins/ws/event"

	"github.com/arwos/ai-agent/internal/pkg/models"
	"github.com/arwos/ai-agent/internal/pkg/utils"
)

func (a *App) kbList(ev event.Event, _ ws.Meta) error {
	var q struct {
		ProfileID string   `json:"profileId"`
		Limit     int      `json:"limit"`
		LastID    string   `json:"lastId"`
		Query     string   `json:"query"`
		Tags      []string `json:"tags"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	if q.ProfileID == "" {
		id, err := a.activeProfile()
		if err != nil {
			return err
		}
		q.ProfileID = id
	}
	if q.Limit < 0 {
		return fmt.Errorf("limit must be non-negative")
	}
	if q.Limit == 0 {
		q.Limit = a.knowledge.PageSize()
	}
	// Tags are encoded separately in the public contract. The store applies
	// them as an additional filter when querying the cursor.
	docs, total, err := a.knowledge.List(q.ProfileID, q.LastID, q.Query, q.Tags, q.Limit)
	if err != nil {
		return err
	}
	next := ""
	if len(docs) > 0 {
		next = docs[len(docs)-1].ID
	}
	tags, err := a.knowledge.Tags(q.ProfileID)
	if err != nil {
		return err
	}
	return ev.Encode(map[string]any{"docs": docs, "total": total, "tags": tags, "hasMore": len(docs) == q.Limit, "nextLastId": next, "quotaBytes": a.kbQuotaBytes()})
}

const defaultKBQuotaBytes int64 = 25 * 1048576

func (a *App) kbQuotaBytes() int64 {
	if a.store != nil {
		if raw, err := a.store.Get("kb.quota_bytes"); err == nil {
			if value, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64); err == nil && value > 0 {
				return value
			}
		}
	}
	return defaultKBQuotaBytes
}
func (a *App) kbUpsert(ev event.Event, _ ws.Meta) error {
	var q models.KBDoc
	if err := ev.Decode(&q); err != nil {
		return err
	}
	if q.ProfileID == "" {
		id, err := a.activeProfile()
		if err != nil {
			return err
		}
		q.ProfileID = id
	}
	if q.ID == "" {
		q.ID = entityID("kb")
	}
	item, err := a.knowledge.Upsert(q)
	if err != nil {
		return err
	}
	return ev.Encode(item)
}
func (a *App) kbImportLink(ev event.Event, meta ws.Meta) error {
	var q struct {
		URL   string   `json:"url"`
		Title string   `json:"title"`
		Tags  []string `json:"tags"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	if q.URL == "" {
		return fmt.Errorf("url is required")
	}
	req, err := http.NewRequestWithContext(meta.Context(), http.MethodGet, q.URL, nil)
	if err != nil {
		return err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close() //nolint:errcheck // cleanup errors cannot be returned from this scope
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("document URL returned HTTP %d", res.StatusCode)
	}
	body, err := utils.ReadAllResponse(res.Body)
	if err != nil {
		return err
	}
	if q.Title == "" {
		q.Title = path.Base(req.URL.Path)
		if q.Title == "" || q.Title == "/" {
			q.Title = req.URL.Host
		}
	}
	profile, err := a.activeProfile()
	if err != nil {
		return err
	}
	doc, err := a.knowledge.Upsert(models.KBDoc{ID: entityID("kb"), ProfileID: profile, Title: q.Title, Tags: q.Tags, Source: q.URL, Kind: "link", Content: string(body)})
	if err != nil {
		return err
	}
	return ev.Encode(doc)
}
func (a *App) kbScanFolder(ev event.Event, _ ws.Meta) error {
	var q struct {
		Path string `json:"path"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	root := filepath.Clean(q.Path)
	allowed := false
	for _, e := range a.workspaces.List() {
		if filepath.Clean(e.Path) == root {
			allowed = true
			break
		}
	}
	if !allowed {
		return fmt.Errorf("folder must be an opened workspace")
	}
	out := []map[string]any{}
	err := filepath.WalkDir(root, func(name string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(name))
		if !map[string]bool{".md": true, ".mdc": true, ".txt": true, ".yaml": true, ".yml": true, ".json": true, ".go": true, ".html": true, ".js": true, ".jsx": true, ".ts": true, ".tsx": true, ".tsc": true}[ext] {
			return nil
		}
		rel, _ := filepath.Rel(root, name)
		info, _ := entry.Info()
		out = append(out, map[string]any{"path": filepath.ToSlash(rel), "name": entry.Name(), "ext": strings.TrimPrefix(ext, "."), "size": info.Size()})
		return nil
	})
	if err != nil {
		return err
	}
	return ev.Encode(out)
}
func (a *App) kbImportFiles(ev event.Event, _ ws.Meta) error {
	var q struct {
		Folder string   `json:"folder"`
		Files  []string `json:"files"`
		Title  string   `json:"title"`
		Tags   []string `json:"tags"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	root := filepath.Clean(q.Folder)
	profile, err := a.activeProfile()
	if err != nil {
		return err
	}
	imported := 0
	for _, rel := range q.Files {
		clean := filepath.Clean(rel)
		if clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) || filepath.IsAbs(clean) {
			return fmt.Errorf("file path is outside folder")
		}
		body, e := os.ReadFile(filepath.Join(root, clean))
		if e != nil {
			return e
		}
		title := strings.TrimSuffix(filepath.Base(clean), filepath.Ext(clean))
		if len(q.Files) == 1 && q.Title != "" {
			title = q.Title
		}
		if _, e = a.knowledge.Upsert(models.KBDoc{ID: entityID("kb"), ProfileID: profile, Title: title, Tags: q.Tags, Source: filepath.ToSlash(clean), Kind: "doc", Content: string(body)}); e != nil {
			return e
		}
		imported++
	}
	return ev.Encode(map[string]int{"imported": imported})
}
func (a *App) kbDelete(ev event.Event, _ ws.Meta) error {
	var q struct {
		ID        string `json:"id"`
		ProfileID string `json:"profileId"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	if q.ProfileID == "" {
		id, err := a.activeProfile()
		if err != nil {
			return err
		}
		q.ProfileID = id
	}
	if err := a.knowledge.Delete(q.ProfileID, q.ID); err != nil {
		return err
	}
	return ev.Encode(map[string]bool{"ok": true})
}
func (a *App) kbReindex(ev event.Event, _ ws.Meta) error {
	var q struct {
		ProfileID string `json:"profileId"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	if q.ProfileID == "" {
		id, err := a.activeProfile()
		if err != nil {
			return err
		}
		q.ProfileID = id
	}
	count, err := a.knowledge.Reindex(q.ProfileID)
	if err != nil {
		return err
	}
	return ev.Encode(map[string]int{"indexed": count, "chunks": count})
}
