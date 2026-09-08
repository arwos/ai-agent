/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package app

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"go.osspkg.com/goppy/v3/plugins/ws"
	"go.osspkg.com/goppy/v3/plugins/ws/event"

	"github.com/arwos/ai-agent/internal/pkg/models"
	"github.com/arwos/ai-agent/internal/pkg/utils"
)

func (a *App) managedSkillsList(ev event.Event, _ ws.Meta) error {
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
	items, err := a.skills.List(q.ProfileID)
	if err != nil {
		return err
	}
	return ev.Encode(items)
}
func (a *App) skillsOpenFolder(ev event.Event, _ ws.Meta) error {
	var q struct {
		ID string `json:"id"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	if q.ID == "" {
		return fmt.Errorf("skill id is required")
	}
	profile, err := a.activeProfile()
	if err != nil {
		return err
	}
	if err := a.skills.OpenFolder(profile, q.ID); err != nil {
		return err
	}
	return ev.Encode(map[string]any{"ok": true})
}
func (a *App) managedSkillsUpsert(ev event.Event, _ ws.Meta) error {
	var payload map[string]json.RawMessage
	if err := ev.Decode(&payload); err != nil {
		return err
	}
	var request struct {
		ID            string          `json:"id"`
		Patch         json.RawMessage `json:"patch"`
		Dependencies  []string        `json:"dependencies"`
		DependencyIDs []string        `json:"dependencyIds"`
	}
	_ = json.Unmarshal(payload["id"], &request.ID)
	_ = json.Unmarshal(payload["patch"], &request.Patch)
	_ = json.Unmarshal(payload["dependencies"], &request.Dependencies)
	_ = json.Unmarshal(payload["dependencyIds"], &request.DependencyIDs)
	var q models.Skill
	if request.ID != "" {
		profileID, err := a.activeProfile()
		if err != nil {
			return err
		}
		items, err := a.skills.List(profileID)
		if err != nil {
			return err
		}
		for _, item := range items {
			if item.ID == request.ID {
				q = item
				break
			}
		}
		if q.ID == "" {
			return fmt.Errorf("skill %q not found", request.ID)
		}
		if err = applyPatch(&q, request.Patch); err != nil {
			return err
		}
	} else {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(encoded, &q); err != nil {
			return err
		}
	}
	if q.ProfileID == "" {
		id, err := a.activeProfile()
		if err != nil {
			return err
		}
		q.ProfileID = id
	}
	if q.ID == "" {
		q.ID = entityID("skill")
	}
	item, err := a.skills.Upsert(q)
	if err != nil {
		return err
	}
	return ev.Encode(item)
}
func (a *App) managedSkillsDelete(ev event.Event, _ ws.Meta) error {
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
	if err := a.skills.Delete(q.ProfileID, q.ID); err != nil {
		return err
	}
	return ev.Encode(map[string]bool{"ok": true})
}
func (a *App) skillsDiscover(ev event.Event, meta ws.Meta) error {
	var q struct {
		Source string `json:"source"`
		Ref    string `json:"ref"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	if q.Ref == "" {
		return fmt.Errorf("ref is required")
	}
	if q.Source == "link" {
		if isArchiveURL(q.Ref) {
			if err := validateExternalURL(q.Ref); err != nil {
				return err
			}
			req, err := http.NewRequestWithContext(meta.Context(), http.MethodGet, q.Ref, nil)
			if err != nil {
				return err
			}
			response, err := externalHTTPClient().Do(req)
			if err != nil {
				return err
			}
			defer response.Body.Close() //nolint:errcheck // cleanup errors cannot be returned from this scope
			if response.StatusCode < 200 || response.StatusCode >= 300 {
				return fmt.Errorf("skill archive returned HTTP %d", response.StatusCode)
			}
			archive, err := utils.ReadAllResponse(response.Body)
			if err != nil {
				return err
			}
			archiveKind := ".zip"
			if strings.HasSuffix(strings.ToLower(q.Ref), ".tar.gz") {
				archiveKind = ".tar.gz"
			} else if strings.HasSuffix(strings.ToLower(q.Ref), ".tar") {
				archiveKind = ".tar"
			}
			root, err := extractSkillArchive(archive, archiveKind)
			if err != nil {
				return err
			}
			defer os.RemoveAll(root) //nolint:errcheck // cleanup errors cannot be returned from this scope
			return discoverSkillDirectory(ev, root, "")
		}
		// A repository URL is not a skill document. Clone it to a temporary
		// directory and discover every Markdown skill recursively.
		if isGitRepositoryURL(q.Ref) {
			tmp, err := os.MkdirTemp("", "arwos-skills-")
			if err != nil {
				return err
			}
			defer os.RemoveAll(tmp)                                                                  //nolint:errcheck // cleanup errors cannot be returned from this scope
			clone := exec.CommandContext(meta.Context(), "git", "clone", "--depth", "1", q.Ref, tmp) //nolint:gosec // validated input or bounded archive is required here
			if output, err := clone.CombinedOutput(); err != nil {
				return fmt.Errorf("clone skill repository: %w: %s", err, strings.TrimSpace(string(output)))
			}
			return discoverSkillDirectory(ev, tmp, q.Ref)
		}
		requestURL := rawGitFileURL(q.Ref)
		if err := validateExternalURL(requestURL); err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(meta.Context(), http.MethodGet, requestURL, nil)
		if err != nil {
			return err
		}
		response, err := externalHTTPClient().Do(req)
		if err != nil {
			return err
		}
		defer response.Body.Close() //nolint:errcheck // cleanup errors cannot be returned from this scope
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return fmt.Errorf("skill URL returned HTTP %d", response.StatusCode)
		}
		body, err := utils.ReadAllResponse(response.Body)
		if err != nil {
			return err
		}
		if !isSkillDocument(body) {
			return fmt.Errorf("skill document must contain name and description frontmatter")
		}
		return ev.Encode([]map[string]any{discoveredSkill(path.Base(requestURL), string(body), q.Ref)})
	}
	if q.Source != "directory" {
		return fmt.Errorf("unsupported skill source %q", q.Source)
	}
	root := filepath.Clean(q.Ref)
	out := make([]map[string]any, 0)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if !isSkillFile(entry.Name()) {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !isSkillDocument(body) {
			return nil
		}
		out = append(out, discoveredSkill(filepath.Base(path), string(body), path))
		return nil
	})
	if err != nil {
		return err
	}
	if len(out) == 0 {
		return fmt.Errorf("no skill documents with name and description frontmatter were found")
	}
	return ev.Encode(out)
}
func discoverSkillDirectory(ev event.Event, root, sourceRef string) error {
	out := make([]map[string]any, 0)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !isSkillFile(entry.Name()) {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !isSkillDocument(body) {
			return nil
		}
		item := discoveredSkill(entry.Name(), string(body), path)
		if sourceRef != "" {
			if relative, err := filepath.Rel(root, path); err == nil {
				item["sourceRef"] = sourceRef
				item["sourcePath"] = filepath.ToSlash(relative)
			}
		}
		out = append(out, item)
		return nil
	})
	if err != nil {
		return err
	}
	if len(out) == 0 {
		return fmt.Errorf("no skill documents with name and description frontmatter were found")
	}
	return ev.Encode(out)
}
func (a *App) skillsImportMany(ev event.Event, _ ws.Meta) error {
	var q struct {
		Items []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Icon        string `json:"icon"`
			Accent      string `json:"accent"`
			Content     string `json:"content"`
			Origin      string `json:"origin"`
			SourceRef   string `json:"sourceRef"`
			SourcePath  string `json:"sourcePath"`
		} `json:"items"`
		Source string `json:"source"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	profileID, err := a.activeProfile()
	if err != nil {
		return err
	}
	created := make([]models.Skill, 0, len(q.Items))
	var gitRoot string
	if q.Source == "link" && len(q.Items) > 0 && q.Items[0].SourceRef != "" {
		if !isGitRepositoryURL(q.Items[0].SourceRef) {
			return fmt.Errorf("skill repository URL is invalid")
		}
		gitRoot, err = os.MkdirTemp("", "arwos-skills-import-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(gitRoot)                                                                                       //nolint:errcheck // cleanup errors cannot be returned from this scope
		clone := exec.CommandContext(context.Background(), "git", "clone", "--depth", "1", q.Items[0].SourceRef, gitRoot) //nolint:gosec // validated input or bounded archive is required here
		if output, cloneErr := clone.CombinedOutput(); cloneErr != nil {
			return fmt.Errorf("clone skill repository: %w: %s", cloneErr, strings.TrimSpace(string(output)))
		}
	}
	for _, item := range q.Items {
		if item.Name == "" || item.Content == "" {
			return fmt.Errorf("imported skill must have name and content")
		}
		if gitRoot != "" {
			if item.SourceRef != q.Items[0].SourceRef {
				return fmt.Errorf("all imported skills must use the discovered repository")
			}
			cleanPath := filepath.Clean(filepath.FromSlash(item.SourcePath))
			if cleanPath == "." || cleanPath == ".." || strings.HasPrefix(cleanPath, ".."+string(filepath.Separator)) || filepath.IsAbs(cleanPath) {
				return fmt.Errorf("skill source path is outside repository")
			}
		}
		sourceRef := item.Origin
		if q.Source == "link" {
			sourceRef = item.SourceRef
		}
		out, err := a.skills.Upsert(models.Skill{ID: entityID("skill"), ProfileID: profileID, Name: item.Name, Description: item.Description, Content: item.Content, Icon: item.Icon, Accent: item.Accent, Enabled: true, Source: q.Source, SourceRef: sourceRef})
		if err != nil {
			return err
		}
		if q.Source == "directory" {
			if err := a.skills.AttachDirectory(profileID, out.ID, filepath.Dir(item.Origin)); err != nil {
				return err
			}
		} else if gitRoot != "" {
			if err := a.skills.AttachDirectory(profileID, out.ID, filepath.Dir(filepath.Join(gitRoot, filepath.FromSlash(item.SourcePath)))); err != nil {
				return err
			}
		}
		created = append(created, out)
	}
	return ev.Encode(created)
}
func (a *App) skillGroupsList(ev event.Event, _ ws.Meta) error {
	profile, err := a.activeProfile()
	if err != nil {
		return err
	}
	groups, err := a.skills.Groups(profile)
	if err != nil {
		return err
	}
	return ev.Encode(groups)
}
func (a *App) skillGroupSave(ev event.Event, _ ws.Meta) error {
	var group models.SkillGroup
	if err := ev.Decode(&group); err != nil {
		return err
	}
	profile, err := a.activeProfile()
	if err != nil {
		return err
	}
	if group.ID == "" {
		group.ID = entityID("skill-group")
	}
	group.ProfileID = profile
	groups, err := a.skills.Groups(profile)
	if err != nil {
		return err
	}
	found := false
	for i := range groups {
		if groups[i].ID == group.ID {
			groups[i] = group
			found = true
			break
		}
	}
	if !found {
		groups = append(groups, group)
	}
	if err := a.skills.SaveGroups(profile, groups); err != nil {
		return err
	}
	return ev.Encode(group)
}
func (a *App) skillGroupDelete(ev event.Event, _ ws.Meta) error {
	var q struct {
		ID string `json:"id"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	profile, err := a.activeProfile()
	if err != nil {
		return err
	}
	groups, err := a.skills.Groups(profile)
	if err != nil {
		return err
	}
	out := groups[:0]
	for _, group := range groups {
		if group.ID != q.ID {
			out = append(out, group)
		}
	}
	if err := a.skills.SaveGroups(profile, out); err != nil {
		return err
	}
	return ev.Encode(map[string]any{"ok": true})
}
func (a *App) skillGroupAssign(ev event.Event, _ ws.Meta) error {
	var q struct {
		SkillID string `json:"skillId"`
		GroupID string `json:"groupId"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	profile, err := a.activeProfile()
	if err != nil {
		return err
	}
	if err := a.skills.SetSkillGroup(profile, q.SkillID, q.GroupID); err != nil {
		return err
	}
	return ev.Encode(map[string]any{"ok": true})
}
func isSkillFile(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return ext == ".md" || ext == ".mdc"
}

var skillFrontmatterRE = regexp.MustCompile(`(?ms)^---[ \t]*\r?\n(.*?)\r?\n---[ \t]*(?:\r?\n|$)`)
var skillNameRE = regexp.MustCompile(`(?mi)^\s*name\s*:\s*(?:"([^"]*)"|'([^']*)'|([^\r\n]*))\s*$`)
var skillDescriptionRE = regexp.MustCompile(`(?mi)^\s*description\s*:\s*(?:"([^"]*)"|'([^']*)'|([^\r\n]*))\s*$`)
var skillFoldedDescriptionRE = regexp.MustCompile(`(?ms)^\s*description\s*:\s*[>|][+-]?\s*\r?\n((?:[ \t]+[^\r\n]*(?:\r?\n|$))+)`)

func isGitRepositoryURL(value string) bool {
	clean := strings.TrimSuffix(strings.TrimSpace(value), "/")
	if strings.HasSuffix(clean, ".git") {
		return true
	}
	u, err := url.Parse(clean)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if host != "github.com" && host != "gitlab.com" && host != "bitbucket.org" {
		return false
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	return len(parts) == 2
}
func isArchiveURL(value string) bool {
	u, err := url.Parse(value)
	if err != nil {
		return false
	}
	name := strings.ToLower(u.Path)
	return strings.HasSuffix(name, ".zip") || strings.HasSuffix(name, ".tar") || strings.HasSuffix(name, ".tar.gz")
}
func extractSkillArchive(data []byte, kind string) (string, error) {
	root, err := os.MkdirTemp("", "arwos-skills-archive-")
	if err != nil {
		return "", err
	}
	removeOnError := true
	defer func() {
		if removeOnError {
			_ = os.RemoveAll(root)
		}
	}()
	writeFile := func(name string, directory bool, reader io.Reader) error {
		rel := filepath.Clean(filepath.FromSlash(name))
		if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("skill archive contains unsafe path %q", name)
		}
		target := filepath.Join(root, rel)
		if directory {
			if err := os.MkdirAll(target, 0755); err != nil {
				return err
			}
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		content, readErr := utils.ReadAllResponse(reader)
		if readErr != nil {
			return readErr
		}
		if err := os.WriteFile(target, content, 0644); err != nil {
			return err
		}
		return nil
	}
	if kind == ".zip" {
		archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return "", fmt.Errorf("read skill archive: %w", err)
		}
		for _, file := range archive.File {
			input, err := file.Open()
			if err != nil {
				return "", err
			}
			err = writeFile(file.Name, file.FileInfo().IsDir(), input)
			input.Close() //nolint:errcheck // cleanup errors cannot be returned from this scope
			if err != nil {
				return "", err
			}
		}
	} else {
		var reader io.Reader = bytes.NewReader(data)
		if kind == ".tar.gz" {
			gz, err := gzip.NewReader(bytes.NewReader(data))
			if err != nil {
				return "", fmt.Errorf("read gzip skill archive: %w", err)
			}
			defer gz.Close() //nolint:errcheck // cleanup errors cannot be returned from this scope
			reader = gz
		}
		tarReader := tar.NewReader(reader)
		for {
			header, err := tarReader.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return "", fmt.Errorf("read tar skill archive: %w", err)
			}
			if err := writeFile(header.Name, header.FileInfo().IsDir(), tarReader); err != nil {
				return "", err
			}
		}
	}
	removeOnError = false
	return root, nil
}
func rawGitFileURL(value string) string {
	u, err := url.Parse(value)
	if err != nil || strings.ToLower(u.Hostname()) != "github.com" {
		return value
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 5 || parts[2] != "blob" {
		return value
	}
	rawPath := strings.Join(append([]string{parts[0], parts[1]}, parts[3:]...), "/")
	return "https://raw.githubusercontent.com/" + rawPath
}
func skillMetadata(body []byte) (string, string, bool) {
	match := skillFrontmatterRE.FindSubmatch(body)
	if len(match) == 0 {
		return "", "", false
	}
	value := func(re *regexp.Regexp) string {
		parts := re.FindStringSubmatch(string(match[1]))
		if len(parts) == 0 {
			return ""
		}
		for _, part := range parts[1:] {
			if strings.TrimSpace(part) != "" {
				return strings.TrimSpace(part)
			}
		}
		return ""
	}
	name := value(skillNameRE)
	description := value(skillDescriptionRE)
	if folded := skillFoldedDescriptionRE.FindStringSubmatch(string(match[1])); len(folded) > 1 {
		description = strings.Join(strings.Fields(folded[1]), " ")
	}
	return name, description, name != "" && description != ""
}
func isSkillDocument(body []byte) bool {
	_, _, ok := skillMetadata(body)
	return ok
}
func discoveredSkill(name, content, origin string) map[string]any {
	skillName := strings.TrimSuffix(name, filepath.Ext(name))
	description := ""
	if parsedName, parsedDescription, ok := skillMetadata([]byte(content)); ok {
		skillName, description = parsedName, parsedDescription
	}
	return map[string]any{"tempId": entityID("discovered"), "name": skillName, "description": description, "icon": "bot", "accent": "indigo", "checked": true, "origin": origin, "content": content}
}
func (a *App) skillsList(ev event.Event, _ ws.Meta) error {
	var q struct {
		Cursor string `json:"cursor"`
		Limit  int    `json:"limit"`
		Query  string `json:"query"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	profileID, err := a.activeProfile()
	if err != nil {
		return err
	}
	// Compatibility callers without paging still receive the full catalogue.
	// The settings UI explicitly sends limit=20 and uses the file-name cursor.
	if q.Limit > 0 || q.Cursor != "" || strings.TrimSpace(q.Query) != "" {
		if q.Limit <= 0 {
			q.Limit = 20
		}
		if q.Limit > 100 {
			q.Limit = 100
		}
		page, err := a.skills.SearchPage(profileID, q.Query, q.Cursor, q.Limit)
		if err != nil {
			return err
		}
		return ev.Encode(page)
	}
	items, err := a.skills.List(profileID)
	if err != nil {
		return err
	}
	return ev.Encode(items)
}
func (a *App) skillsReindex(ev event.Event, _ ws.Meta) error {
	profile, err := a.activeProfile()
	if err != nil {
		return err
	}
	count, err := a.skills.Reindex(profile)
	if err != nil {
		return err
	}
	return ev.Encode(map[string]int{"count": count})
}
func (a *App) skillsPickStart(ev event.Event, _ ws.Meta) error {
	if a.picker == nil {
		return fmt.Errorf("native folder picker is unavailable")
	}
	id := entityID("skills-pick")
	a.pickMu.Lock()
	a.pickJobs[id] = &pickJob{}
	a.pickMu.Unlock()
	go func() {
		path, err := a.picker.Directory(context.Background())
		a.pickMu.Lock()
		if job := a.pickJobs[id]; job != nil {
			job.done = true
			if err != nil {
				job.errText = err.Error()
			} else {
				job.result = path
			}
		}
		a.pickMu.Unlock()
	}()
	return ev.Encode(map[string]string{"operationId": id})
}
func (a *App) skillsPickStatus(ev event.Event, _ ws.Meta) error {
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
	return ev.Encode(map[string]any{"status": "completed", "path": result})
}
func (a *App) filesystemSkillsList(ev event.Event, _ ws.Meta) error {
	items, err := a.skills.FilesystemList()
	if err != nil {
		return err
	}
	return ev.Encode(items)
}
func (a *App) skillsGet(ev event.Event, _ ws.Meta) error {
	var q struct {
		Name      string `json:"name"`
		File      string `json:"file"`
		ListFiles bool   `json:"listFiles"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	if q.Name == "" {
		return fmt.Errorf("name is required")
	}
	profile, err := a.activeProfile()
	if err != nil {
		return err
	}
	if q.ListFiles {
		files, err := a.skills.Files(profile, q.Name)
		if err != nil {
			return err
		}
		return ev.Encode(files)
	}
	var content string
	if q.File == "" {
		content, err = a.skills.Get(profile, q.Name)
	} else {
		content, err = a.skills.ReadFile(profile, q.Name, q.File)
	}
	if err != nil {
		return err
	}
	return ev.Encode(content)
}
