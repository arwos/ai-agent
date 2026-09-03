/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package skills

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/search/query"
	"github.com/mozillazg/go-unidecode"
	"gopkg.in/yaml.v3"

	"github.com/arwos/ai-agent/internal/pkg/models"
)

type Skill struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Path        string `json:"path"`
}

// Service stores both managed skill metadata and its instruction body in SKILL.md.
// SQLite is not a source of truth for skills.
type Service struct {
	Root      string
	mu        sync.Mutex
	catalogMu sync.Mutex
	indexes   map[string]bleve.Index
}

var markdownFileReference = regexp.MustCompile(`\[[^\]]*\]\(([^)\s]+)(?:\s+[^)]*)?\)`)

type catalog struct {
	Version int            `yaml:"version"`
	Skills  []catalogSkill `yaml:"skills"`
}

type catalogSkill struct {
	ID          string   `yaml:"id"`
	ProfileID   string   `yaml:"profileId"`
	Name        string   `yaml:"name"`
	Description string   `yaml:"description,omitempty"`
	Icon        string   `yaml:"icon,omitempty"`
	Accent      string   `yaml:"accent,omitempty"`
	Source      string   `yaml:"source,omitempty"`
	SourceRef   string   `yaml:"sourceRef,omitempty"`
	Enabled     bool     `yaml:"enabled"`
	Path        string   `yaml:"path"`
	Files       []string `yaml:"files"`
}

type groupsFile struct {
	Version int                 `yaml:"version"`
	Groups  []models.SkillGroup `yaml:"groups"`
}

func New(root string) *Service { return &Service{Root: root, indexes: make(map[string]bleve.Index)} }

// CanonicalName returns a portable skill identifier containing only
// lowercase ASCII letters, digits, and hyphens. Unicode input is transliterated
// before separators are normalized, so imports from any supported writing
// system cannot create an invalid skill directory.
func CanonicalName(value string) string {
	var out strings.Builder
	dash := false
	for _, r := range strings.ToLower(unidecode.Unidecode(strings.TrimSpace(value))) {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			dash = true
			continue
		}
		if dash && out.Len() > 0 {
			out.WriteByte('-')
		}
		dash = false
		out.WriteRune(r)
	}
	name := strings.Trim(out.String(), "-")
	if name != "" {
		return name
	}
	sum := sha256.Sum256([]byte(value))
	return fmt.Sprintf("skill-%x", sum[:4])
}

func safe(v string) bool {
	return v != "" && filepath.Base(v) == v && v != "." && v != ".." && !strings.ContainsAny(v, `/\\`)
}
func (s *Service) profileDir(profile string) string { return filepath.Join(s.Root, profile) }
func (s *Service) catalogPath(profile string) string {
	return filepath.Join(s.profileDir(profile), "index.yaml")
}
func (s *Service) groupsPath(profile string) string {
	return filepath.Join(s.profileDir(profile), "groups.yaml")
}

func (s *Service) Groups(profile string) ([]models.SkillGroup, error) {
	s.catalogMu.Lock()
	defer s.catalogMu.Unlock()
	return s.loadGroupsLocked(profile)
}

func (s *Service) loadGroupsLocked(profile string) ([]models.SkillGroup, error) {
	if !safe(profile) {
		return nil, fmt.Errorf("invalid profile id")
	}
	b, err := os.ReadFile(s.groupsPath(profile))
	if os.IsNotExist(err) {
		return []models.SkillGroup{}, nil
	}
	if err != nil {
		return nil, err
	}
	var f groupsFile
	if err := yaml.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("read skill groups: %w", err)
	}
	return f.Groups, nil
}

func (s *Service) SaveGroups(profile string, groups []models.SkillGroup) error {
	s.catalogMu.Lock()
	defer s.catalogMu.Unlock()
	if !safe(profile) {
		return fmt.Errorf("invalid profile id")
	}
	if err := os.MkdirAll(s.profileDir(profile), 0755); err != nil {
		return err
	}
	b, err := yaml.Marshal(groupsFile{Version: 1, Groups: groups})
	if err != nil {
		return err
	}
	tmp := s.groupsPath(profile) + ".tmp"
	if err := os.WriteFile(tmp, b, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, s.groupsPath(profile))
}

func (s *Service) SetSkillGroup(profile, skillID, groupID string) error {
	groups, err := s.loadGroupsLocked(profile)
	if err != nil {
		return err
	}
	for i := range groups {
		filtered := groups[i].SkillIDs[:0]
		for _, id := range groups[i].SkillIDs {
			if id != skillID {
				filtered = append(filtered, id)
			}
		}
		groups[i].SkillIDs = filtered
		if groups[i].ID == groupID {
			groups[i].SkillIDs = append(groups[i].SkillIDs, skillID)
		}
	}
	return s.saveGroupsLocked(profile, groups)
}

func (s *Service) saveGroupsLocked(profile string, groups []models.SkillGroup) error {
	if err := os.MkdirAll(s.profileDir(profile), 0755); err != nil {
		return err
	}
	b, err := yaml.Marshal(groupsFile{Version: 1, Groups: groups})
	if err != nil {
		return err
	}
	tmp := s.groupsPath(profile) + ".tmp"
	if err := os.WriteFile(tmp, b, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, s.groupsPath(profile))
}
func (s *Service) openProfileRoot(profile string) (*os.Root, error) {
	if !safe(profile) {
		return nil, fmt.Errorf("invalid profile id")
	}
	return os.OpenRoot(s.profileDir(profile))
}
func (s *Service) skillDir(x models.Skill) string {
	return filepath.Join(s.profileDir(x.ProfileID), x.Name)
}

func (s *Service) document(x models.Skill) ([]byte, error) {
	content := x.Content
	// Keep metadata in the instruction file as standard YAML front matter so
	// skills remain portable outside Arwos. Existing front matter is merged,
	// preserving custom fields while making name and description authoritative.
	if strings.HasPrefix(content, "---\n") {
		rest := content[4:]
		if at := strings.Index(rest, "\n---\n"); at >= 0 {
			fields := map[string]any{}
			if yaml.Unmarshal([]byte(rest[:at]), &fields) == nil {
				fields["name"], fields["description"] = x.Name, x.Description
				delete(fields, "user-invocable")
				delete(fields, "disable-model-invocation")
				front, err := yaml.Marshal(fields)
				if err != nil {
					return nil, err
				}
				return append(append([]byte("---\n"), append(front, []byte("---\n")...)...), []byte(rest[at+5:])...), nil
			}
		}
	}
	front, err := yaml.Marshal(map[string]any{"name": x.Name, "description": x.Description})
	if err != nil {
		return nil, err
	}
	result := append([]byte("---\n"), front...)
	result = append(result, []byte("---\n")...)
	return append(result, []byte(content)...), nil
}
func metadataDocument(x models.Skill) ([]byte, error) {
	meta := struct {
		ID, ProfileID, Name, Description, Icon, Accent, Source, SourceRef string
		Enabled                                                           bool
	}{x.ID, x.ProfileID, x.Name, x.Description, x.Icon, x.Accent, x.Source, x.SourceRef, x.Enabled}
	return json.MarshalIndent(meta, "", "  ")
}
func readMetadata(b []byte) (models.Skill, error) {
	var meta struct {
		ID          string `yaml:"id"`
		ProfileID   string `yaml:"profile-id"`
		Name        string `yaml:"name"`
		Description string `yaml:"description"`
		Icon        string `yaml:"icon"`
		Accent      string `yaml:"accent"`
		Source      string `yaml:"source"`
		SourceRef   string `yaml:"source-ref"`
		Enabled     bool   `yaml:"enabled"`
	}
	if err := json.Unmarshal(b, &meta); err != nil {
		return models.Skill{}, err
	}
	return models.Skill{ID: meta.ID, ProfileID: meta.ProfileID, Name: meta.Name, Description: meta.Description, Icon: meta.Icon, Accent: meta.Accent, Source: meta.Source, SourceRef: meta.SourceRef, Enabled: meta.Enabled}, nil
}
func parseDocument(b []byte) (models.Skill, error) {
	text := string(b)
	if !strings.HasPrefix(text, "---\n") {
		return models.Skill{}, fmt.Errorf("skill frontmatter is missing")
	}
	rest := text[4:]
	at := strings.Index(rest, "\n---\n")
	if at < 0 {
		return models.Skill{}, fmt.Errorf("skill frontmatter is invalid")
	}
	var meta struct {
		ID          string `yaml:"id"`
		ProfileID   string `yaml:"profile-id"`
		Name        string `yaml:"name"`
		Description string `yaml:"description"`
		Icon        string `yaml:"icon"`
		Accent      string `yaml:"accent"`
		Source      string `yaml:"source"`
		SourceRef   string `yaml:"source-ref"`
		Enabled     bool   `yaml:"enabled"`
	}
	if err := json.Unmarshal([]byte(rest[:at]), &meta); err != nil {
		// Skills added manually commonly use the standard YAML frontmatter:
		// name, description and optional metadata. It is intentionally accepted
		// alongside Arwos' JSON metadata frontmatter.
		if yamlErr := yaml.Unmarshal([]byte(rest[:at]), &meta); yamlErr != nil {
			return models.Skill{}, yamlErr
		}
	}
	return models.Skill{ID: meta.ID, ProfileID: meta.ProfileID, Name: meta.Name, Description: meta.Description, Content: rest[at+5:], Icon: meta.Icon, Accent: meta.Accent, Enabled: meta.Enabled, Source: meta.Source, SourceRef: meta.SourceRef}, nil
}

func filesystemSkillID(name string) string {
	sum := sha256.Sum256([]byte(name))
	return fmt.Sprintf("skill-%x", sum[:8])
}

func (x catalogSkill) model() models.Skill {
	return models.Skill{ID: x.ID, ProfileID: x.ProfileID, Name: x.Name, Description: x.Description, Icon: x.Icon, Accent: x.Accent, Enabled: x.Enabled, Source: x.Source, SourceRef: x.SourceRef, Files: append([]string(nil), x.Files...)}
}
func catalogSkillFromModel(x models.Skill, path string) catalogSkill {
	return catalogSkill{ID: x.ID, ProfileID: x.ProfileID, Name: x.Name, Description: x.Description, Icon: x.Icon, Accent: x.Accent, Source: x.Source, SourceRef: x.SourceRef, Enabled: x.Enabled, Path: path, Files: append([]string(nil), x.Files...)}
}
func skillDirectory(path string) string {
	path = filepath.ToSlash(path)
	return strings.TrimSuffix(path, "/SKILL.md")
}

// loadCatalog is the fast path used by list/read operations. A one-time
// filesystem scan only happens when upgrading an existing profile that has no
// index.yaml yet; subsequent operations use the YAML catalogue exclusively.
func (s *Service) loadCatalog(profile string) (catalog, error) {
	s.catalogMu.Lock()
	defer s.catalogMu.Unlock()
	return s.loadCatalogLocked(profile)
}
func (s *Service) loadCatalogLocked(profile string) (catalog, error) {
	if !safe(profile) {
		return catalog{}, fmt.Errorf("invalid profile id")
	}
	root, err := s.openProfileRoot(profile)
	if os.IsNotExist(err) {
		return s.rebuildCatalogLocked(profile)
	}
	if err != nil {
		return catalog{}, err
	}
	defer root.Close() //nolint:errcheck // cleanup errors cannot be returned from this scope
	b, err := root.ReadFile("index.yaml")
	if os.IsNotExist(err) {
		return s.rebuildCatalogLocked(profile)
	}
	if err != nil {
		return catalog{}, err
	}
	var out catalog
	if err := yaml.Unmarshal(b, &out); err != nil {
		// index.yaml is derived data. A damaged or manually edited file must
		// not make the skills screen unavailable; rebuild it from SKILL.md.
		return s.rebuildCatalogLocked(profile)
	}
	if out.Version != 1 {
		return s.rebuildCatalogLocked(profile)
	}
	if out.Skills == nil {
		out.Skills = []catalogSkill{}
	}
	if normalizeCatalogPaths(&out) {
		if err := s.saveCatalogLocked(profile, out); err != nil {
			return catalog{}, err
		}
	}
	return out, nil
}

// normalizeCatalogPaths upgrades the first index.yaml format where path
// pointed at SKILL.md and files were relative to the profile root.
func normalizeCatalogPaths(value *catalog) bool {
	changed := false
	for i := range value.Skills {
		entry := &value.Skills[i]
		legacyPath := strings.HasSuffix(filepath.ToSlash(entry.Path), "/SKILL.md")
		if legacyPath {
			directory := skillDirectory(entry.Path)
			entry.Path = directory
			for j := range entry.Files {
				entry.Files[j] = strings.TrimPrefix(filepath.ToSlash(entry.Files[j]), directory+"/")
			}
			changed = true
		}
		if len(entry.Files) == 0 {
			entry.Files = []string{"SKILL.md"}
			changed = true
		}
	}
	return changed
}
func (s *Service) rebuildCatalogLocked(profile string) (catalog, error) {
	out := catalog{Version: 1, Skills: []catalogSkill{}}
	root, err := s.openProfileRoot(profile)
	if os.IsNotExist(err) {
		return out, s.saveCatalogLocked(profile, out)
	}
	if err != nil {
		return out, err
	}
	defer root.Close() //nolint:errcheck // cleanup errors cannot be returned from this scope
	dir, err := root.Open(".")
	if err != nil {
		return out, err
	}
	entries, err := dir.ReadDir(-1)
	dir.Close() //nolint:errcheck // cleanup errors cannot be returned from this scope
	if err != nil {
		return out, err
	}
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == "index" || entry.Type()&fs.ModeSymlink != 0 {
			continue
		}
		body, readErr := root.ReadFile(path.Join(entry.Name(), "SKILL.md"))
		if readErr != nil {
			continue
		}
		skill, parseErr := parseDocument(body)
		metadata, metadataErr := root.ReadFile(path.Join(entry.Name(), ".metadata.json"))
		if metadataErr == nil {
			if stored, err := readMetadata(metadata); err == nil {
				if parseErr == nil {
					stored.Content = skill.Content
				} else {
					stored.Content = string(body)
				}
				skill = stored
				parseErr = nil
			}
		}
		if parseErr != nil && metadataErr != nil {
			// A newly added skill may be a plain SKILL.md with no frontmatter.
			skill = models.Skill{Content: string(body)}
			parseErr = nil
		}
		if parseErr != nil || (skill.ProfileID != "" && skill.ProfileID != profile) {
			continue
		}
		if skill.ProfileID == "" {
			skill.ProfileID = profile
		}
		if skill.Name == "" {
			skill.Name = entry.Name()
		}
		if !safe(skill.Name) {
			continue
		}
		if skill.ID == "" {
			skill.ID = filesystemSkillID(skill.Name)
		}
		if skill.Icon == "" {
			skill.Icon = "bot"
		}
		if skill.Accent == "" {
			skill.Accent = "indigo"
		}
		if skill.Source == "" {
			skill.Source = "directory"
		}
		if metadataErr != nil {
			if err := s.saveMetadata(profile, entry.Name(), skill); err != nil {
				return out, err
			}
		}
		files, filesErr := s.collectFiles(profile, entry.Name())
		if filesErr != nil {
			return out, filesErr
		}
		skill.Files = files
		out.Skills = append(out.Skills, catalogSkillFromModel(skill, entry.Name()))
	}
	sort.Slice(out.Skills, func(i, j int) bool { return strings.ToLower(out.Skills[i].Name) < strings.ToLower(out.Skills[j].Name) })
	return out, s.saveCatalogLocked(profile, out)
}
func (s *Service) saveCatalogLocked(profile string, value catalog) error {
	value.Version = 1
	if value.Skills == nil {
		value.Skills = []catalogSkill{}
	}
	if err := os.MkdirAll(s.profileDir(profile), 0755); err != nil {
		return err
	}
	b, err := yaml.Marshal(value)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.profileDir(profile), ".index-*.yaml")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name) //nolint:errcheck // cleanup errors cannot be returned from this scope
	if _, err = tmp.Write(b); err == nil {
		err = tmp.Close()
	}
	if err != nil {
		return err
	}
	return os.Rename(name, s.catalogPath(profile))
}
func (s *Service) saveMetadata(profile, directory string, x models.Skill) error {
	b, err := metadataDocument(x)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.profileDir(profile), directory, ".metadata.json"), append(b, '\n'), 0644)
}
func (s *Service) collectFiles(profile, directory string) ([]string, error) {
	if !safe(profile) || !safe(directory) {
		return nil, fmt.Errorf("invalid skill location")
	}
	root, err := s.openProfileRoot(profile)
	if err != nil {
		return nil, err
	}
	defer root.Close() //nolint:errcheck // cleanup errors cannot be returned from this scope
	files := make([]string, 0)
	err = fs.WalkDir(root.FS(), directory, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Name() == ".metadata.json" {
			return nil
		}
		rel := strings.TrimPrefix(path, directory+"/")
		if rel != "" && rel != "." {
			files = append(files, rel)
		}
		return nil
	})
	sort.Strings(files)
	return files, err
}

// Files returns the primary instruction and every regular file belonging to
// the requested skill, entirely from index.yaml.
func (s *Service) Files(profile, reference string) ([]string, error) {
	entry, err := s.catalogEntry(profile, reference)
	if err != nil {
		return nil, err
	}
	return append([]string(nil), entry.Files...), nil
}
func (s *Service) catalogEntry(profile, reference string) (catalogSkill, error) {
	catalog, err := s.loadCatalog(profile)
	if err != nil {
		return catalogSkill{}, err
	}
	for _, entry := range catalog.Skills {
		if entry.ID == reference || entry.Name == reference {
			return entry, nil
		}
	}
	return catalogSkill{}, os.ErrNotExist
}

// ReadFile only permits files explicitly recorded in index.yaml. This avoids
// path traversal and makes the catalogue the single source for skill files.
func (s *Service) ReadFile(profile, reference, file string) (string, error) {
	entry, err := s.catalogEntry(profile, reference)
	if err != nil {
		return "", err
	}
	if file == "" {
		file = "SKILL.md"
	}
	file = filepath.ToSlash(filepath.Clean(filepath.FromSlash(file)))
	allowed := false
	for _, item := range entry.Files {
		if item == file {
			allowed = true
			break
		}
	}
	if !allowed {
		return "", fmt.Errorf("skill file %q is not indexed", file)
	}
	root, err := s.openProfileRoot(profile)
	if err != nil {
		return "", err
	}
	defer root.Close() //nolint:errcheck // cleanup errors cannot be returned from this scope
	fullPath := path.Join(entry.Path, file)
	info, err := root.Lstat(fullPath)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 || info.IsDir() {
		return "", fmt.Errorf("skill file %q is unavailable", file)
	}
	body, err := root.ReadFile(fullPath)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// SetReference writes a companion file inside a skill directory. It never
// permits replacing SKILL.md; instruction metadata and the primary document
// are managed by Upsert.
func (s *Service) SetReference(profile, reference, file, content string) error {
	file = filepath.ToSlash(filepath.Clean(filepath.FromSlash(file)))
	if file == "" || file == "." || strings.EqualFold(file, "SKILL.md") || strings.HasPrefix(file, "../") || file == ".." || strings.HasPrefix(file, "/") {
		return fmt.Errorf("invalid skill reference path %q", file)
	}
	s.catalogMu.Lock()
	defer s.catalogMu.Unlock()
	catalog, err := s.loadCatalogLocked(profile)
	if err != nil {
		return err
	}
	var entry *catalogSkill
	for i := range catalog.Skills {
		if catalog.Skills[i].ID == reference || catalog.Skills[i].Name == reference {
			entry = &catalog.Skills[i]
			break
		}
	}
	if entry == nil {
		return fmt.Errorf("skill %q not found", reference)
	}
	root, err := s.openProfileRoot(profile)
	if err != nil {
		return err
	}
	defer root.Close() //nolint:errcheck // cleanup errors cannot be returned from this scope
	if err := root.MkdirAll(entry.Path, 0755); err != nil {
		return err
	}
	if err := root.WriteFile(path.Join(entry.Path, file), []byte(content), 0644); err != nil {
		return err
	}
	files, err := s.collectFiles(profile, entry.Path)
	if err != nil {
		return err
	}
	entry.Files = files
	return s.saveCatalogLocked(profile, catalog)
}

func (s *Service) List(profile string) ([]models.Skill, error) {
	page, err := s.Page(profile, "", 0)
	if err != nil {
		return nil, err
	}
	return page.Items, nil
}

// OpenFolder opens a skill directory in the user's native file manager.
// The directory is resolved from the indexed skill name; callers cannot pass
// an arbitrary filesystem path.
func (s *Service) OpenFolder(profile, id string) error {
	catalog, err := s.loadCatalog(profile)
	if err != nil {
		return err
	}
	for _, entry := range catalog.Skills {
		if entry.ID != id {
			continue
		}
		directory := filepath.Join(s.profileDir(profile), entry.Path)
		if _, err := os.Stat(directory); err != nil {
			return err
		}
		var command string
		var args []string
		switch runtime.GOOS {
		case "windows":
			command, args = "explorer.exe", []string{directory}
		case "darwin":
			command, args = "open", []string{directory}
		default:
			command, args = "xdg-open", []string{directory}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := exec.CommandContext(ctx, command, args...).Run(); err != nil { //nolint:gosec // validated input or bounded archive is required here
			return fmt.Errorf("open skill folder: %w", err)
		}
		return nil
	}
	return fmt.Errorf("skill %q not found", id)
}

// Page returns skills ordered by their on-disk directory name. The cursor is
// that directory name, which keeps paging stable without exposing a path.
// A non-positive limit returns all items for internal callers that require
// the complete profile catalogue.
func (s *Service) Page(profile, cursor string, limit int) (models.SkillPage, error) {
	if !safe(profile) {
		return models.SkillPage{}, fmt.Errorf("invalid profile id")
	}
	if cursor != "" && !safe(cursor) {
		return models.SkillPage{}, fmt.Errorf("invalid skill cursor")
	}
	catalog, err := s.loadCatalog(profile)
	if err != nil {
		return models.SkillPage{}, err
	}
	all := catalog.Skills
	start := 0
	if cursor != "" {
		for start < len(all) && strings.ToLower(skillDirectory(all[start].Path)) <= strings.ToLower(cursor) {
			start++
		}
	}
	end := len(all)
	if limit > 0 && start+limit < end {
		end = start + limit
	}
	items := make([]models.Skill, 0, end-start)
	for _, item := range all[start:end] {
		items = append(items, item.model())
	}
	page := models.SkillPage{Items: items, Total: len(all), HasMore: end < len(all)}
	if len(items) > 0 {
		page.NextCursor = skillDirectory(all[end-1].Path)
	}
	return page, nil
}
func (s *Service) Upsert(x models.Skill) (models.Skill, error) {
	x.Name = CanonicalName(x.Name)
	if !safe(x.ProfileID) || x.ID == "" || !safe(x.Name) {
		return x, fmt.Errorf("skill id, profile id and name are required")
	}
	s.catalogMu.Lock()
	catalog, err := s.loadCatalogLocked(x.ProfileID)
	if err != nil {
		s.catalogMu.Unlock()
		return x, err
	}
	for i := range catalog.Skills {
		if catalog.Skills[i].ID == x.ID {
			if !strings.EqualFold(catalog.Skills[i].Name, x.Name) {
				return x, fmt.Errorf("skill name cannot be changed after creation")
			}
			// List/update requests commonly contain only a metadata patch. Keep
			// the existing instruction body instead of replacing SKILL.md with an
			// empty document when Content was not supplied.
			if x.Content == "" {
				root, rootErr := s.openProfileRoot(x.ProfileID)
				if rootErr != nil {
					return x, rootErr
				}
				body, readErr := root.ReadFile(path.Join(catalog.Skills[i].Path, "SKILL.md"))
				root.Close() //nolint:errcheck // cleanup errors cannot be returned from this scope
				if readErr != nil {
					return x, readErr
				}
				if parsed, parseErr := parseDocument(body); parseErr == nil {
					x.Content = parsed.Content
				} else {
					x.Content = string(body)
				}
			}
			continue
		}
		if strings.EqualFold(catalog.Skills[i].Name, x.Name) {
			s.catalogMu.Unlock()
			return x, fmt.Errorf("skill name %q already exists in this profile; rename the skill and try again", x.Name)
		}
	}
	if x.Icon == "" {
		x.Icon = "bot"
	}
	if x.Accent == "" {
		x.Accent = "indigo"
	}
	if x.Source == "" {
		x.Source = "manual"
	}
	b, err := s.document(x)
	if err != nil {
		s.catalogMu.Unlock()
		return x, err
	}
	dir := s.skillDir(x)
	if err := os.MkdirAll(dir, 0755); err != nil {
		s.catalogMu.Unlock()
		return x, err
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), b, 0644); err != nil {
		s.catalogMu.Unlock()
		return x, err
	}
	if err := s.saveMetadata(x.ProfileID, x.Name, x); err != nil {
		s.catalogMu.Unlock()
		return x, err
	}
	entry := catalogSkillFromModel(x, x.Name)
	entry.Files, err = s.collectFiles(x.ProfileID, x.Name)
	if err != nil {
		s.catalogMu.Unlock()
		return x, err
	}
	replaced := false
	for i := range catalog.Skills {
		if catalog.Skills[i].ID == x.ID {
			catalog.Skills[i] = entry
			replaced = true
			break
		}
	}
	if !replaced {
		catalog.Skills = append(catalog.Skills, entry)
	}
	sort.Slice(catalog.Skills, func(i, j int) bool {
		return strings.ToLower(catalog.Skills[i].Name) < strings.ToLower(catalog.Skills[j].Name)
	})
	if err := s.saveCatalogLocked(x.ProfileID, catalog); err != nil {
		s.catalogMu.Unlock()
		return x, err
	}
	x.Files = entry.Files
	s.catalogMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	idx, err := s.index(x.ProfileID)
	if err != nil {
		return x, err
	}
	if err := idx.Index(x.ID, map[string]string{"name": x.Name, "description": x.Description, "content": x.Content}); err != nil {
		return x, err
	}
	return x, nil
}

func (s *Service) Delete(profile, id string) error {
	s.catalogMu.Lock()
	catalog, err := s.loadCatalogLocked(profile)
	if err != nil {
		s.catalogMu.Unlock()
		return err
	}
	var target *catalogSkill
	for i := range catalog.Skills {
		if catalog.Skills[i].ID == id {
			target = &catalog.Skills[i]
			break
		}
	}
	if target == nil {
		s.catalogMu.Unlock()
		return fmt.Errorf("skill %q not found", id)
	}
	if err := os.RemoveAll(filepath.Join(s.profileDir(profile), filepath.FromSlash(target.Path))); err != nil {
		s.catalogMu.Unlock()
		return err
	}
	for i := range catalog.Skills {
		if catalog.Skills[i].ID == id {
			catalog.Skills = append(catalog.Skills[:i], catalog.Skills[i+1:]...)
			break
		}
	}
	err = s.saveCatalogLocked(profile, catalog)
	s.catalogMu.Unlock()
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	idx, err := s.index(profile)
	if err != nil {
		return err
	}
	return idx.Delete(id)
}

// DeleteProfile removes all imported and managed skills plus derived catalog
// and search index for the profile.
func (s *Service) DeleteProfile(profile string) error {
	if !safe(profile) {
		return fmt.Errorf("invalid profile id")
	}
	s.catalogMu.Lock()
	defer s.catalogMu.Unlock()
	s.mu.Lock()
	if idx := s.indexes[profile]; idx != nil {
		if err := idx.Close(); err != nil {
			s.mu.Unlock()
			return err
		}
		delete(s.indexes, profile)
	}
	s.mu.Unlock()
	return os.RemoveAll(s.profileDir(profile))
}

// CleanupOrphanProfiles removes profiles that are absent from the database.
// Existing profile skill files are not altered by this maintenance operation.
func (s *Service) CleanupOrphanProfiles(valid map[string]struct{}) (int, error) {
	s.catalogMu.Lock()
	defer s.catalogMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.Root)
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
		if err := os.RemoveAll(s.profileDir(entry.Name())); err != nil {
			return removed, err
		}
		removed++
	}
	return removed, nil
}
func (s *Service) Reindex(profile string) (int, error) {
	if !safe(profile) {
		return 0, fmt.Errorf("invalid profile id")
	}
	// Drop both derived stores first. index.yaml is recreated from SKILL.md and
	// the file tree; Bleve is then rebuilt from that fresh catalogue.
	s.mu.Lock()
	if idx := s.indexes[profile]; idx != nil {
		_ = idx.Close()
		delete(s.indexes, profile)
	}
	if root, err := s.openProfileRoot(profile); err == nil {
		if err := root.Remove("index.yaml"); err != nil && !os.IsNotExist(err) {
			root.Close() //nolint:errcheck // cleanup errors cannot be returned from this scope
			s.mu.Unlock()
			return 0, err
		}
		if err := root.RemoveAll("index"); err != nil {
			root.Close() //nolint:errcheck // cleanup errors cannot be returned from this scope
			s.mu.Unlock()
			return 0, err
		}
		root.Close() //nolint:errcheck // cleanup errors cannot be returned from this scope
	} else if !os.IsNotExist(err) {
		s.mu.Unlock()
		return 0, err
	}
	s.mu.Unlock()

	s.catalogMu.Lock()
	catalog, err := s.rebuildCatalogLocked(profile)
	s.catalogMu.Unlock()
	if err != nil {
		return 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.index(profile); err != nil {
		return 0, err
	}
	return len(catalog.Skills), nil
}

// SearchPage uses the persistent Bleve index. Results are ordered by
// relevance: matches in a name rank above description matches, which rank
// above matches in the instruction text. The index intentionally considers at
// most 100 hits, so searching cannot produce an unbounded browser response.
func (s *Service) SearchPage(profile, text, cursor string, limit int) (models.SkillPage, error) {
	if !safe(profile) {
		return models.SkillPage{}, fmt.Errorf("invalid profile id")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return s.Page(profile, cursor, limit)
	}
	if cursor != "" && !safe(cursor) {
		return models.SkillPage{}, fmt.Errorf("invalid skill search cursor")
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	s.mu.Lock()
	idx, err := s.index(profile)
	s.mu.Unlock()
	if err != nil {
		return models.SkillPage{}, err
	}
	request := bleve.NewSearchRequestOptions(skillQuery(text), 100, 0, false)
	result, err := idx.Search(request)
	if err != nil {
		return models.SkillPage{}, err
	}
	items, err := s.List(profile)
	if err != nil {
		return models.SkillPage{}, err
	}
	byID := make(map[string]models.Skill, len(items))
	for _, item := range items {
		byID[item.ID] = item
	}
	ranked := make([]models.Skill, 0, len(result.Hits))
	for _, hit := range result.Hits {
		if item, ok := byID[hit.ID]; ok {
			ranked = append(ranked, item)
		}
	}
	start := 0
	if cursor != "" {
		for start < len(ranked) && ranked[start].ID != cursor {
			start++
		}
		if start < len(ranked) {
			start++
		}
	}
	end := start + limit
	if end > len(ranked) {
		end = len(ranked)
	}
	page := models.SkillPage{Items: ranked[start:end], Total: len(ranked), HasMore: end < len(ranked)}
	if len(page.Items) > 0 {
		page.NextCursor = page.Items[len(page.Items)-1].ID
	}
	return page, nil
}

func skillQuery(value string) query.Query {
	name := bleve.NewMatchQuery(value)
	name.SetField("name")
	name.SetBoost(3)
	description := bleve.NewMatchQuery(value)
	description.SetField("description")
	description.SetBoost(2)
	content := bleve.NewMatchQuery(value)
	content.SetField("content")
	return bleve.NewDisjunctionQuery(name, description, content)
}

func (s *Service) index(profile string) (bleve.Index, error) {
	if idx := s.indexes[profile]; idx != nil {
		return idx, nil
	}
	dir := filepath.Join(s.profileDir(profile), "index")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	idx, err := bleve.Open(dir)
	fresh := err != nil
	if err != nil {
		idx, err = bleve.New(dir, bleve.NewIndexMapping())
	}
	if err == nil && fresh {
		items, listErr := s.List(profile)
		if listErr != nil {
			_ = idx.Close()
			return nil, listErr
		}
		for _, item := range items {
			if indexErr := idx.Index(item.ID, map[string]string{"name": item.Name, "description": item.Description, "content": item.Content}); indexErr != nil {
				_ = idx.Close()
				return nil, indexErr
			}
		}
	}
	if err == nil {
		s.indexes[profile] = idx
	}
	return idx, err
}
func (s *Service) Get(profile, name string) (string, error) {
	body, err := s.ReadFile(profile, name, "")
	if err != nil {
		return "", err
	}
	skill, err := parseDocument([]byte(body))
	if err != nil {
		// New managed skills keep SKILL.md as pure instruction text.
		return body, err
	}
	return skill.Content, nil
}

// AttachDirectory copies only local files referenced by the imported skill's
// SKILL.md. External URLs, fragments, directories, symlinks, and paths that
// escape the skill directory are intentionally ignored.
func (s *Service) AttachDirectory(profile, id, source string) error {
	if source == "" {
		return nil
	}
	entry, err := s.catalogEntry(profile, id)
	if err != nil {
		return err
	}
	targetDir := filepath.Join(s.profileDir(profile), filepath.FromSlash(entry.Path))
	body, err := os.ReadFile(filepath.Join(source, "SKILL.md"))
	if err != nil {
		// Compatibility for directories imported before reference-aware
		// copying was introduced. New skill directories always contain
		// SKILL.md, while old companion-only directories may not.
		if !os.IsNotExist(err) {
			return err
		}
		if err := s.attachLegacyDirectory(profile, id, source, targetDir); err != nil {
			return err
		}
		body = nil
	}
	references := make(map[string]struct{})
	for _, match := range markdownFileReference.FindAllStringSubmatch(string(body), -1) {
		ref := strings.TrimSpace(match[1])
		if ref == "" || strings.HasPrefix(ref, "#") || strings.Contains(ref, "://") {
			continue
		}
		if hash := strings.IndexByte(ref, '#'); hash >= 0 {
			ref = ref[:hash]
		}
		if question := strings.IndexByte(ref, '?'); question >= 0 {
			ref = ref[:question]
		}
		clean := filepath.Clean(filepath.FromSlash(ref))
		if clean == "." || clean == "SKILL.md" || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			continue
		}
		references[clean] = struct{}{}
	}
	for rel := range references {
		path := filepath.Join(source, rel)
		info, statErr := os.Lstat(path)
		if statErr != nil || info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		destination := filepath.Join(targetDir, rel)
		if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
			return err
		}
		input, err := os.Open(path)
		if err != nil {
			return err
		}
		output, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644) //nolint:gosec // validated input or bounded archive is required here
		if err == nil {
			_, err = io.Copy(output, input)
			closeErr := output.Close()
			if err == nil {
				err = closeErr
			}
		}
		input.Close() //nolint:errcheck // cleanup errors cannot be returned from this scope
		if err != nil {
			return err
		}
	}

	s.catalogMu.Lock()
	defer s.catalogMu.Unlock()
	catalog, err := s.loadCatalogLocked(profile)
	if err != nil {
		return err
	}
	for i := range catalog.Skills {
		if catalog.Skills[i].ID == id {
			files, err := s.collectFiles(profile, catalog.Skills[i].Path)
			if err != nil {
				return err
			}
			catalog.Skills[i].Files = files
			return s.saveCatalogLocked(profile, catalog)
		}
	}
	return os.ErrNotExist
}

func (s *Service) attachLegacyDirectory(_, _, source, targetDir string) error {
	err := filepath.WalkDir(source, func(current string, item fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if item.IsDir() || item.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		rel, err := filepath.Rel(source, current)
		if err != nil {
			return err
		}
		destination := filepath.Join(targetDir, rel)
		if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
			return err
		}
		input, err := os.Open(current)
		if err != nil {
			return err
		}
		output, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644) //nolint:gosec // validated input or bounded archive is required here
		if err == nil {
			_, err = io.Copy(output, input)
			if closeErr := output.Close(); err == nil {
				err = closeErr
			}
		}
		input.Close() //nolint:errcheck // cleanup errors cannot be returned from this scope
		return err
	})
	if err != nil {
		return err
	}
	return nil
}
func (s *Service) FilesystemList() ([]Skill, error) { return []Skill{}, nil }
