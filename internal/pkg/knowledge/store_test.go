package knowledge

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/arwos/ai-agent/internal/pkg/models"
)

func TestStorePersistsAndSearchesDocuments(t *testing.T) {
	s := NewAt(filepath.Join(t.TempDir(), "knowledge"))
	if _, err := s.Upsert(models.KBDoc{ID: "kb-a", ProfileID: "profile-a", Title: "Go guide", Content: "Bleve search guide", Tags: []string{"go"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Upsert(models.KBDoc{ID: "kb-b", ProfileID: "profile-a", Title: "Other", Content: "unrelated", Tags: []string{"misc"}}); err != nil {
		t.Fatal(err)
	}
	items, total, err := s.List("profile-a", "", "guide", []string{"go"}, 10)
	if err != nil || total != 1 || len(items) != 1 || items[0].ID != "kb-a" {
		t.Fatalf("unexpected page: %#v total=%d err=%v", items, total, err)
	}
	if _, err := s.Search("profile-a", "Bleve", 10); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete("profile-a", "kb-a"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Search("profile-a", "Bleve", 10); err != nil {
		t.Fatal(err)
	}
}

func TestStoreCursorIsProfileScoped(t *testing.T) {
	s := NewAt(t.TempDir())
	for _, d := range []models.KBDoc{{ID: "kb-2", ProfileID: "p", Title: "Two"}, {ID: "kb-1", ProfileID: "p", Title: "One"}, {ID: "kb-x", ProfileID: "other", Title: "Other"}} {
		if _, err := s.Upsert(d); err != nil {
			t.Fatal(err)
		}
	}
	page, total, err := s.List("p", "", "", nil, 1)
	if err != nil || total != 2 || len(page) != 1 {
		t.Fatalf("first page: %#v %d %v", page, total, err)
	}
	next, _, err := s.List("p", page[0].ID, "", nil, 1)
	if err != nil || len(next) != 1 || next[0].ID == page[0].ID {
		t.Fatalf("next page: %#v %v", next, err)
	}
}

func TestDeleteProfileRemovesDocumentsAndIndex(t *testing.T) {
	root := t.TempDir()
	s := NewAt(root)
	if _, err := s.Upsert(models.KBDoc{ID: "kb-a", ProfileID: "profile-a", Title: "Guide", Content: "content"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Search("profile-a", "content", 10); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteProfile("profile-a"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "profile-a")); !os.IsNotExist(err) {
		t.Fatalf("knowledge profile directory still exists: %v", err)
	}
}

func TestCleanupOrphanProfilesKeepsKnownKnowledge(t *testing.T) {
	root := t.TempDir()
	s := NewAt(root)
	for _, profile := range []string{"known", "missing"} {
		if _, err := s.Upsert(models.KBDoc{ID: "kb-" + profile, ProfileID: profile, Title: profile, Content: "content"}); err != nil {
			t.Fatal(err)
		}
	}
	removed, err := s.CleanupOrphanProfiles(map[string]struct{}{"known": {}})
	if err != nil || removed != 1 {
		t.Fatalf("removed=%d err=%v", removed, err)
	}
	if _, err := os.Stat(filepath.Join(root, "known")); err != nil {
		t.Fatalf("known knowledge was removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "missing")); !os.IsNotExist(err) {
		t.Fatalf("orphan knowledge still exists: %v", err)
	}
}
