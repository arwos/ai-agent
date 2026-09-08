package goal

import (
	"fmt"
	"testing"
)

func TestStartWorkflowTaskEnforcesOrderDependenciesCapabilitiesAndRetries(t *testing.T) {
	g := &Goal{WorkflowID: "release", CurrentTaskID: "build", Tasks: []GoalTask{
		{ID: "build", Status: "pending", MaxAttempts: 1, Tools: []string{"fs.write"}, Skills: []string{"release"}},
		{ID: "test", Status: "pending", MaxAttempts: 2, DependsOn: []string{"build"}},
	}}
	if err := StartWorkflowTask(g, "test", nil); err == nil {
		t.Fatal("out-of-order task accepted")
	}
	if err := StartWorkflowTask(g, "build", map[string]bool{}); err == nil {
		t.Fatal("missing capability accepted")
	}
	if err := StartWorkflowTask(g, "build", map[string]bool{"fs.write": true}); err == nil {
		t.Fatal("missing skill accepted")
	}
	if err := StartWorkflowTask(g, "build", map[string]bool{"fs.write": true, "release": true}); err != nil {
		t.Fatal(err)
	}
	if err := StartWorkflowTask(g, "test", nil); err == nil {
		t.Fatal("parallel task accepted")
	}
	if g.CurrentTaskID != "build" {
		t.Fatalf("current task = %q, want build", g.CurrentTaskID)
	}
	if err := CompleteWorkflowTask(g, "build", "", "built", nil); err != nil {
		t.Fatal(err)
	}
	if g.CurrentTaskID != "" {
		t.Fatalf("current task after completion = %q", g.CurrentTaskID)
	}
	if err := StartWorkflowTask(g, "test", nil); err != nil {
		t.Fatal(err)
	}
	if err := StartWorkflowTask(g, "test", nil); err == nil {
		t.Fatal("duplicate task start accepted")
	}
}

func TestCompleteWorkflowTaskRejectsStaleAndBoundsAttempts(t *testing.T) {
	g := &Goal{WorkflowID: "release", Tasks: []GoalTask{{ID: "build", Status: "pending", MaxAttempts: 1, Attempts: 1}}}
	if err := StartWorkflowTask(g, "build", nil); err == nil {
		t.Fatal("retry beyond limit accepted")
	}
	g.Tasks[0].Attempts = 0
	if err := StartWorkflowTask(g, "build", nil); err != nil {
		t.Fatal(err)
	}
	if err := CompleteWorkflowTask(g, "build", "wrong-child", "done", nil); err == nil {
		t.Fatal("stale child accepted")
	}
	if err := CompleteWorkflowTask(g, "build", "", "done", nil); err != nil {
		t.Fatal(err)
	}
	if err := CompleteWorkflowTask(g, "build", "", "again", nil); err == nil {
		t.Fatal("duplicate completion accepted")
	}
}

func TestValidateWorkflowTaskActiveRejectsOutOfOrderDelegation(t *testing.T) {
	g := &Goal{WorkflowID: "release", Tasks: []GoalTask{
		{ID: "build", Status: "running", Attempts: 1, MaxAttempts: 1},
		{ID: "test", Status: "running", Attempts: 1, MaxAttempts: 1},
	}}
	if err := ValidateWorkflowTaskActive(g, "test"); err == nil {
		t.Fatal("parallel delegation accepted")
	}
	g.Tasks[1].Status = "pending"
	if err := ValidateWorkflowTaskActive(g, "test"); err == nil {
		t.Fatal("pending delegation accepted")
	}
	if err := ValidateWorkflowTaskActive(g, "build"); err != nil {
		t.Fatal(err)
	}
}

func TestStartWorkflowTaskEnforcesExecutorPolicy(t *testing.T) {
	g := &Goal{WorkflowID: "release", Tasks: []GoalTask{{ID: "build", Status: "pending", MaxAttempts: 1, Executor: "self"}}}
	if err := StartWorkflowTaskWithPolicy(g, "build", nil, "agent_id", "worker"); err == nil {
		t.Fatal("self task accepted delegated executor")
	}
	g.Tasks[0].Status = "pending"
	if err := StartWorkflowTaskWithPolicy(g, "build", nil, "self", ""); err != nil {
		t.Fatal(err)
	}

	g = &Goal{WorkflowID: "release", Tasks: []GoalTask{{ID: "build", Status: "pending", MaxAttempts: 1, Executor: "agent_id", ExecutorAgentID: "worker"}}}
	if err := StartWorkflowTaskWithPolicy(g, "build", nil, "agent_id", "other"); err == nil {
		t.Fatal("task accepted wrong delegated agent")
	}
	g.Tasks[0].Status = "pending"
	if err := StartWorkflowTaskWithPolicy(g, "build", nil, "agent_id", "worker"); err != nil {
		t.Fatal(err)
	}
	g = &Goal{WorkflowID: "release", Tasks: []GoalTask{{ID: "build", Status: "pending", MaxAttempts: 1, Executor: "auto"}}}
	if err := StartWorkflowTaskWithPolicy(g, "build", nil, "self", ""); err != nil {
		t.Fatal(err)
	}
}

func TestCancelWorkflowTasksPersistsTerminalState(t *testing.T) {
	g := &Goal{WorkflowID: "release", CurrentTaskID: "build", Tasks: []GoalTask{
		{ID: "build", Status: "running"},
		{ID: "child", Status: "waiting_agent"},
		{ID: "done", Status: "done"},
	}}
	if !CancelWorkflowTasks(g) {
		t.Fatal("active tasks were not cancelled")
	}
	if g.CurrentTaskID != "" || g.Tasks[0].Status != "stopped" || g.Tasks[1].Status != "stopped" || g.Tasks[2].Status != "done" {
		t.Fatalf("unexpected cancellation state: %#v", g)
	}
}

func TestCompleteWorkflowTaskClearsCursorForDelegatedSuccess(t *testing.T) {
	g := &Goal{WorkflowID: "release", CurrentTaskID: "child", Tasks: []GoalTask{{ID: "child", Status: "waiting_agent", ChildDialogID: "dialog", MaxAttempts: 1}}}
	if err := CompleteWorkflowTask(g, "child", "dialog", "result", nil); err != nil {
		t.Fatal(err)
	}
	if g.CurrentTaskID != "" || g.Tasks[0].Status != "done" || g.Tasks[0].LastResult != "result" {
		t.Fatalf("unexpected delegated success state: %#v", g)
	}
}

func TestCompleteWorkflowTaskRetriesDelegatedFailure(t *testing.T) {
	g := &Goal{WorkflowID: "release", CurrentTaskID: "child", Tasks: []GoalTask{{ID: "child", Status: "reviewing", ChildDialogID: "dialog", Attempts: 1, MaxAttempts: 2}}}
	if err := CompleteWorkflowTask(g, "child", "dialog", "", fmt.Errorf("child failed")); err != nil {
		t.Fatal(err)
	}
	if g.CurrentTaskID != "" || g.Tasks[0].Status != "pending" || g.Tasks[0].Error != "child failed" {
		t.Fatalf("unexpected delegated retry state: %#v", g)
	}
}
