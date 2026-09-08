package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/arwos/ai-agent/internal/pkg/dialog"
	"github.com/arwos/ai-agent/internal/pkg/goal"
	"github.com/arwos/ai-agent/internal/pkg/workflow"
)

const (
	childRoleBuilder  = "builder"
	childRoleReviewer = "reviewer"
)

func (a *App) scheduleWorkflow(ctx context.Context, profileID, parentID, workspaceID string, g *goal.Goal) error {
	if g == nil || g.WorkflowID == "" || g.Status != "running" {
		return nil
	}
	for index := range g.Tasks {
		task := &g.Tasks[index]
		if task.Status != "pending" {
			continue
		}
		if !workflowTaskReady(g, task) {
			return nil
		}
		capabilities := make(map[string]bool, len(task.Tools)+len(task.Skills))
		for _, item := range append(append([]string{}, task.Tools...), task.Skills...) {
			capabilities[item] = true
		}
		if err := goal.StartWorkflowTaskWithPolicy(g, task.ID, capabilities, "agent_id", task.ExecutorAgentID); err != nil {
			return err
		}
		if err := a.dialogs.SaveGoal(dialog.SessionKey(profileID, parentID), *g); err != nil {
			return err
		}
		a.publish(StreamMessage{Type: "goal", Payload: map[string]any{"dialog_id": parentID, "goal": g}})
		prompt, err := workflowBuilderPrompt(g, task)
		if err != nil {
			return err
		}
		_, err = a.startChildRun(ctx, profileID, parentID, workspaceID, "workflow", map[string]any{
			"agent_id": task.ExecutorAgentID,
			"task_id":  task.ID,
			"task":     prompt,
			"role":     childRoleBuilder,
		})
		if err != nil {
			a.failWorkflowTask(profileID, parentID, g, task.ID, err)
		}
		return err
	}
	if allWorkflowTasksDone(g) {
		g.Status = "done"
		if err := a.dialogs.SaveGoal(dialog.SessionKey(profileID, parentID), *g); err != nil {
			return err
		}
		a.publish(StreamMessage{Type: "goal", Payload: map[string]any{"dialog_id": parentID, "goal": g}})
	}
	return nil
}

func workflowTaskReady(g *goal.Goal, task *goal.GoalTask) bool {
	for _, dependency := range task.DependsOn {
		for _, candidate := range g.Tasks {
			if candidate.WorkflowStepID == dependency && candidate.Status == "done" {
				goto found
			}
		}
		return false
	found:
	}
	return true
}

func allWorkflowTasksDone(g *goal.Goal) bool {
	for _, task := range g.Tasks {
		if task.Status != "done" && task.Status != "skipped" {
			return false
		}
	}
	return true
}

func workflowDefinition(task *goal.GoalTask) (workflow.Definition, workflow.Step, error) {
	var definition workflow.Definition
	if err := json.Unmarshal(task.WorkflowSnapshot, &definition); err != nil {
		return workflow.Definition{}, workflow.Step{}, fmt.Errorf("decode workflow snapshot: %w", err)
	}
	for _, step := range definition.Steps {
		if step.ID == task.WorkflowStepID {
			return definition, step, nil
		}
	}
	return workflow.Definition{}, workflow.Step{}, fmt.Errorf("workflow step %q is absent from snapshot", task.WorkflowStepID)
}

func workflowBuilderPrompt(g *goal.Goal, task *goal.GoalTask) (string, error) {
	_, step, err := workflowDefinition(task)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString("# Builder task\n\n")
	b.WriteString("Complete this workflow step. You may use only its assigned tools and skills. Do not start another workflow step. Finish by calling `agent.result` with a concise factual result.\n\n")
	fmt.Fprintf(&b, "## User request\n%s\n\n## Step: %s\n%s\n\n## Acceptance criteria\n%s\n", g.Request, step.Title, step.Description, step.Acceptance)
	if len(step.Substeps) > 0 {
		b.WriteString("\n## Checklist\n")
		for _, substep := range step.Substeps {
			fmt.Fprintf(&b, "- %s", substep.Title)
			if substep.Description != "" {
				fmt.Fprintf(&b, ": %s", substep.Description)
			}
			b.WriteByte('\n')
		}
	}
	appendDependencyResults(&b, g, task)
	if task.ReviewFeedback != "" {
		fmt.Fprintf(&b, "\n## Reviewer feedback from the previous attempt\n%s\n", task.ReviewFeedback)
	}
	return b.String(), nil
}

func workflowReviewerPrompt(g *goal.Goal, task *goal.GoalTask) (string, error) {
	_, step, err := workflowDefinition(task)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString("# Independent workflow review\n\n")
	b.WriteString("Review the Builder result independently. Use only read-only tools. Do not change files or delegate work. Call `workflow.review_result` with `pass` only when every acceptance criterion has evidence; otherwise call it with `fail` and actionable feedback.\n\n")
	fmt.Fprintf(&b, "## User request\n%s\n\n## Step: %s\n%s\n\n## Acceptance criteria\n%s\n\n## Builder result\n%s\n", g.Request, step.Title, step.Description, step.Acceptance, task.LastResult)
	if len(task.ResultMetadata) > 0 {
		metadata, marshalErr := json.Marshal(task.ResultMetadata)
		if marshalErr != nil {
			return "", fmt.Errorf("encode builder evidence: %w", marshalErr)
		}
		fmt.Fprintf(&b, "\n## Builder artifacts and changed files\n```json\n%s\n```\n", metadata)
	}
	appendDependencyResults(&b, g, task)
	return b.String(), nil
}

func (a *App) startWorkflowReview(ctx context.Context, profileID, parentID, workspaceID string, g *goal.Goal, task *goal.GoalTask) error {
	prompt, err := workflowReviewerPrompt(g, task)
	if err != nil {
		return err
	}
	_, err = a.startChildRun(ctx, profileID, parentID, workspaceID, "workflow", map[string]any{
		"agent_id": task.ReviewerAgentID,
		"task_id":  task.ID,
		"task":     prompt,
		"role":     childRoleReviewer,
	})
	if err != nil {
		a.failWorkflowTask(profileID, parentID, g, task.ID, err)
	}
	return err
}

func (a *App) failWorkflowTask(profileID, parentID string, g *goal.Goal, taskID string, cause error) {
	for index := range g.Tasks {
		if g.Tasks[index].ID == taskID {
			g.Tasks[index].Status = "failed"
			g.Tasks[index].Error = cause.Error()
			break
		}
	}
	g.CurrentTaskID, g.Status = "", "failed"
	if err := a.dialogs.SaveGoal(dialog.SessionKey(profileID, parentID), *g); err != nil {
		return
	}
	a.publish(StreamMessage{Type: "goal", Payload: map[string]any{"dialog_id": parentID, "goal": g}})
}

func appendDependencyResults(b *strings.Builder, g *goal.Goal, task *goal.GoalTask) {
	if len(task.DependsOn) == 0 {
		return
	}
	b.WriteString("\n## Accepted dependency results\n")
	for _, dependency := range task.DependsOn {
		for _, candidate := range g.Tasks {
			if candidate.WorkflowStepID == dependency {
				fmt.Fprintf(b, "### %s\n%s\n", candidate.Label, candidate.LastResult)
			}
		}
	}
}
