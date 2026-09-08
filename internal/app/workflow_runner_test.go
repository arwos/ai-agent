package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/arwos/ai-agent/internal/pkg/goal"
	"github.com/arwos/ai-agent/internal/pkg/workflow"
)

func TestWorkflowTaskReadyRequiresAcceptedDependencies(t *testing.T) {
	g := &goal.Goal{Tasks: []goal.GoalTask{
		{WorkflowStepID: "build", Status: "pending"},
		{WorkflowStepID: "verify", Status: "pending", DependsOn: []string{"build"}},
	}}
	if workflowTaskReady(g, &g.Tasks[1]) {
		t.Fatal("dependent task became ready before its dependency")
	}
	g.Tasks[0].Status = "done"
	if !workflowTaskReady(g, &g.Tasks[1]) {
		t.Fatal("dependent task did not become ready after accepted dependency")
	}
}

func TestWorkflowChildPromptsUseOnlyDeclaredDependencies(t *testing.T) {
	definition := workflow.Definition{Steps: []workflow.Step{
		{ID: "one", Title: "One", Description: "First work", AgentID: "builder", Acceptance: "First is complete", Retry: workflow.Retry{MaxAttempts: 1}},
		{ID: "two", Title: "Two", Description: "Second work", AgentID: "builder", Acceptance: "Second is complete", Retry: workflow.Retry{MaxAttempts: 1}},
		{ID: "three", Title: "Three", Description: "Third work", AgentID: "builder", Acceptance: "Third is complete", Retry: workflow.Retry{MaxAttempts: 1}, Dependencies: []string{"one"}},
	}}
	snapshot, err := json.Marshal(definition)
	if err != nil {
		t.Fatal(err)
	}
	g := &goal.Goal{Request: "Implement the feature", Tasks: []goal.GoalTask{
		{WorkflowStepID: "one", Label: "One", Status: "done", LastResult: "accepted one"},
		{WorkflowStepID: "two", Label: "Two", Status: "done", LastResult: "unrelated two"},
		{WorkflowStepID: "three", Label: "Three", Status: "pending", DependsOn: []string{"one"}, WorkflowSnapshot: snapshot, ReviewFeedback: "Address the edge case."},
	}}
	builder, err := workflowBuilderPrompt(g, &g.Tasks[2])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(builder, "accepted one") || strings.Contains(builder, "unrelated two") || !strings.Contains(builder, "Address the edge case.") {
		t.Fatalf("unexpected builder context: %s", builder)
	}
	g.Tasks[2].LastResult = "candidate result"
	reviewer, err := workflowReviewerPrompt(g, &g.Tasks[2])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reviewer, "candidate result") || strings.Contains(reviewer, "unrelated two") {
		t.Fatalf("unexpected reviewer context: %s", reviewer)
	}
}
