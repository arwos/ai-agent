/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package app

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"path"
	"path/filepath"
	"strings"
	"time"

	mime2 "go.osspkg.com/goppy/v3/pkg/mime"

	"go.osspkg.com/goppy/v3/plugins/ws"
	"go.osspkg.com/goppy/v3/plugins/ws/event"

	"github.com/arwos/ai-agent/internal/pkg/dialog"
	"github.com/arwos/ai-agent/internal/pkg/models"
	"github.com/arwos/ai-agent/internal/pkg/workspace"
)

func (a *App) filesList(ev event.Event, _ ws.Meta) error {
	var q struct {
		WorkspaceID string `json:"workspaceId"`
		Dir         string `json:"dir"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	if q.WorkspaceID == "" {
		q.WorkspaceID = "default"
	}
	w, err := a.workspaces.Get(q.WorkspaceID)
	if err != nil {
		return err
	}
	items, err := w.ListFileInfo(q.Dir)
	if err != nil {
		return err
	}
	return ev.Encode(items)
}
func (a *App) filesRead(ev event.Event, _ ws.Meta) error {
	var q struct {
		WorkspaceID string `json:"workspaceId"`
		FileID      string `json:"fileId"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	if q.WorkspaceID == "" {
		q.WorkspaceID = "default"
	}
	if q.FileID == "" {
		return fmt.Errorf("fileId is required")
	}
	w, err := a.workspaces.Get(q.WorkspaceID)
	if err != nil {
		return err
	}
	const maxPreviewSize int64 = 5 << 20
	fileSize, err := w.FileSize(q.FileID)
	if err != nil {
		return err
	}
	if fileSize > maxPreviewSize {
		return fmt.Errorf("file %q is too large to preview (maximum is 5 MiB)", q.FileID)
	}
	body, err := w.ReadFileBytes(q.FileID)
	if err != nil {
		return err
	}
	contentType := mime2.DetectByFilename(q.FileID)
	ext := strings.TrimPrefix(strings.ToLower(path.Ext(q.FileID)), ".")
	image := strings.HasPrefix(contentType, "image/")
	if image || contentType == "application/octet-stream" {
		return ev.Encode(map[string]any{"id": q.FileID, "name": path.Base(q.FileID), "ext": ext, "content": "", "data": "data:" + http.DetectContentType(body) + ";base64," + base64.StdEncoding.EncodeToString(body), "size": len(body), "kind": "binary"})
	}
	return ev.Encode(map[string]any{"id": q.FileID, "name": path.Base(q.FileID), "ext": ext, "content": string(body), "size": len(body), "kind": "text"})
}
func (a *App) filesWrite(ev event.Event, _ ws.Meta) error {
	var q struct {
		WorkspaceID string `json:"workspaceId"`
		FileID      string `json:"fileId"`
		Content     string `json:"content"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	if q.WorkspaceID == "" {
		q.WorkspaceID = "default"
	}
	if q.FileID == "" {
		return fmt.Errorf("fileId is required")
	}
	w, err := a.workspaces.Get(q.WorkspaceID)
	if err != nil {
		return err
	}
	if err = w.WriteFile(q.FileID, q.Content); err != nil {
		return err
	}
	return ev.Encode(map[string]any{"id": q.FileID, "name": path.Base(q.FileID), "content": q.Content, "size": len(q.Content)})
}
func (a *App) filesAdd(ev event.Event, _ ws.Meta) error {
	var q struct {
		WorkspaceID string `json:"workspaceId"`
		Name        string `json:"name"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	if q.WorkspaceID == "" {
		q.WorkspaceID = "default"
	}
	if q.Name == "" {
		return fmt.Errorf("name is required")
	}
	w, err := a.workspaces.Get(q.WorkspaceID)
	if err != nil {
		return err
	}
	if err = w.WriteFile(q.Name, ""); err != nil {
		return err
	}
	return ev.Encode(map[string]any{"id": q.Name, "name": path.Base(q.Name), "content": "", "size": 0})
}
func (a *App) filesRemove(ev event.Event, _ ws.Meta) error {
	var q struct {
		WorkspaceID string `json:"workspaceId"`
		FileID      string `json:"fileId"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	if q.WorkspaceID == "" {
		q.WorkspaceID = "default"
	}
	if q.FileID == "" {
		return fmt.Errorf("fileId is required")
	}
	w, err := a.workspaces.Get(q.WorkspaceID)
	if err != nil {
		return err
	}
	if err = w.RemoveFile(q.FileID); err != nil {
		return err
	}
	return ev.Encode(map[string]bool{"ok": true})
}
func (a *App) workspaceList(ev event.Event, _ ws.Meta) error {
	var q struct {
		WorkspaceID string `json:"workspace_id"`
		Dir         string `json:"dir"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	if q.WorkspaceID == "" {
		q.WorkspaceID = "default"
	}
	w, err := a.workspaces.Get(q.WorkspaceID)
	if err != nil {
		return err
	}
	files, err := w.ListFiles(q.Dir)
	if err != nil {
		return err
	}
	return ev.Encode(files)
}
func (a *App) workspaceRead(ev event.Event, _ ws.Meta) error {
	var q struct {
		WorkspaceID string `json:"workspace_id"`
		Path        string `json:"path"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	if q.Path == "" {
		return fmt.Errorf("path is required")
	}
	if q.WorkspaceID == "" {
		q.WorkspaceID = "default"
	}
	w, err := a.workspaces.Get(q.WorkspaceID)
	if err != nil {
		return err
	}
	content, err := w.ReadFile(q.Path)
	if err != nil {
		return err
	}
	return ev.Encode(content)
}
func (a *App) workspaceWrite(ev event.Event, _ ws.Meta) error {
	var q struct {
		WorkspaceID string `json:"workspace_id"`
		Path        string `json:"path"`
		Content     string `json:"content"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	if q.Path == "" {
		return fmt.Errorf("path is required")
	}
	if q.WorkspaceID == "" {
		q.WorkspaceID = "default"
	}
	w, err := a.workspaces.Get(q.WorkspaceID)
	if err != nil {
		return err
	}
	if err := w.WriteFile(q.Path, q.Content); err != nil {
		return err
	}
	return ev.Encode(map[string]bool{"ok": true})
}
func (a *App) workspaceOpen(ev event.Event, _ ws.Meta) error {
	var q struct {
		ID   string `json:"id"`
		Path string `json:"path"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	entry, err := a.workspaces.Open(q.ID, q.Path)
	if err != nil {
		return err
	}
	if a.store != nil {
		if err := a.persistWorkspace(models.Workspace{ID: entry.ID, Name: filepath.Base(entry.Path), FolderPath: entry.Path}); err != nil {
			return err
		}
	}
	return ev.Encode(a.workspaceView(entry))
}
func (a *App) workspaceCreate(ev event.Event, _ ws.Meta) error {
	var q struct {
		Name       string `json:"name"`
		FolderPath string `json:"folderPath"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	if q.FolderPath == "" {
		return fmt.Errorf("folderPath is required")
	}
	// Folder display names are not IDs. Always allocate the next free ID so
	// directories such as /projects/a/app and /projects/b/app can coexist.
	entry, err := a.workspaces.OpenNext(q.FolderPath)
	if err != nil {
		return err
	}
	if a.store != nil {
		if err := a.persistWorkspace(models.Workspace{ID: entry.ID, Name: filepath.Base(entry.Path), FolderPath: entry.Path}); err != nil {
			return err
		}
	}
	return ev.Encode(a.workspaceView(entry))
}
func (a *App) workspaceGet(ev event.Event, _ ws.Meta) error {
	var q struct {
		ID string `json:"id"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	if q.ID == "" {
		return fmt.Errorf("workspace id is required")
	}
	entry, err := a.workspaces.Entry(q.ID)
	if err != nil {
		return err
	}
	return ev.Encode(a.workspaceView(entry))
}
func (a *App) workspacePick(ev event.Event, meta ws.Meta) error {
	if a.picker == nil {
		return fmt.Errorf("native folder picker is unavailable")
	}
	path, err := a.picker.Directory(meta.Context())
	if err != nil {
		return err
	}
	entry, err := a.workspaces.OpenNext(path)
	if err != nil {
		return err
	}
	if a.store != nil {
		if err := a.persistWorkspace(models.Workspace{ID: entry.ID, Name: filepath.Base(entry.Path), FolderPath: entry.Path}); err != nil {
			return err
		}
	}
	return ev.Encode(a.workspaceView(entry))
}
func (a *App) workspacePickStart(ev event.Event, _ ws.Meta) error {
	if a.picker == nil {
		return fmt.Errorf("native folder picker is unavailable")
	}
	id := entityID("workspace-pick")
	a.pickMu.Lock()
	a.pickJobs[id] = &pickJob{}
	a.pickMu.Unlock()
	go func() {
		path, err := a.picker.Directory(context.Background())
		var result any
		var message string
		if err != nil {
			message = err.Error()
		} else if entry, openErr := a.workspaces.OpenNext(path); openErr != nil {
			message = openErr.Error()
		} else {
			if a.store != nil {
				if saveErr := a.persistWorkspace(models.Workspace{ID: entry.ID, Name: filepath.Base(entry.Path), FolderPath: entry.Path}); saveErr != nil {
					message = saveErr.Error()
				}
			}
			result = a.workspaceView(entry)
		}
		a.pickMu.Lock()
		if job := a.pickJobs[id]; job != nil {
			job.done, job.result, job.errText = true, result, message
		}
		a.pickMu.Unlock()
	}()
	return ev.Encode(map[string]string{"operationId": id})
}
func (a *App) workspacePickStatus(ev event.Event, _ ws.Meta) error {
	var q struct {
		OperationID string `json:"operationId"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	a.pickMu.RLock()
	job := a.pickJobs[q.OperationID]
	if job == nil {
		a.pickMu.RUnlock()
		return fmt.Errorf("pick operation not found")
	}
	done, result, message := job.done, job.result, job.errText
	a.pickMu.RUnlock()
	if !done {
		return ev.Encode(map[string]any{"status": "pending"})
	}
	a.pickMu.Lock()
	delete(a.pickJobs, q.OperationID)
	a.pickMu.Unlock()
	if message != "" {
		return ev.Encode(map[string]any{"status": "error", "message": message})
	}
	return ev.Encode(map[string]any{"status": "completed", "workspace": result})
}
func (a *App) workspaceClose(ev event.Event, _ ws.Meta) error {
	var q struct {
		ID string `json:"id"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	if q.ID == "" {
		return fmt.Errorf("workspace id is required")
	}
	if a.store != nil {
		if conversations, err := a.store.WorkspaceConversations(q.ID); err != nil {
			return err
		} else {
			for _, conversation := range conversations {
				_ = a.dialogs.Delete(dialog.SessionKey(conversation.ProfileID, conversation.ID))
				_ = a.memory.Delete(conversation.ProfileID, q.ID, conversation.ID)
			}
		}
		profile, err := a.activeProfile()
		if err != nil {
			return err
		}
		if err := a.store.WorkspaceDelete(profile, q.ID); err != nil {
			return err
		}
	}
	if err := a.workspaces.Close(q.ID); err != nil {
		return err
	}
	return ev.Encode(map[string]bool{"ok": true})
}
func (a *App) workspaceListOpen(ev event.Event, _ ws.Meta) error {
	entries := a.workspaces.List()
	if a.store != nil {
		profile, err := a.activeProfile()
		if err != nil {
			return err
		}
		saved, err := a.store.Workspaces(profile)
		if err != nil {
			return err
		}
		allowed := make(map[string]bool, len(saved))
		for _, item := range saved {
			allowed[item.ID] = true
		}
		filtered := entries[:0]
		for _, entry := range entries {
			if allowed[entry.ID] {
				filtered = append(filtered, entry)
			}
		}
		entries = filtered
	}
	items := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		items = append(items, a.workspaceView(entry))
	}
	return ev.Encode(items)
}
func (a *App) persistWorkspace(item models.Workspace) error {
	if a.store == nil {
		return nil
	}
	profile, err := a.activeProfile()
	if err != nil {
		return err
	}
	return a.store.WorkspaceUpsert(profile, item)
}
func (a *App) workspaceView(entry workspace.Entry) map[string]any {
	name := filepath.Base(entry.Path)
	if name == "." || name == string(filepath.Separator) || name == "" {
		name = entry.ID
	}
	fileCount := 0
	if service, err := a.workspaces.Get(entry.ID); err == nil {
		if files, err := service.ListFileInfo(""); err == nil {
			fileCount = len(files)
		}
	}
	createdAt := ""
	if !entry.OpenedAt.IsZero() {
		createdAt = entry.OpenedAt.Format(time.RFC3339)
	}
	return map[string]any{
		"id": entry.ID, "name": name, "folderPath": entry.Path,
		"createdAt": createdAt, "files": []any{}, "fileCount": fileCount,
	}
}
