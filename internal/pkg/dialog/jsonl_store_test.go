package dialog

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestConcurrentAppendAndHistoryLimit(t *testing.T) {
	s := &Store{Root: filepath.Join(t.TempDir(), "dialogs")}
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if e := s.Append("chat", Message{Role: "user", Content: "x"}); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	all, e := s.History("chat", 0)
	if e != nil || len(all) != 12 {
		t.Fatalf("len=%d err=%v", len(all), e)
	}
	limited, e := s.History("chat", 3)
	if e != nil || len(limited) != 3 {
		t.Fatalf("len=%d err=%v", len(limited), e)
	}
}

func TestClear(t *testing.T) {
	s := &Store{Root: filepath.Join(t.TempDir(), "dialogs")}
	if err := s.Append("workspace--chat", Message{Role: "user", Content: "keep no history"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Clear("workspace--chat"); err != nil {
		t.Fatal(err)
	}
	items, err := s.History("workspace--chat", 0)
	if err != nil || len(items) != 0 {
		t.Fatalf("items=%#v err=%v", items, err)
	}
}

func TestResolveErrorKeepsAppendOnlyHistory(t *testing.T) {
	s := &Store{Root: filepath.Join(t.TempDir(), "dialogs")}
	if err := s.Append("chat", Message{ID: "user-1", Role: "user", Content: "continue"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Append("chat", Message{ID: "error-1", Role: "assistant", Content: "agent iteration limit exceeded"}); err != nil {
		t.Fatal(err)
	}
	if err := s.ResolveError("chat", "error-1"); err != nil {
		t.Fatal(err)
	}
	history, err := s.History("chat", 0)
	if err != nil || len(history) != 3 || history[2].Role != "error_resolution" || history[2].Resolves != "error-1" {
		t.Fatalf("history=%#v err=%v", history, err)
	}
}

func TestFileBackedLongTermAndTopicMemories(t *testing.T) {
	root := t.TempDir()
	s := NewMemoryStore(filepath.Join(root, "dialogs"))
	if err := s.SaveNote("profile", "workspace", LongTermNote{ID: "note-1", Title: "Migration rules", Content: "Use numbered SQL migrations.", Tags: []string{"sqlite", "migration"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveTopic("profile", "workspace", TopicMemory{ID: "topic-1", Title: "SQLite", Summary: "FTS5 is enabled.", Tags: []string{"sqlite", "fts5"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveNote("profile", "workspace", LongTermNote{ID: "note-2", Title: "Other", Content: "Unrelated content"}); err != nil {
		t.Fatal(err)
	}
	if matches, err := filepath.Glob(filepath.Join(root, "memory", "profile", "note", "note-*.json")); err != nil || len(matches) != 2 {
		t.Fatalf("note files=%v err=%v", matches, err)
	}
	if matches, err := filepath.Glob(filepath.Join(root, "memory", "profile", "topics", "topic-*.json")); err != nil || len(matches) != 1 {
		t.Fatalf("topic files=%v err=%v", matches, err)
	}
	if _, err := os.Stat(filepath.Join(root, "memory", "profile", "note", "index")); err != nil {
		t.Fatalf("note index: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "memory", "profile", "topics", "index")); err != nil {
		t.Fatalf("topic index: %v", err)
	}
	page, err := s.NotesPage("profile", "", 1)
	if err != nil || len(page.Items) != 1 || !page.HasMore || page.NextCursor == "" {
		t.Fatalf("page=%#v err=%v", page, err)
	}
	second, err := s.NotesPage("profile", page.NextCursor, 1)
	if err != nil || len(second.Items) != 1 {
		t.Fatalf("second=%#v err=%v", second, err)
	}
	items, err := s.Relevant("profile", "workspace", "sqlite migration", 5)
	if err != nil || len(items) != 2 {
		t.Fatalf("items=%#v err=%v", items, err)
	}
	if notes, topics, err := s.Reindex("profile"); err != nil || notes != 2 || topics != 1 {
		t.Fatalf("reindex notes=%d topics=%d err=%v", notes, topics, err)
	}
	if err := s.DeleteNote("profile", "workspace", "note-1"); err != nil {
		t.Fatal(err)
	}
	notes, err := s.Notes("profile", "workspace")
	if err != nil || len(notes) != 1 || notes[0].ID != "note-2" {
		t.Fatalf("notes=%#v err=%v", notes, err)
	}
}

func TestDelete(t *testing.T) {
	s := &Store{Root: filepath.Join(t.TempDir(), "dialogs")}
	if err := s.Append("workspace--chat", Message{Role: "user", Content: "delete me"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete("workspace--chat"); err != nil {
		t.Fatal(err)
	}
	items, err := s.History("workspace--chat", 0)
	if err != nil || len(items) != 0 {
		t.Fatalf("items=%#v err=%v", items, err)
	}
}

func TestGoalRoundTrip(t *testing.T) {
	s := &Store{Root: t.TempDir()}
	id := SessionKey("profile-1", "session-1")
	goal := Goal{ID: "goal-1", DialogID: "session-1", Goal: "Inspect the project", Status: "running", Tasks: []GoalTask{{ID: "task-1", Label: "fs.list_dir", Status: "running"}}}
	if err := s.SaveGoal(id, goal); err != nil {
		t.Fatal(err)
	}
	stored, err := s.Goal(id)
	if err != nil || stored == nil || stored.Goal != goal.Goal || len(stored.Tasks) != 1 {
		t.Fatalf("goal=%#v err=%v", stored, err)
	}
	if err := s.DeleteGoal(id); err != nil {
		t.Fatal(err)
	}
	stored, err = s.Goal(id)
	if err != nil || stored != nil {
		t.Fatalf("goal=%#v err=%v", stored, err)
	}
	goals, err := s.Goals(id)
	if err != nil || len(goals) != 1 || goals[0].ID != goal.ID {
		t.Fatalf("goals=%#v err=%v", goals, err)
	}
}

func TestProfileSessionLayout(t *testing.T) {
	root := filepath.Join(t.TempDir(), "dialogs")
	s := &Store{Root: root}
	if err := s.Append(SessionKey("profile-1", "session-1"), Message{Role: "user", Content: "hello"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "profile-1", "session-1", "history.jsonl")); err != nil {
		t.Fatal(err)
	}
	memory := NewMemoryStore(root)
	if err := memory.Save("profile-1", "workspace", "session-1", Memory{Title: "Memory", Summary: "summary"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "profile-1", "session-1", "memory.json")); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(SessionKey("profile-1", "session-1")); err != nil {
		t.Fatal(err)
	}
	if err := memory.Delete("profile-1", "workspace", "session-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "profile-1", "session-1")); !os.IsNotExist(err) {
		t.Fatalf("session directory should be removed, err=%v", err)
	}
}

func TestDeleteProfileRemovesDialogsAndDurableMemory(t *testing.T) {
	root := t.TempDir()
	dialogs := NewStore(filepath.Join(root, "dialogs"))
	if err := dialogs.Append(SessionKey("profile-1", "session-1"), Message{Role: "user", Content: "hello"}); err != nil {
		t.Fatal(err)
	}
	memory := NewMemoryStore(dialogs.Root)
	if err := memory.SaveNote("profile-1", "", LongTermNote{ID: "note-1", Title: "Note", Content: "content"}); err != nil {
		t.Fatal(err)
	}
	if err := memory.SaveTopic("profile-1", "", TopicMemory{ID: "topic-1", Title: "Topic", Summary: "summary"}); err != nil {
		t.Fatal(err)
	}
	if err := dialogs.DeleteProfile("profile-1"); err != nil {
		t.Fatal(err)
	}
	if err := memory.DeleteProfile("profile-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "dialogs", "profile-1")); !os.IsNotExist(err) {
		t.Fatalf("dialog profile directory still exists: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "memory", "profile-1")); !os.IsNotExist(err) {
		t.Fatalf("memory profile directory still exists: %v", err)
	}
}

func TestCleanupOrphansKeepsKnownProfileSessions(t *testing.T) {
	root := t.TempDir()
	s := NewStore(filepath.Join(root, "dialogs"))
	if err := s.Append(SessionKey("known", "keep"), Message{Role: "user", Content: "keep"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Append(SessionKey("known", "stale"), Message{Role: "user", Content: "stale"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Append(SessionKey("missing", "dialog"), Message{Role: "user", Content: "missing"}); err != nil {
		t.Fatal(err)
	}
	profiles, dialogs, err := s.CleanupOrphans(map[string]map[string]struct{}{"known": {"keep": {}}})
	if err != nil || profiles != 1 || dialogs != 1 {
		t.Fatalf("profiles=%d dialogs=%d err=%v", profiles, dialogs, err)
	}
	if _, err := os.Stat(filepath.Join(root, "dialogs", "known", "keep", "history.jsonl")); err != nil {
		t.Fatalf("known session was removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "dialogs", "known", "stale")); !os.IsNotExist(err) {
		t.Fatalf("stale session still exists: %v", err)
	}
}

func TestCleanupOrphanProfilesKeepsKnownMemory(t *testing.T) {
	root := t.TempDir()
	s := NewMemoryStore(filepath.Join(root, "dialogs"))
	for _, profile := range []string{"known", "missing"} {
		if err := s.SaveNote(profile, "", LongTermNote{ID: "note-" + profile, Title: profile, Content: "content"}); err != nil {
			t.Fatal(err)
		}
	}
	removed, err := s.CleanupOrphanProfiles(map[string]struct{}{"known": {}})
	if err != nil || removed != 1 {
		t.Fatalf("removed=%d err=%v", removed, err)
	}
	if _, err := os.Stat(filepath.Join(root, "memory", "known")); err != nil {
		t.Fatalf("known memory was removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "memory", "missing")); !os.IsNotExist(err) {
		t.Fatalf("orphan memory still exists: %v", err)
	}
}
