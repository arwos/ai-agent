package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/arwos/ai-agent/internal/pkg/dialog"
	"github.com/arwos/ai-agent/internal/pkg/goal"
	"github.com/arwos/ai-agent/internal/pkg/models"
)

type childRun struct {
	ProfileID      string
	ParentDialogID string
	ChildDialogID  string
	TaskID         string
	TokenHash      string
	Role           string
}

type agentResult struct {
	RunToken     string        `json:"run_token"`
	Status       string        `json:"status"`
	Result       string        `json:"result"`
	Error        string        `json:"error,omitempty"`
	Artifacts    []string      `json:"artifacts,omitempty"`
	ChangedFiles []changedFile `json:"changed_files,omitempty"`
}

type changedFile struct {
	Path      string `json:"path"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
}

type workflowReviewResult struct {
	RunToken string   `json:"run_token"`
	Verdict  string   `json:"verdict"`
	Summary  string   `json:"summary"`
	Evidence []string `json:"evidence,omitempty"`
	Feedback string   `json:"feedback,omitempty"`
}

func workflowTaskLabel(g *goal.Goal, taskID string) string {
	if g != nil {
		for _, task := range g.Tasks {
			if task.ID == taskID && strings.TrimSpace(task.Label) != "" {
				return task.Label
			}
		}
	}
	return "workflow step"
}

func (a *App) appendChildProgress(run childRun, parentGoal *goal.Goal, status, result, errorText string) error {
	label := workflowTaskLabel(parentGoal, run.TaskID)
	var content string
	if status == "waiting_agent" || status == "reviewing" {
		if run.Role == childRoleReviewer {
			content = "Запускаю проверку шага: " + label
		} else {
			content = "Запускаю шаг: " + label
		}
	} else {
		prefix := "Шаг"
		if run.Role == childRoleReviewer {
			prefix = "Проверка шага"
		}
		if status == "success" {
			content = prefix + " выполнен: " + label
			if value := strings.TrimSpace(truncateGoalResult(result)); value != "" {
				content += "\n\n" + value
			}
		} else {
			content = prefix + " не выполнен: " + label
			if value := strings.TrimSpace(truncateGoalResult(errorText)); value != "" {
				content += "\n\n" + value
			}
		}
	}
	return a.dialogs.Append(a.dialogKey(run.ProfileID, run.ParentDialogID), dialog.Message{
		ID:          dialog.NewMessageID(),
		Role:        "assistant",
		Content:     content,
		VisibleOnly: true,
	})
}

func token() (string, string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", "", err
	}
	plain := hex.EncodeToString(value)
	sum := sha256.Sum256([]byte(plain))
	return plain, hex.EncodeToString(sum[:]), nil
}

func (a *App) agentTools(profileID, publicDialogID, scopedDialogID, workspaceID, mode string) map[string]func(context.Context, map[string]any) (any, error) {
	out := map[string]func(context.Context, map[string]any) (any, error){
		"agent.list": func(_ context.Context, _ map[string]any) (any, error) {
			agents, err := a.store.Agents(profileID)
			if err != nil {
				return nil, err
			}
			out := make([]map[string]any, 0, len(agents))
			for _, item := range agents {
				out = append(out, map[string]any{"id": item.ID, "name": item.Name, "description": item.Description, "capabilities": map[string]any{"mcp": len(item.MCPIDs), "skill_groups": len(item.SkillGroupIDs)}})
			}
			return out, nil
		},
		"artifact.create": func(_ context.Context, args map[string]any) (any, error) {
			path, _ := args["path"].(string)
			if strings.TrimSpace(path) == "" {
				return nil, fmt.Errorf("path is required")
			}
			workspace, err := a.workspaces.Get(workspaceID)
			if err != nil {
				return nil, err
			}
			content, err := workspace.ReadFile(path)
			if err != nil {
				return nil, err
			}
			return a.workflows.CreateArtifact(profileID, path, []byte(content))
		},
		"artifact.get": func(_ context.Context, args map[string]any) (any, error) {
			id, _ := args["id"].(string)
			return a.workflows.GetArtifact(profileID, id)
		},
		"artifact.read": func(_ context.Context, args map[string]any) (any, error) {
			id, _ := args["id"].(string)
			content, err := a.workflows.ReadArtifact(profileID, id)
			if err != nil {
				return nil, err
			}
			return string(content), nil
		},
	}
	if run, ok := a.childRun(scopedDialogID); ok {
		if run.Role == childRoleReviewer {
			out["workflow.review_result"] = func(_ context.Context, args map[string]any) (any, error) {
				return a.completeWorkflowReview(run, args)
			}
			return out
		}
		out["agent.result"] = func(_ context.Context, args map[string]any) (any, error) {
			return a.completeChildRun(run, args)
		}
		return out
	}
	out["agent.run"] = func(ctx context.Context, args map[string]any) (any, error) {
		return a.startChildRun(ctx, profileID, publicDialogID, workspaceID, mode, args)
	}
	return out
}

func (a *App) childRun(dialogID string) (childRun, bool) {
	a.childMu.RLock()
	defer a.childMu.RUnlock()
	run, ok := a.childRuns[dialogID]
	return run, ok
}

func (a *App) startChildRun(ctx context.Context, profileID, parentID, workspaceID, mode string, args map[string]any) (any, error) {
	agentID, _ := args["agent_id"].(string)
	task, _ := args["task"].(string)
	expected, _ := args["expected_result"].(string)
	taskID, _ := args["task_id"].(string)
	role, _ := args["role"].(string)
	if role == "" {
		role = childRoleBuilder
	}
	if role != childRoleBuilder && role != childRoleReviewer {
		return nil, fmt.Errorf("invalid child role")
	}
	if agentID == "" || strings.TrimSpace(task) == "" {
		return nil, fmt.Errorf("agent_id and task are required")
	}
	agents, err := a.store.Agents(profileID)
	if err != nil {
		return nil, err
	}
	if !containsAgent(agents, agentID) {
		return nil, fmt.Errorf("child agent %q not found", agentID)
	}
	parentGoal, err := a.dialogs.Goal(a.dialogKey(profileID, parentID))
	if err != nil {
		return nil, err
	}
	if parentGoal != nil && parentGoal.WorkflowID != "" {
		if taskID == "" {
			return nil, fmt.Errorf("task_id is required for workflow delegation")
		}
		if role == childRoleReviewer {
			if parentGoal.CurrentTaskID != taskID {
				return nil, fmt.Errorf("workflow review task %q is not current", taskID)
			}
			found := false
			for _, task := range parentGoal.Tasks {
				if task.ID == taskID && task.Status == "reviewing" {
					found = true
				}
			}
			if !found {
				return nil, fmt.Errorf("workflow review task %q is not active", taskID)
			}
		} else if err := goal.ValidateWorkflowTaskActive(parentGoal, taskID); err != nil {
			return nil, err
		}
	}
	if taskID == "" {
		taskID = dialog.NewMessageID()
		if parentGoal == nil {
			parentGoal = &goal.Goal{ID: dialog.NewMessageID(), DialogID: parentID, Goal: "Delegated work", Status: "running"}
		}
		parentGoal.Tasks = append(parentGoal.Tasks, goal.GoalTask{ID: taskID, Label: task, Status: "running", StartedAt: time.Now().UTC()})
	}
	childID := dialog.NewMessageID()
	plain, hash, err := token()
	if err != nil {
		return nil, err
	}
	childConversation, err := a.dialogs.SaveConversation(models.Conversation{ID: childID, ProfileID: profileID, WorkspaceID: workspaceID, AgentID: agentID, ParentID: parentID, Title: goalTitle(task)})
	if err != nil {
		return nil, err
	}
	for i := range parentGoal.Tasks {
		if parentGoal.Tasks[i].ID == taskID {
			if role == childRoleReviewer {
				parentGoal.Tasks[i].ReviewerDialogID = childID
			} else {
				parentGoal.Tasks[i].ChildDialogID = childID
			}
			parentGoal.Tasks[i].ChildAgentID = agentID
			parentGoal.Tasks[i].RunTokenHash = hash
			parentGoal.Tasks[i].ExpectedResult = expected
			if role == childRoleReviewer {
				parentGoal.Tasks[i].Status = "reviewing"
			} else {
				parentGoal.Tasks[i].Status = "waiting_agent"
			}
			parentGoal.Tasks[i].StartedAt = time.Now().UTC()
		}
	}
	if err = a.dialogs.SaveGoal(a.dialogKey(profileID, parentID), *parentGoal); err != nil {
		return nil, err
	}
	a.childMu.Lock()
	run := childRun{ProfileID: profileID, ParentDialogID: parentID, ChildDialogID: childID, TaskID: taskID, TokenHash: hash, Role: role}
	a.childRuns[childID] = run
	a.childMu.Unlock()
	childStatus := "waiting_agent"
	if role == childRoleReviewer {
		childStatus = "reviewing"
	}
	if err = a.appendChildProgress(run, parentGoal, childStatus, "", ""); err != nil {
		a.childMu.Lock()
		delete(a.childRuns, childID)
		a.childMu.Unlock()
		return nil, err
	}
	a.publish(StreamMessage{Type: "subagent", Payload: map[string]any{"dialog_id": parentID, "child_dialog_id": childID, "task_id": taskID, "status": childStatus, "conversation": childConversation}})
	resultTool := "agent.result"
	if role == childRoleReviewer {
		resultTool = "workflow.review_result"
	}
	childMode := "normal"
	if role == childRoleReviewer {
		childMode = "consult"
	}
	// The parent request finishes immediately after scheduling the child. Its
	// deferred cancellation must not stop the child before it can run.
	childCtx := context.Background()
	a.llmMu.Lock()
	if a.llmRootCtx != nil {
		childCtx = a.llmRootCtx
	}
	a.llmMu.Unlock()
	go a.runChildChat(childCtx, run, chatSendQuery{DialogID: childID, Content: task + "\n\nYou must report the canonical result with " + resultTool + " using this run token: " + plain, WorkspaceID: workspaceID, AgentID: agentID, Mode: childMode, Internal: true})
	return map[string]any{"dialog_id": childID, "task_id": taskID, "status": "waiting_agent"}, nil
}

func (a *App) runChildChat(ctx context.Context, run childRun, q chatSendQuery) {
	childCtx, cancel := context.WithCancel(ctx)
	job := &chatJob{cancel: cancel, done: make(chan struct{})}
	a.chatMu.Lock()
	a.chatJobs[run.ChildDialogID] = job
	a.chatMu.Unlock()
	defer func() {
		a.chatMu.Lock()
		delete(a.chatJobs, run.ChildDialogID)
		a.chatMu.Unlock()
		cancel()
		close(job.done)
		if _, active := a.childRun(run.ChildDialogID); active {
			_ = a.completeChildRunInternal(run, "failed", "", "child ended without agent.result")
		}
	}()
	if err := a.runChatSend(q, childCtx, "", nil); err != nil {
		_ = a.completeChildRunInternal(run, "failed", "", err.Error())
	}
}

func (a *App) completeChildRun(run childRun, raw map[string]any) (any, error) {
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid result payload: %w", err)
	}
	var result agentResult
	if err = json.Unmarshal(encoded, &result); err != nil {
		return nil, fmt.Errorf("invalid result payload: %w", err)
	}
	status, value, errorText, runToken := result.Status, result.Result, result.Error, result.RunToken
	if runToken == "" {
		return nil, fmt.Errorf("run_token is required")
	}
	sum := sha256.Sum256([]byte(runToken))
	if hex.EncodeToString(sum[:]) != run.TokenHash {
		return nil, fmt.Errorf("invalid run token")
	}
	if status != "success" && status != "failed" {
		return nil, fmt.Errorf("status must be success or failed")
	}
	if len([]rune(value)) > 2000 || len([]rune(errorText)) > 2000 || (status == "success" && strings.TrimSpace(value) == "") || (status == "failed" && strings.TrimSpace(errorText) == "") {
		return nil, fmt.Errorf("invalid result payload")
	}
	for _, artifactID := range result.Artifacts {
		if _, artifactErr := a.workflows.GetArtifact(run.ProfileID, artifactID); artifactErr != nil {
			return nil, fmt.Errorf("invalid artifact %q: %w", artifactID, artifactErr)
		}
	}
	for _, changed := range result.ChangedFiles {
		clean := filepath.ToSlash(filepath.Clean(changed.Path))
		if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") || changed.StartLine < 1 || changed.EndLine < changed.StartLine || changed.EndLine-changed.StartLine > 100000 {
			return nil, fmt.Errorf("invalid changed file metadata")
		}
	}
	parentGoal, err := a.dialogs.Goal(a.dialogKey(run.ProfileID, run.ParentDialogID))
	if err != nil || parentGoal == nil {
		return nil, fmt.Errorf("parent goal not found")
	}
	found := false
	for i := range parentGoal.Tasks {
		task := &parentGoal.Tasks[i]
		if task.ID != run.TaskID || task.ChildDialogID != run.ChildDialogID || task.RunTokenHash != run.TokenHash {
			continue
		}
		if task.Status != "waiting_agent" {
			return nil, fmt.Errorf("child result already completed")
		}
		found = true
		task.LastResult, task.Error = value, errorText
		task.ResultMetadata = map[string]any{"artifacts": result.Artifacts, "changed_files": result.ChangedFiles, "status": status}
		task.FinishedAt = time.Now().UTC()
		break
	}
	if !found {
		return nil, fmt.Errorf("child run is not current")
	}
	if err = a.finishChildRun(run, parentGoal, status, value, errorText, result.Artifacts, result.ChangedFiles); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "status": status}, nil
}

func (a *App) completeWorkflowReview(run childRun, raw map[string]any) (any, error) {
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid review result: %w", err)
	}
	var result workflowReviewResult
	if err = json.Unmarshal(encoded, &result); err != nil {
		return nil, fmt.Errorf("invalid review result: %w", err)
	}
	if result.RunToken == "" || (result.Verdict != "pass" && result.Verdict != "fail") || strings.TrimSpace(result.Summary) == "" || (result.Verdict == "fail" && strings.TrimSpace(result.Feedback) == "") {
		return nil, fmt.Errorf("invalid review result")
	}
	sum := sha256.Sum256([]byte(result.RunToken))
	if hex.EncodeToString(sum[:]) != run.TokenHash {
		return nil, fmt.Errorf("invalid run token")
	}
	parentGoal, err := a.dialogs.Goal(a.dialogKey(run.ProfileID, run.ParentDialogID))
	if err != nil || parentGoal == nil {
		return nil, fmt.Errorf("parent goal not found")
	}
	var task *goal.GoalTask
	for index := range parentGoal.Tasks {
		candidate := &parentGoal.Tasks[index]
		if candidate.ID == run.TaskID && candidate.ReviewerDialogID == run.ChildDialogID && candidate.RunTokenHash == run.TokenHash && candidate.Status == "reviewing" {
			task = candidate
			break
		}
	}
	if task == nil {
		return nil, fmt.Errorf("review result is not current")
	}
	task.ReviewVerdict, task.ReviewSummary, task.ReviewEvidence, task.ReviewFeedback = result.Verdict, result.Summary, append([]string(nil), result.Evidence...), result.Feedback
	task.History = append(task.History, goal.TaskAttempt{Attempt: task.Attempts, BuilderResult: task.LastResult, ReviewVerdict: result.Verdict, ReviewSummary: result.Summary, ReviewEvidence: append([]string(nil), result.Evidence...), ReviewFeedback: result.Feedback})
	if result.Verdict == "pass" {
		task.Status = "done"
		parentGoal.CurrentTaskID = ""
	} else if task.Attempts < task.MaxAttempts {
		task.Status = "pending"
		parentGoal.CurrentTaskID = ""
	} else {
		task.Status = "failed"
		parentGoal.CurrentTaskID = ""
		parentGoal.Status = "failed"
	}
	if err = a.dialogs.SaveGoal(a.dialogKey(run.ProfileID, run.ParentDialogID), *parentGoal); err != nil {
		return nil, err
	}
	status := "failed"
	if result.Verdict == "pass" {
		status = "success"
	}
	if err = a.appendChildProgress(run, parentGoal, status, result.Summary, result.Feedback); err != nil {
		return nil, err
	}
	a.childMu.Lock()
	delete(a.childRuns, run.ChildDialogID)
	a.childMu.Unlock()
	a.publish(StreamMessage{Type: "subagent", Payload: map[string]any{"dialog_id": run.ParentDialogID, "child_dialog_id": run.ChildDialogID, "task_id": run.TaskID, "status": status}})
	a.publish(StreamMessage{Type: "goal", Payload: map[string]any{"dialog_id": run.ParentDialogID, "goal": parentGoal}})
	if parentGoal.Status == "running" {
		conversation, conversationErr := a.dialogs.Conversation(run.ProfileID, run.ParentDialogID)
		if conversationErr == nil {
			go func() {
				_ = a.scheduleWorkflow(context.Background(), run.ProfileID, run.ParentDialogID, conversation.WorkspaceID, parentGoal)
			}()
		}
	}
	return map[string]any{"ok": true, "verdict": result.Verdict}, nil
}

func (a *App) completeChildRunInternal(run childRun, status, value, errorText string) error {
	parentGoal, err := a.dialogs.Goal(a.dialogKey(run.ProfileID, run.ParentDialogID))
	if err != nil || parentGoal == nil {
		return err
	}
	for i := range parentGoal.Tasks {
		task := &parentGoal.Tasks[i]
		if run.Role == childRoleReviewer && task.ID == run.TaskID && task.ReviewerDialogID == run.ChildDialogID && task.RunTokenHash == run.TokenHash && task.Status == "reviewing" {
			task.Status, task.Error, parentGoal.Status = "failed", errorText, "failed"
			return a.saveWorkflowChildFailure(run, parentGoal, errorText)
		}
		if task.ID == run.TaskID && task.ChildDialogID == run.ChildDialogID && task.RunTokenHash == run.TokenHash && task.Status == "waiting_agent" {
			return a.finishChildRun(run, parentGoal, status, value, errorText, nil, nil)
		}
	}
	return nil
}

func (a *App) saveWorkflowChildFailure(run childRun, parentGoal *goal.Goal, errorText string) error {
	if err := a.dialogs.SaveGoal(a.dialogKey(run.ProfileID, run.ParentDialogID), *parentGoal); err != nil {
		return err
	}
	if err := a.appendChildProgress(run, parentGoal, "failed", "", errorText); err != nil {
		return err
	}
	a.childMu.Lock()
	delete(a.childRuns, run.ChildDialogID)
	a.childMu.Unlock()
	a.publish(StreamMessage{Type: "subagent", Payload: map[string]any{"dialog_id": run.ParentDialogID, "child_dialog_id": run.ChildDialogID, "task_id": run.TaskID, "status": "failed"}})
	a.publish(StreamMessage{Type: "goal", Payload: map[string]any{"dialog_id": run.ParentDialogID, "goal": parentGoal}})
	return nil
}

func (a *App) finishChildRun(run childRun, parentGoal *goal.Goal, status, value, errorText string, artifacts []string, changedFiles []changedFile) error {
	var schedule bool
	var reviewTask *goal.GoalTask
	for i := range parentGoal.Tasks {
		task := &parentGoal.Tasks[i]
		if task.ID != run.TaskID || task.ChildDialogID != run.ChildDialogID || task.RunTokenHash != run.TokenHash {
			continue
		}
		task.LastResult, task.Error = value, errorText
		task.ResultMetadata = map[string]any{"artifacts": artifacts, "changed_files": changedFiles, "status": status}
		task.FinishedAt = time.Now().UTC()
		if parentGoal.WorkflowID != "" && run.Role == childRoleBuilder && status == "success" {
			task.Status = "reviewing"
			reviewTask = task
		} else if status == "success" {
			if parentGoal.WorkflowID != "" {
				if err := goal.CompleteWorkflowTask(parentGoal, task.ID, run.ChildDialogID, value, nil); err != nil {
					return err
				}
			} else {
				task.Status = "done"
			}
		} else {
			if parentGoal.WorkflowID != "" {
				task.History = append(task.History, goal.TaskAttempt{Attempt: task.Attempts, BuilderResult: value, ReviewSummary: errorText})
				if err := goal.CompleteWorkflowTask(parentGoal, task.ID, run.ChildDialogID, "", fmt.Errorf("%s", errorText)); err != nil {
					return err
				}
				if task.Status == "failed" {
					parentGoal.Status = "failed"
				} else {
					schedule = true
				}
			} else {
				task.Status = "failed"
			}
		}
		break
	}
	if err := a.dialogs.SaveGoal(a.dialogKey(run.ProfileID, run.ParentDialogID), *parentGoal); err != nil {
		return err
	}
	if err := a.appendChildProgress(run, parentGoal, status, value, errorText); err != nil {
		return err
	}
	a.childMu.Lock()
	delete(a.childRuns, run.ChildDialogID)
	a.childMu.Unlock()
	a.publish(StreamMessage{Type: "subagent", Payload: map[string]any{"dialog_id": run.ParentDialogID, "child_dialog_id": run.ChildDialogID, "task_id": run.TaskID, "status": status}})
	a.publish(StreamMessage{Type: "goal", Payload: map[string]any{"dialog_id": run.ParentDialogID, "goal": parentGoal}})
	if reviewTask != nil {
		conversation, err := a.dialogs.Conversation(run.ProfileID, run.ParentDialogID)
		if err == nil {
			go func() {
				_ = a.startWorkflowReview(context.Background(), run.ProfileID, run.ParentDialogID, conversation.WorkspaceID, parentGoal, reviewTask)
			}()
		}
		return nil
	}
	if schedule && parentGoal.Status == "running" {
		conversation, err := a.dialogs.Conversation(run.ProfileID, run.ParentDialogID)
		if err == nil {
			go func() {
				_ = a.scheduleWorkflow(context.Background(), run.ProfileID, run.ParentDialogID, conversation.WorkspaceID, parentGoal)
			}()
		}
		return nil
	}
	if status == "success" || status == "failed" {
		if parentGoal.WorkflowID == "" {
			a.resumeParentAfterChild(run, parentGoal)
		}
	}
	return nil
}

func (a *App) resumeParentAfterChild(run childRun, parentGoal *goal.Goal) {
	if a.store == nil {
		return
	}
	conversation, err := a.dialogs.Conversation(run.ProfileID, run.ParentDialogID)
	if err != nil {
		return
	}
	a.chatMu.Lock()
	if _, running := a.chatJobs[run.ParentDialogID]; running {
		a.chatMu.Unlock()
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	job := &chatJob{cancel: cancel, done: make(chan struct{})}
	a.chatJobs[run.ParentDialogID] = job
	a.chatMu.Unlock()
	mode := "normal"
	if parentGoal.WorkflowID != "" {
		mode = "workflow"
	}
	go func() {
		defer func() {
			a.chatMu.Lock()
			if a.chatJobs[run.ParentDialogID] == job {
				delete(a.chatJobs, run.ParentDialogID)
			}
			a.chatMu.Unlock()
			cancel()
			close(job.done)
		}()
		_ = a.runChatSend(chatSendQuery{DialogID: run.ParentDialogID, WorkspaceID: conversation.WorkspaceID, AgentID: conversation.AgentID, Model: conversation.ActiveModel, Resume: true, Mode: mode, WorkflowID: parentGoal.WorkflowID, Internal: true}, ctx, "", nil)
	}()
}

func (a *App) restoreChildRuns() {
	profiles, _, err := a.store.Profiles()
	if err != nil {
		return
	}
	for _, profile := range profiles {
		conversations, listErr := a.dialogs.ProfileConversations(profile.ID)
		if listErr != nil {
			continue
		}
		for _, conversation := range conversations {
			if conversation.ParentID == "" {
				continue
			}
			parentGoal, goalErr := a.dialogs.Goal(a.dialogKey(profile.ID, conversation.ParentID))
			if goalErr != nil || parentGoal == nil {
				continue
			}
			for _, task := range parentGoal.Tasks {
				role := childRoleBuilder
				childID, active := task.ChildDialogID, task.Status == "waiting_agent"
				if task.ReviewerDialogID == conversation.ID && task.Status == "reviewing" {
					role, childID, active = childRoleReviewer, task.ReviewerDialogID, true
				}
				if childID == conversation.ID && active && task.RunTokenHash != "" {
					a.childMu.Lock()
					if _, exists := a.childRuns[conversation.ID]; !exists {
						a.childRuns[conversation.ID] = childRun{ProfileID: profile.ID, ParentDialogID: conversation.ParentID, ChildDialogID: conversation.ID, TaskID: task.ID, TokenHash: task.RunTokenHash, Role: role}
					}
					a.childMu.Unlock()
				}
			}
		}
	}
}

func (a *App) cancelChildJobs(profileID, parentID string) {
	a.childMu.RLock()
	runs := make([]childRun, 0)
	for _, run := range a.childRuns {
		if run.ProfileID == profileID && run.ParentDialogID == parentID {
			runs = append(runs, run)
		}
	}
	a.childMu.RUnlock()
	for _, run := range runs {
		a.chatMu.RLock()
		job := a.chatJobs[run.ChildDialogID]
		a.chatMu.RUnlock()
		if job != nil {
			job.cancel()
		}
		a.childMu.Lock()
		delete(a.childRuns, run.ChildDialogID)
		a.childMu.Unlock()
		parentGoal, goalErr := a.dialogs.Goal(a.dialogKey(profileID, parentID))
		if goalErr != nil || parentGoal == nil || a.appendChildProgress(run, parentGoal, "stopped", "", "workflow stopped") != nil {
			continue
		}
		a.publish(StreamMessage{Type: "subagent", Payload: map[string]any{"dialog_id": run.ParentDialogID, "child_dialog_id": run.ChildDialogID, "task_id": run.TaskID, "status": "stopped"}})
	}
	if len(runs) == 0 || a.dialogs == nil {
		return
	}
	parentGoal, err := a.dialogs.Goal(a.dialogKey(profileID, parentID))
	if err != nil || parentGoal == nil {
		return
	}
	if goal.CancelWorkflowTasks(parentGoal) {
		if err := a.dialogs.SaveGoal(a.dialogKey(profileID, parentID), *parentGoal); err == nil {
			a.publish(StreamMessage{Type: "goal", Payload: map[string]any{"dialog_id": parentID, "goal": parentGoal}})
		}
	}
}

func containsAgent(items []models.Agent, id string) bool {
	for _, item := range items {
		if item.ID == id {
			return true
		}
	}
	return false
}
