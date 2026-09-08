package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.osspkg.com/goppy/v3/plugins/orm"
	"go.osspkg.com/goppy/v3/plugins/orm/clients/sqlite"
	"go.osspkg.com/goppy/v3/plugins/ws"
	"go.osspkg.com/goppy/v3/plugins/ws/event"

	"github.com/arwos/ai-agent/internal/pkg/agent"
	"github.com/arwos/ai-agent/internal/pkg/configstore"
	"github.com/arwos/ai-agent/internal/pkg/dialog"
	"github.com/arwos/ai-agent/internal/pkg/goal"
	"github.com/arwos/ai-agent/internal/pkg/mcp"
	"github.com/arwos/ai-agent/internal/pkg/models"
	"github.com/arwos/ai-agent/internal/pkg/skills"
	"github.com/arwos/ai-agent/internal/pkg/workspace"
)

type testWSMeta struct {
	ctx context.Context
	id  string
}

func (m testWSMeta) ConnectID() string {
	if m.id == "" {
		return "test"
	}
	return m.id
}
func (m testWSMeta) Head(string) string          { return "" }
func (m testWSMeta) AddOnCloseFunc(func(string)) {}
func (m testWSMeta) AddOnOpenFunc(func(string))  {}
func (m testWSMeta) Context() context.Context    { return m.ctx }

var _ ws.Meta = testWSMeta{}

func TestWebSocketUserResponseUnblocksMatchingDialogRequest(t *testing.T) {
	answer := make(chan any, 1)
	a := &App{requests: map[string]pendingRequest{
		"request-1": {dialogID: "dialog-a", answer: answer},
	}}
	ev := event.Pool.Get()
	defer event.Pool.Put(ev)
	if err := ev.Encode(chatWebSocketInput{Type: "user.response", ConvID: "dialog-a", ReqID: "request-1", Value: true}); err != nil {
		t.Fatal(err)
	}
	if err := a.handleWebSocketInput(ev, testWSMeta{ctx: context.Background(), id: "socket-a"}); err != nil {
		t.Fatal(err)
	}
	if value := <-answer; value != true {
		t.Fatalf("answer = %#v, want true", value)
	}

	a.requests["request-2"] = pendingRequest{dialogID: "dialog-a", answer: make(chan any, 1)}
	if err := ev.Encode(chatWebSocketInput{Type: "user.response", ConvID: "dialog-b", ReqID: "request-2", Value: true}); err != nil {
		t.Fatal(err)
	}
	if err := a.handleWebSocketInput(ev, testWSMeta{ctx: context.Background(), id: "socket-b"}); err == nil {
		t.Fatal("expected another dialog to be rejected")
	}
}

func TestPersistedWorkflowApprovalDeclinedPausesWithoutSideEffects(t *testing.T) {
	dialogs := dialog.NewStore(t.TempDir())
	store, database := newChildRecoveryStore(t)
	defer database.Close()
	profileID, dialogID := "default", "workflow-decline"
	approval := goal.Goal{
		ID: "goal-1", DialogID: dialogID, Goal: "run workflow", Status: "awaiting_approval",
		Tasks:    []goal.GoalTask{{ID: "task-1", Label: "write file", Tools: []string{"fs.write_file"}, Status: "pending"}},
		Approval: &goal.GoalApproval{ID: "approval-1", Kind: "approval", Command: "goal.start"},
	}
	if err := dialogs.SaveGoal(dialog.SessionKey(profileID, dialogID), approval); err != nil {
		t.Fatal(err)
	}
	a := &App{
		dialogs: dialogs,
		store:   store,
		publish: func(StreamMessage) {},
	}
	if err := sendPersistedApproval(t, a, dialogID, "approval-1", false); err != nil {
		t.Fatal(err)
	}
	saved, err := dialogs.Goal(dialog.SessionKey(profileID, dialogID))
	if err != nil {
		t.Fatal(err)
	}
	if saved == nil || saved.Status != "paused" || saved.Approval != nil || saved.Tasks[0].Status != "pending" {
		t.Fatalf("declined approval changed unexpected state: %#v", saved)
	}
	history, err := dialogs.History(dialog.SessionKey(profileID, dialogID), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 0 {
		t.Fatalf("declined approval created side effects in history: %#v", history)
	}
}

func TestPersistedWorkflowApprovalAcceptedCompletesApprovalWithoutUserMessage(t *testing.T) {
	dialogs := dialog.NewStore(t.TempDir())
	store, database := newChildRecoveryStore(t)
	defer database.Close()
	profileID, dialogID := "default", "workflow-accept"
	approval := goal.Goal{
		ID: "goal-1", DialogID: dialogID, Goal: "run workflow", Status: "awaiting_approval",
		Tasks:    []goal.GoalTask{{ID: "task-1", Label: "inspect", Status: "pending"}},
		Approval: &goal.GoalApproval{ID: "approval-1", Kind: "approval", Command: "goal.start"},
	}
	if err := dialogs.SaveGoal(dialog.SessionKey(profileID, dialogID), approval); err != nil {
		t.Fatal(err)
	}
	a := &App{
		dialogs: dialogs,
		store:   store,
		publish: func(StreamMessage) {},
	}
	if err := sendPersistedApproval(t, a, dialogID, "approval-1", true); err != nil {
		t.Fatal(err)
	}
	saved, err := dialogs.Goal(dialog.SessionKey(profileID, dialogID))
	if err != nil {
		t.Fatal(err)
	}
	if saved == nil || saved.Status != "done" || saved.Approval != nil {
		t.Fatalf("accepted approval changed unexpected state: %#v", saved)
	}
	history, err := dialogs.History(dialog.SessionKey(profileID, dialogID), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 0 {
		t.Fatalf("accepted approval created a synthetic user message: %#v", history)
	}
}

func sendPersistedApproval(t *testing.T, a *App, dialogID, requestID string, accepted bool) error {
	t.Helper()
	ev := event.Pool.Get()
	defer event.Pool.Put(ev)
	if err := ev.Encode(chatWebSocketInput{Type: "user.response", ConvID: dialogID, ReqID: requestID, Value: accepted}); err != nil {
		return err
	}
	return a.handleWebSocketInput(ev, testWSMeta{ctx: context.Background()})
}

func TestRestoreChildRunsIsIdempotent(t *testing.T) {
	app, dialogs, _, database := newChildRecoveryApp(t)
	defer database.Close()
	parentID, childID := "parent", "child"
	if _, err := dialogs.SaveConversation(models.Conversation{ID: parentID, ProfileID: "default", WorkspaceID: "default", Title: "parent"}); err != nil {
		t.Fatal(err)
	}
	if _, err := dialogs.SaveConversation(models.Conversation{ID: childID, ProfileID: "default", WorkspaceID: "default", ParentID: parentID, Title: "child"}); err != nil {
		t.Fatal(err)
	}
	parent := goal.Goal{ID: "goal-1", DialogID: parentID, Goal: "delegate work", Status: "running", Tasks: []goal.GoalTask{{ID: "task-1", Label: "child task", ChildDialogID: childID, RunTokenHash: "hash", Status: "waiting_agent"}}}
	if err := dialogs.SaveGoal(dialog.SessionKey("default", parentID), parent); err != nil {
		t.Fatal(err)
	}
	app.restoreChildRuns()
	app.restoreChildRuns()
	if len(app.childRuns) != 1 {
		t.Fatalf("restored child runs = %d, want 1: %#v", len(app.childRuns), app.childRuns)
	}
	run, ok := app.childRuns[childID]
	if !ok || run.ParentDialogID != parentID || run.TaskID != "task-1" || run.TokenHash != "hash" {
		t.Fatalf("unexpected restored run: %#v", run)
	}
	if len(app.chatJobs) != 0 {
		t.Fatalf("restore started duplicate jobs: %#v", app.chatJobs)
	}
}

func TestPrepareConversationForChatKeepsChildMetadataNested(t *testing.T) {
	dialogs := dialog.NewStore(t.TempDir())
	app := &App{dialogs: dialogs}
	child := models.Conversation{ID: "child", ProfileID: "default", WorkspaceID: "workspace", AgentID: "builder", ParentID: "parent", Title: "Child task"}
	if _, err := dialogs.SaveConversation(child); err != nil {
		t.Fatal(err)
	}
	conversation, err := app.prepareConversationForChat("default", child.ID, chatSendQuery{
		WorkspaceID: "other-workspace", AgentID: "other-agent", Content: "internal child instruction", Internal: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if conversation.ParentID != child.ParentID || conversation.WorkspaceID != child.WorkspaceID || conversation.AgentID != child.AgentID || conversation.Title != child.Title {
		t.Fatalf("child metadata was overwritten: %#v", conversation)
	}
	nested := filepath.Join(dialogs.Root, "default", "parent", "subconversation", "child", "conversation.json")
	if _, err := os.Stat(nested); err != nil {
		t.Fatalf("nested child metadata is missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dialogs.Root, "default", "child", "conversation.json")); !os.IsNotExist(err) {
		t.Fatalf("unexpected root child metadata: %v", err)
	}
}

func newChildRecoveryApp(t *testing.T) (*App, *dialog.Store, *configstore.Store, orm.ORM) {
	store, database := newChildRecoveryStore(t)
	dialogs := dialog.NewStore(t.TempDir())
	return &App{store: store, dialogs: dialogs, childRuns: map[string]childRun{}, chatJobs: map[string]*chatJob{}}, dialogs, store, database
}

func newChildRecoveryStore(t *testing.T) (*configstore.Store, orm.ORM) {
	t.Helper()
	database := orm.New(context.Background())
	if err := database.ApplyConfig(sqlite.Name, &sqlite.ConfigGroup{Pool: []sqlite.Config{{Tags: "master", File: filepath.Join(t.TempDir(), "db.sqlite"), Mode: "rwc", Journal: "WAL", LockingMode: "NORMAL", OtherParams: "_busy_timeout=10000"}}}); err != nil {
		t.Fatal(err)
	}
	if err := database.Tag("master").Tx(context.Background(), "test.schema", func(tx orm.Tx) {
		tx.Exec(func(q orm.Executor) {
			q.SQL(`CREATE TABLE profiles (id TEXT PRIMARY KEY, name TEXT NOT NULL, role TEXT NOT NULL DEFAULT '', accent TEXT NOT NULL DEFAULT 'indigo', temperature REAL NOT NULL DEFAULT 0.7, top_p REAL NOT NULL DEFAULT 0.9, created_at TEXT NOT NULL)`)
		})
		tx.Exec(func(q orm.Executor) {
			q.SQL(`CREATE TABLE profile_state (profile_id TEXT PRIMARY KEY, active INTEGER NOT NULL DEFAULT 0)`)
		})
		tx.Exec(func(q orm.Executor) {
			q.SQL(`CREATE TABLE conversations (id TEXT PRIMARY KEY, profile_id TEXT NOT NULL, workspace_id TEXT NOT NULL, agent_id TEXT NOT NULL DEFAULT '', title TEXT NOT NULL, updated_at TEXT NOT NULL, active_model TEXT NOT NULL DEFAULT '', parent_id TEXT NOT NULL DEFAULT '')`)
		})
	}); err != nil {
		database.Close()
		t.Fatal(err)
	}
	store := configstore.New(database)
	if _, err := store.ProfileCreate(models.Profile{ID: "default", Name: "Default"}); err != nil {
		database.Close()
		t.Fatal(err)
	}
	return store, database
}

func TestAgentResultResumesParentWithoutSyntheticUserMessage(t *testing.T) {
	dialogs := dialog.NewStore(t.TempDir())
	profileID, parentID, childID := "default", "parent", "child"
	plain := "callback-token"
	sum := sha256.Sum256([]byte(plain))
	run := childRun{ProfileID: profileID, ParentDialogID: parentID, ChildDialogID: childID, TaskID: "task-1", TokenHash: hex.EncodeToString(sum[:])}
	parent := goal.Goal{ID: "goal-1", DialogID: parentID, Goal: "delegate work", Status: "running", Tasks: []goal.GoalTask{{ID: "task-1", Label: "child task", ChildDialogID: childID, RunTokenHash: run.TokenHash, Status: "waiting_agent"}}}
	if err := dialogs.SaveGoal(dialog.SessionKey(profileID, parentID), parent); err != nil {
		t.Fatal(err)
	}
	if err := dialogs.Append(dialog.SessionKey(profileID, parentID), dialog.Message{Role: "user", Content: "delegate work"}); err != nil {
		t.Fatal(err)
	}
	if err := dialogs.Append(dialog.SessionKey(profileID, childID), dialog.Message{Role: "user", Content: "child work"}); err != nil {
		t.Fatal(err)
	}
	app := &App{dialogs: dialogs, childRuns: map[string]childRun{childID: run}, chatJobs: map[string]*chatJob{}, publish: func(StreamMessage) {}}
	if _, err := app.completeChildRun(run, map[string]any{"run_token": plain, "status": "success", "result": "finished"}); err != nil {
		t.Fatal(err)
	}
	history, err := dialogs.History(dialog.SessionKey(profileID, parentID), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 || history[0].Role != "user" || history[0].Content != "delegate work" || !history[1].VisibleOnly || !strings.Contains(history[1].Content, "Шаг выполнен: child task") {
		t.Fatalf("workflow completion progress missing: %#v", history)
	}
}

func TestCompactionMessagesSkipVisibleOnlyProgress(t *testing.T) {
	messages := compactionMessages([]dialog.Message{
		{Role: "user", Content: "request"},
		{Role: "assistant", Content: "workflow progress", VisibleOnly: true},
		{Role: "assistant", Content: "answer"},
	}, "balanced")
	if len(messages) != 3 || messages[0].Content != "request" || messages[1].Content != "answer" {
		t.Fatalf("messages=%#v", messages)
	}
	for _, message := range messages {
		if message.Content == "workflow progress" {
			t.Fatalf("visible-only progress reached compaction: %#v", messages)
		}
	}
}

func TestWebSocketConfigGetEncodesResult(t *testing.T) {
	ev := event.Pool.Get()
	defer event.Pool.Put(ev)
	ev.WithID(EventConfigGet)
	if err := ev.Encode(map[string]string{"key": "missing"}); err != nil {
		t.Fatal(err)
	}
	a := &App{settings: map[string]string{}}
	if err := a.configGet(ev, testWSMeta{ctx: context.Background()}); err != nil {
		t.Fatal(err)
	}
	var response map[string]any
	if err := ev.Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response["key"] != "missing" {
		t.Fatalf("unexpected response: %#v", response)
	}
}

func TestRPCConfigRoundTrip(t *testing.T) {
	database := orm.New(context.Background())
	defer database.Close()
	if e := database.ApplyConfig(sqlite.Name, &sqlite.ConfigGroup{Pool: []sqlite.Config{{Tags: "master", File: filepath.Join(t.TempDir(), "db.sqlite"), Mode: "rwc", Journal: "WAL", LockingMode: "NORMAL", OtherParams: "_busy_timeout=10000"}}}); e != nil {
		t.Fatal(e)
	}
	if e := database.Tag("master").Tx(context.Background(), "test.schema", func(tx orm.Tx) {
		tx.Exec(func(q orm.Executor) {
			q.SQL(`CREATE TABLE app_settings (key TEXT PRIMARY KEY, value TEXT NOT NULL DEFAULT '')`)
		})
		tx.Exec(func(q orm.Executor) {
			q.SQL(`CREATE TABLE mcp_configs (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL UNIQUE, type TEXT NOT NULL, endpoint TEXT NOT NULL, prefix TEXT NOT NULL DEFAULT '', access_order INTEGER NOT NULL DEFAULT 0)`)
		})
		tx.Exec(func(q orm.Executor) {
			q.SQL(`CREATE TABLE provider_configs (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL UNIQUE, type TEXT NOT NULL, base_url TEXT NOT NULL, api_key TEXT NOT NULL DEFAULT '', model TEXT NOT NULL DEFAULT '')`)
		})
	}); e != nil {
		t.Fatal(e)
	}
	s := configstore.New(database)
	w := workspace.New()
	d := dialog.NewStore(t.TempDir())
	a := &App{settings: map[string]string{}, store: s, workspaces: w, skills: skills.New("./skills"), dialogs: d, dialogRegistry: dialog.NewRegistry(d), mcpManager: mcp.New(), engines: agent.NewRegistry()}
	setEvent := event.Pool.Get()
	defer event.Pool.Put(setEvent)
	if e := setEvent.Encode(map[string]string{"key": "theme", "value": "dark"}); e != nil {
		t.Fatal(e)
	}
	if e := a.configSet(setEvent, testWSMeta{ctx: context.Background()}); e != nil {
		t.Fatal(e)
	}
	getEvent := event.Pool.Get()
	defer event.Pool.Put(getEvent)
	if e := getEvent.Encode(map[string]string{"key": "theme"}); e != nil {
		t.Fatal(e)
	}
	if e := a.configGet(getEvent, testWSMeta{ctx: context.Background()}); e != nil {
		t.Fatal(e)
	}
	var out map[string]any
	if e := getEvent.Decode(&out); e != nil {
		t.Fatal(e)
	}
	if out["value"] != "dark" {
		t.Fatalf("unexpected result: %#v", out)
	}
}

func TestParseGeneratedMemoryExtractsNotesAndTopics(t *testing.T) {
	parsed := parseGeneratedMemory(`{"title":"Session","summary":"Summary","topics":["go"],"notes":[{"title":"Rule","content":"Keep migrations append-only.","tags":["migration"]}],"topicMemories":[{"title":"Storage","summary":"SQLite notes.","tags":["sqlite"]}]}`)
	if parsed.Title != "Session" || len(parsed.Notes) != 1 || len(parsed.TopicMemories) != 1 {
		t.Fatalf("parsed=%#v", parsed)
	}
}

func TestParseGeneratedMemoryKeepsSummaryWhenOptionalFieldsAreMalformed(t *testing.T) {
	parsed := parseGeneratedMemory(`{"title":"Session","summary":"Only explicit facts.","topics":[{"wrong":"shape"}],"notes":[]}`)
	if parsed.Summary != "Only explicit facts." {
		t.Fatalf("summary was lost: %#v", parsed)
	}
	if len(parsed.Topics) != 0 {
		t.Fatalf("malformed topics must be ignored: %#v", parsed.Topics)
	}
}

func TestApplyGoalPlanAcceptsWrappedJSON(t *testing.T) {
	goal := defaultGoal("dialog-1", "Inspect the repository")
	err := applyGoalPlan(goal, "Plan:\n```json\n{\"goal\":\"Inspect repository\",\"tasks\":[{\"label\":\"List root files\"},{\"label\":\"Read project manifest\"}]}\n```")
	if err != nil {
		t.Fatal(err)
	}
	if goal.Goal != "Inspect repository" || len(goal.Tasks) != 2 || goal.Tasks[0].Label != "List root files" {
		t.Fatalf("goal=%#v", goal)
	}
}

func TestApplyGoalPlanKeepsFallbackTaskForInvalidPlan(t *testing.T) {
	goal := defaultGoal("dialog-1", "Inspect the repository")
	if err := applyGoalPlan(goal, "the model did not return JSON"); err == nil {
		t.Fatal("expected invalid plan error")
	}
	if len(goal.Tasks) != 1 || goal.Tasks[0].Status != "pending" {
		t.Fatalf("fallback task was lost: %#v", goal)
	}
}

func TestBrowserRequestEventIDsAreUnique(t *testing.T) {
	ids := []event.Id{EventConfigGet, EventAgentsCreate, EventKBList, EventConversationGet, EventConversationSetModel, EventWorkspaceListOpen, EventFilesWrite, EventMCPFetchTools, EventProvidersFetchModels, EventProxiesTest}
	seen := make(map[event.Id]struct{}, len(ids))
	for _, id := range ids {
		if id <= StreamInputEventID {
			t.Fatalf("application event ID %d overlaps streaming protocol", id)
		}
		if _, exists := seen[id]; exists {
			t.Fatalf("duplicate application event ID %d", id)
		}
		seen[id] = struct{}{}
	}
}

func TestCompleteChildRunRejectsWrongAndDuplicateCallback(t *testing.T) {
	dialogs := dialog.NewStore(t.TempDir())
	parentID := "parent"
	profileID := "default"
	plain := "callback-token"
	sum := sha256.Sum256([]byte(plain))
	run := childRun{ProfileID: profileID, ParentDialogID: parentID, ChildDialogID: "child", TaskID: "task", TokenHash: hex.EncodeToString(sum[:])}
	parent := goal.Goal{ID: "goal", DialogID: parentID, Goal: "delegate", Status: "running", Tasks: []goal.GoalTask{{ID: "task", Status: "waiting_agent", ChildDialogID: "child", RunTokenHash: run.TokenHash}}}
	if err := dialogs.SaveGoal(dialog.SessionKey(profileID, parentID), parent); err != nil {
		t.Fatal(err)
	}
	a := &App{dialogs: dialogs, childRuns: map[string]childRun{run.ChildDialogID: run}, publish: func(StreamMessage) {}}
	if _, err := a.completeChildRun(run, map[string]any{"run_token": "wrong", "status": "success", "result": "ok"}); err == nil {
		t.Fatal("wrong callback token accepted")
	}
	if _, err := a.completeChildRun(run, map[string]any{"run_token": plain, "status": "success", "result": "ok"}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.completeChildRun(run, map[string]any{"run_token": plain, "status": "success", "result": "again"}); err == nil {
		t.Fatal("duplicate callback accepted")
	}
}

func TestCancelChildJobsStopsAndPersistsParentState(t *testing.T) {
	dialogs := dialog.NewStore(t.TempDir())
	parentID, profileID, childID := "parent", "default", "child"
	parent := goal.Goal{ID: "goal", DialogID: parentID, Goal: "delegate work", WorkflowID: "workflow", CurrentTaskID: "task", Status: "running", Tasks: []goal.GoalTask{{ID: "task", Label: "child task", Status: "waiting_agent", ChildDialogID: childID}}}
	if err := dialogs.SaveGoal(dialog.SessionKey(profileID, parentID), parent); err != nil {
		t.Fatal(err)
	}
	childStopped := false
	a := &App{
		dialogs:   dialogs,
		childRuns: map[string]childRun{childID: {ProfileID: profileID, ParentDialogID: parentID, ChildDialogID: childID, TaskID: "task"}},
		chatJobs:  map[string]*chatJob{childID: {cancel: func() { childStopped = true }}},
		publish:   func(StreamMessage) {},
	}
	a.cancelChildJobs(profileID, parentID)
	if !childStopped {
		t.Fatal("child job was not cancelled")
	}
	if _, exists := a.childRuns[childID]; exists {
		t.Fatal("child run was not removed")
	}
	saved, err := dialogs.Goal(dialog.SessionKey(profileID, parentID))
	if err != nil {
		t.Fatal(err)
	}
	if saved == nil || saved.CurrentTaskID != "" || saved.Tasks[0].Status != "stopped" {
		t.Fatalf("unexpected persisted parent state: %#v", saved)
	}
}

func TestWorkspaceListOpenReturnsReactWorkspaceModel(t *testing.T) {
	registry := workspace.NewRegistry()
	root := t.TempDir()
	if _, err := registry.Open("project", root); err != nil {
		t.Fatal(err)
	}
	service, err := registry.Get("project")
	if err != nil {
		t.Fatal(err)
	}
	if err := service.WriteFile("README.md", "hello"); err != nil {
		t.Fatal(err)
	}

	a := &App{workspaces: registry}
	ev := event.Pool.Get()
	defer event.Pool.Put(ev)
	if err := ev.Encode(map[string]any{}); err != nil {
		t.Fatal(err)
	}
	if err := a.workspaceListOpen(ev, testWSMeta{ctx: context.Background()}); err != nil {
		t.Fatal(err)
	}
	var items []map[string]any
	if err := ev.Decode(&items); err != nil || len(items) != 1 {
		t.Fatalf("unexpected list_open response: %#v err=%v", items, err)
	}
	item := items[0]
	if item["id"] != "project" || item["name"] != filepath.Base(root) || item["folderPath"] != root {
		t.Fatalf("incorrect workspace identity: %#v", item)
	}
	if item["createdAt"] == "" || item["fileCount"] != float64(1) {
		t.Fatalf("workspace metadata is incomplete: %#v", item)
	}
	files, ok := item["files"].([]any)
	if !ok || len(files) != 0 {
		t.Fatalf("files must be a lazy empty array: %#v", item["files"])
	}
}
