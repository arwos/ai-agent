package skills

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/arwos/ai-agent/internal/pkg/models"
)

func TestListAndSafeGet(t *testing.T) {
	root := t.TempDir()
	s := New(root)
	if _, e := s.Upsert(models.Skill{ID: "skill-demo", ProfileID: "profile-demo", Name: "demo", Description: "test skill", Content: "body", Enabled: true}); e != nil {
		t.Fatal(e)
	}
	items, e := s.List("profile-demo")
	if e != nil || len(items) != 1 || items[0].Description != "test skill" {
		t.Fatalf("%v %#v", e, items)
	}
	if _, e = s.Get("profile-demo", "../secret"); e == nil {
		t.Fatal("expected safe get error")
	}
}

func TestCanonicalName(t *testing.T) {
	for input, want := range map[string]string{
		"Go Code Review": "go-code-review",
		"Привет, Мир!":   "privet-mir",
		"angular___22":   "angular-22",
	} {
		if got := CanonicalName(input); got != want {
			t.Fatalf("CanonicalName(%q) = %q, want %q", input, got, want)
		}
	}
	validName := regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	for _, input := range []string{"Γειά σου", "مرحبا بالعالم", "北京 工具", "São Paulo"} {
		if got := CanonicalName(input); !validName.MatchString(got) || strings.HasPrefix(got, "skill-") {
			t.Fatalf("CanonicalName(%q) = %q is not canonical", input, got)
		}
	}
}

func TestDeleteAndReindex(t *testing.T) {
	s := New(t.TempDir())
	item := models.Skill{ID: "skill-demo", ProfileID: "profile-demo", Name: "Demo skill", Content: "instructions", Enabled: true}
	if _, err := s.Upsert(item); err != nil {
		t.Fatal(err)
	}
	if count, err := s.Reindex(item.ProfileID); err != nil || count != 1 {
		t.Fatalf("reindex: count=%d err=%v", count, err)
	}
	if _, err := os.Stat(filepath.Join(s.Root, item.ProfileID, "index.yaml")); err != nil {
		t.Fatalf("reindex must recreate index.yaml: %v", err)
	}
	if _, err := s.SearchPage(item.ProfileID, "instructions", "", 20); err != nil {
		t.Fatalf("reindex must recreate Bleve index: %v", err)
	}
	if err := s.Delete(item.ProfileID, item.ID); err != nil {
		t.Fatal(err)
	}
	items, err := s.List(item.ProfileID)
	if err != nil || len(items) != 0 {
		t.Fatalf("delete: items=%#v err=%v", items, err)
	}
}

func TestNameIsUniqueAndSeparateSkillDirectories(t *testing.T) {
	s := New(t.TempDir())
	first := models.Skill{ID: "skill-one", ProfileID: "profile-demo", Name: "first", Content: "one", Enabled: true}
	if _, err := s.Upsert(first); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Upsert(models.Skill{ID: "skill-two", ProfileID: first.ProfileID, Name: first.Name, Content: "two", Enabled: true}); err == nil {
		t.Fatal("expected duplicate name error")
	}
	copy := first
	copy.ID, copy.Name, copy.Content = "skill-copy", "renamed", "updated"
	if _, err := s.Upsert(copy); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.Root, copy.ProfileID, "renamed", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.Root, first.ProfileID, "first", "SKILL.md")); err != nil {
		t.Fatalf("previous skill directory should remain: %v", err)
	}
	items, err := s.List(copy.ProfileID)
	if err != nil || len(items) != 2 {
		t.Fatalf("both skill directories should be listed: %#v, %v", items, err)
	}
}

func TestPageUsesSkillDirectoryCursor(t *testing.T) {
	s := New(t.TempDir())
	const profile = "profile-demo"
	for _, name := range []string{"alpha", "bravo", "charlie"} {
		if _, err := s.Upsert(models.Skill{ID: "skill-" + name, ProfileID: profile, Name: name, Content: name, Enabled: true}); err != nil {
			t.Fatal(err)
		}
	}
	first, err := s.Page(profile, "", 2)
	if err != nil || first.Total != 3 || !first.HasMore || first.NextCursor != "bravo" || len(first.Items) != 2 {
		t.Fatalf("first page: %#v, %v", first, err)
	}
	second, err := s.Page(profile, first.NextCursor, 2)
	if err != nil || second.HasMore || len(second.Items) != 1 || second.Items[0].Name != "charlie" {
		t.Fatalf("second page: %#v, %v", second, err)
	}
}

func TestSearchPageRanksAndPaginatesFirstHundredResults(t *testing.T) {
	s := New(t.TempDir())
	const profile = "profile-demo"
	for _, item := range []models.Skill{
		{ID: "skill-name", ProfileID: profile, Name: "needle skill", Content: "ordinary", Enabled: true},
		{ID: "skill-description", ProfileID: profile, Name: "second", Description: "needle description", Content: "ordinary", Enabled: true},
		{ID: "skill-content", ProfileID: profile, Name: "third", Content: "needle in instructions", Enabled: true},
	} {
		if _, err := s.Upsert(item); err != nil {
			t.Fatal(err)
		}
	}
	first, err := s.SearchPage(profile, "needle", "", 2)
	if err != nil || first.Total != 3 || !first.HasMore || len(first.Items) != 2 {
		t.Fatalf("first search page: %#v, %v", first, err)
	}
	if first.Items[0].ID != "skill-name" {
		t.Fatalf("name match must rank first: %#v", first.Items)
	}
	second, err := s.SearchPage(profile, "needle", first.NextCursor, 2)
	if err != nil || second.HasMore || len(second.Items) != 1 {
		t.Fatalf("second search page: %#v, %v", second, err)
	}
}

func TestProfileIndexContainsAndReadsRelatedFiles(t *testing.T) {
	s := New(t.TempDir())
	item := models.Skill{ID: "skill-demo", ProfileID: "profile-demo", Name: "demo", Content: "instructions", Enabled: true}
	if _, err := s.Upsert(item); err != nil {
		t.Fatal(err)
	}
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "README.md"), []byte("related documentation"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(source, "references"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "references", "example.txt"), []byte("example"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := s.AttachDirectory(item.ProfileID, item.ID, source); err != nil {
		t.Fatal(err)
	}
	index, err := os.ReadFile(filepath.Join(s.Root, item.ProfileID, "index.yaml"))
	if err != nil || !strings.Contains(string(index), "README.md") {
		t.Fatalf("index.yaml: %q, %v", index, err)
	}
	files, err := s.Files(item.ProfileID, item.ID)
	if err != nil || len(files) != 3 {
		t.Fatalf("files: %#v, %v", files, err)
	}
	catalog, err := s.loadCatalog(item.ProfileID)
	if err != nil || len(catalog.Skills) != 1 || catalog.Skills[0].Path != "demo" || !strings.Contains(strings.Join(catalog.Skills[0].Files, "\n"), "README.md") {
		t.Fatalf("catalog path format: %#v, %v", catalog, err)
	}
	content, err := s.ReadFile(item.ProfileID, item.ID, "references/example.txt")
	if err != nil || content != "example" {
		t.Fatalf("read related file: %q, %v", content, err)
	}
}

func TestInvalidProfileIndexIsRebuilt(t *testing.T) {
	s := New(t.TempDir())
	item := models.Skill{ID: "skill-demo", ProfileID: "profile-demo", Name: "demo", Content: "instructions", Enabled: true}
	if _, err := s.Upsert(item); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.Root, item.ProfileID, "index.yaml"), []byte("skills: : invalid"), 0644); err != nil {
		t.Fatal(err)
	}
	items, err := s.List(item.ProfileID)
	if err != nil || len(items) != 1 || items[0].ID != item.ID {
		t.Fatalf("rebuild invalid index: %#v, %v", items, err)
	}
}

func TestDeleteProfileRemovesSkillsAndIndex(t *testing.T) {
	root := t.TempDir()
	s := New(root)
	if _, err := s.Upsert(models.Skill{ID: "skill-demo", ProfileID: "profile-demo", Name: "demo", Description: "test", Content: "body", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SearchPage("profile-demo", "body", "", 20); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteProfile("profile-demo"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "profile-demo")); !os.IsNotExist(err) {
		t.Fatalf("skills profile directory still exists: %v", err)
	}
}

func TestCleanupOrphanProfilesKeepsKnownSkills(t *testing.T) {
	root := t.TempDir()
	s := New(root)
	for _, profile := range []string{"known", "missing"} {
		if _, err := s.Upsert(models.Skill{ID: "skill-" + profile, ProfileID: profile, Name: "skill-" + profile, Description: "test", Content: "body", Enabled: true}); err != nil {
			t.Fatal(err)
		}
	}
	removed, err := s.CleanupOrphanProfiles(map[string]struct{}{"known": {}})
	if err != nil || removed != 1 {
		t.Fatalf("removed=%d err=%v", removed, err)
	}
	if _, err := os.Stat(filepath.Join(root, "known")); err != nil {
		t.Fatalf("known skills were removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "missing")); !os.IsNotExist(err) {
		t.Fatalf("orphan skills still exist: %v", err)
	}
}

func TestReindexDiscoversManuallyAddedYAMLSkill(t *testing.T) {
	s := New(t.TempDir())
	const profile = "profile-demo"
	if err := os.MkdirAll(filepath.Join(s.Root, profile, "local-rules"), 0755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: local-rules\ndescription: Local development rules\n---\n\nAlways run tests.\n"
	if err := os.WriteFile(filepath.Join(s.Root, profile, "local-rules", "SKILL.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	if count, err := s.Reindex(profile); err != nil || count != 1 {
		t.Fatalf("reindex manual skill: count=%d err=%v", count, err)
	}
	items, err := s.List(profile)
	if err != nil || len(items) != 1 || items[0].Name != "local-rules" || items[0].ID == "" || items[0].Icon != "bot" || items[0].Accent != "indigo" {
		t.Fatalf("manual skill was not indexed: %#v, %v", items, err)
	}
	if items[0].ID != filesystemSkillID("local-rules") {
		t.Fatalf("manual skill ID must be derived from its name: %q", items[0].ID)
	}
	read, err := s.Get(profile, items[0].ID)
	if err != nil || read != "\nAlways run tests.\n" {
		t.Fatalf("manual skill read: %q, %v", read, err)
	}
}
