package app

import (
	"context"
	"path/filepath"
	"testing"

	"go.osspkg.com/goppy/v3/plugins/orm"
	"go.osspkg.com/goppy/v3/plugins/orm/clients/sqlite"
	"go.osspkg.com/goppy/v3/plugins/ws"
	"go.osspkg.com/goppy/v3/plugins/ws/event"

	"github.com/arwos/ai-agent/internal/pkg/agent"
	"github.com/arwos/ai-agent/internal/pkg/configstore"
	"github.com/arwos/ai-agent/internal/pkg/dialog"
	"github.com/arwos/ai-agent/internal/pkg/mcp"
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
