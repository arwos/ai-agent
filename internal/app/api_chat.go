/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"time"

	"go.osspkg.com/goppy/v3/plugins/ws"
	"go.osspkg.com/goppy/v3/plugins/ws/event"
	"go.osspkg.com/logx"

	"github.com/arwos/ai-agent/internal/pkg/dialog"
	"github.com/arwos/ai-agent/internal/pkg/goal"
	"github.com/arwos/ai-agent/internal/pkg/interaction"
	"github.com/arwos/ai-agent/internal/pkg/llm"
	"github.com/arwos/ai-agent/internal/pkg/mcp"
	"github.com/arwos/ai-agent/internal/pkg/models"
	"github.com/arwos/ai-agent/internal/pkg/prompts"
	"github.com/arwos/ai-agent/internal/pkg/toolexecutor"
	"github.com/arwos/ai-agent/internal/pkg/workflow"
)

type chatSendQuery struct {
	DialogID       string   `json:"dialog_id"`
	Content        string   `json:"content"`
	Provider       string   `json:"provider"`
	Model          string   `json:"model"`
	AgentID        string   `json:"agentId"`
	Skills         []string `json:"skills"`
	WorkspaceID    string   `json:"workspace_id"`
	Resume         bool     `json:"resume"`
	ErrorMessageID string   `json:"errorMessageId"`
	AsGoal         bool     `json:"asGoal"`
	Mode           string   `json:"mode"`
	WorkflowID     string   `json:"workflow_id"`
}

func validateChatMode(mode, workflowID string, asGoal bool) error {
	if mode == "" {
		mode = "normal"
	}
	if mode != "normal" && mode != "plan" && mode != "consult" && mode != "workflow" {
		return fmt.Errorf("invalid chat mode %q", mode)
	}
	if mode == "workflow" && strings.TrimSpace(workflowID) == "" {
		return fmt.Errorf("workflow_id is required in workflow mode")
	}
	if mode != "workflow" && workflowID != "" {
		return fmt.Errorf("workflow_id is only valid in workflow mode")
	}
	if asGoal && mode == "normal" {
		return nil
	}
	return nil
}

func (a *App) chatSend(ev event.Event, meta ws.Meta) error {
	var q chatSendQuery
	if err := ev.Decode(&q); err != nil {
		return err
	}
	return a.runChatSend(q, meta.Context(), meta.ConnectID(), func(value any) error { return ev.Encode(value) })
}
func goalTitle(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	runes := []rune(value)
	if len(runes) > 140 {
		return string(runes[:140]) + "…"
	}
	return value
}
func goalPlan(ctx context.Context, provider llm.Provider, dialogID, request string, availableTools []toolexecutor.Tool) (*goal.Goal, llm.Usage) {
	goal := defaultGoal(dialogID, request)
	toolNames := make([]string, 0, len(availableTools))
	for _, tool := range availableTools {
		toolNames = append(toolNames, tool.Name)
	}
	planningRequest := prompts.GoalPlanningRequest(request)
	if len(toolNames) > 0 {
		planningRequest += "\nAvailable tools (use exact names in task tools):\n" + strings.Join(toolNames, ", ")
	}
	response, err := provider.ChatCompletion(ctx, prompts.GoalPlanningSystem, []llm.Message{{Role: "user", Content: planningRequest}}, false)
	if err != nil {
		logx.Error("goal planning request failed", "dialog_id", dialogID, "err", err)
		return goal, llm.Usage{}
	}
	if err := applyGoalPlan(goal, response.Content); err != nil {
		logx.Error("goal planning response is not valid JSON", "dialog_id", dialogID, "err", err)
	}
	return goal, response.Usage
}
func defaultGoal(dialogID, request string) *goal.Goal {
	title := goalTitle(request)
	return &goal.Goal{
		ID:       dialog.NewMessageID(),
		DialogID: dialogID,
		Goal:     title,
		Request:  request,
		Status:   "running",
		Tasks:    []goal.GoalTask{{ID: dialog.NewMessageID(), Label: title, Status: "pending"}},
	}
}
func applyGoalPlan(planGoal *goal.Goal, content string) error {
	start, end := strings.Index(content, "{"), strings.LastIndex(content, "}")
	if start < 0 || end <= start {
		return fmt.Errorf("goal plan does not contain a JSON object")
	}
	var plan struct {
		Goal  string `json:"goal"`
		Tasks []struct {
			Label     string   `json:"label"`
			Tools     []string `json:"tools"`
			DependsOn []string `json:"dependsOn"`
		} `json:"tasks"`
	}
	if err := json.Unmarshal([]byte(content[start:end+1]), &plan); err != nil {
		return err
	}
	if strings.TrimSpace(plan.Goal) != "" {
		planGoal.Goal = goalTitle(plan.Goal)
	}
	planned := make([]goal.GoalTask, 0, min(len(plan.Tasks), 8))
	for _, task := range plan.Tasks {
		label := strings.Join(strings.Fields(task.Label), " ")
		if label == "" || len(planned) >= 8 {
			continue
		}
		tools := uniqueStrings(task.Tools)
		planned = append(planned, goal.GoalTask{ID: dialog.NewMessageID(), Label: label, Tools: tools, DependsOn: task.DependsOn, MaxAttempts: 3, Status: "pending"})
	}
	if len(planned) > 0 {
		planGoal.Tasks = planned
	}
	if len(planGoal.Tasks) == 0 {
		planGoal.Tasks = append(planGoal.Tasks, goal.GoalTask{ID: dialog.NewMessageID(), Label: planGoal.Goal, Status: "pending"})
	}
	return nil
}
func addChatUsage(first, second llm.Usage) llm.Usage {
	second.PromptTokens += first.PromptTokens
	second.CompletionTokens += first.CompletionTokens
	second.TotalTokens += first.TotalTokens
	if second.TotalTokens == 0 {
		second.TotalTokens = second.PromptTokens + second.CompletionTokens
	}
	return second
}
func truncateGoalResult(value string) string {
	runes := []rune(value)
	if len(runes) > 2000 {
		return string(runes[:2000]) + "…"
	}
	return value
}
func cloneToolArguments(value map[string]any) map[string]any {
	if len(value) == 0 {
		return map[string]any{}
	}
	// Tool arguments originate from JSON, so a marshal round trip is a small,
	// reliable deep copy for nested arrays and objects as well.
	encoded, err := json.Marshal(value)
	if err != nil {
		return map[string]any{}
	}
	copy := make(map[string]any, len(value))
	if err := json.Unmarshal(encoded, &copy); err != nil {
		return map[string]any{}
	}
	return copy
}
func goalHasExecutablePending(goal *goal.Goal) bool {
	for _, task := range goal.Tasks {
		if task.Status == "pending" && len(task.Tools) > 0 {
			return true
		}
	}
	return false
}
func goalAllDone(goal *goal.Goal) bool {
	if len(goal.Tasks) == 0 {
		return false
	}
	for _, task := range goal.Tasks {
		if task.Status != "done" {
			return false
		}
	}
	return true
}
func (a *App) dialogHistory(ev event.Event, _ ws.Meta) error {
	var q struct {
		WorkspaceID string `json:"workspace_id"`
		DialogID    string `json:"dialog_id"`
		Limit       int    `json:"limit"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	if q.DialogID == "" {
		return fmt.Errorf("dialog_id is required")
	}
	if q.Limit == 0 {
		q.Limit = a.dialogs.HistoryLimit
	}
	profile, err := a.activeProfile()
	if err != nil {
		return err
	}
	history, err := a.dialogs.History(dialog.SessionKey(profile, q.DialogID), q.Limit)
	if err != nil {
		return err
	}
	return ev.Encode(history)
}
func (a *App) runChatSend(q chatSendQuery, ctx context.Context, connectionID string, encode func(any) error) error { //nolint:gocyclo // chat orchestration state machine
	startedAt := time.Now()
	if q.DialogID == "" || (!q.Resume && q.Content == "") {
		return fmt.Errorf("dialog_id and content are required")
	}
	if q.Mode == "" {
		// Older clients used asGoal for the planning flow. An explicitly
		// supplied mode always takes precedence over that compatibility flag.
		if q.AsGoal {
			q.Mode = "plan"
		} else {
			q.Mode = "normal"
		}
	}
	if err := validateChatMode(q.Mode, q.WorkflowID, q.AsGoal); err != nil {
		return err
	}
	if q.WorkspaceID == "" {
		q.WorkspaceID = "default"
	}
	workspaceService, e := a.workspaces.Get(q.WorkspaceID)
	if e != nil {
		return e
	}
	publicDialogID := q.DialogID
	if a.store == nil {
		return fmt.Errorf("configuration store unavailable")
	}
	profileID, e := a.activeProfile()
	if e != nil {
		return e
	}
	q.DialogID = dialog.SessionKey(profileID, publicDialogID)
	var resumedGoal *goal.Goal
	if q.Resume {
		resumedGoal, e = a.dialogs.Goal(q.DialogID)
		if e != nil {
			return fmt.Errorf("load dialog goal for continuation: %w", e)
		}
		if resumedGoal != nil {
			resumedGoal.Status = "running"
			for index := range resumedGoal.Tasks {
				if resumedGoal.Tasks[index].Status == "failed" {
					resumedGoal.Tasks[index].Status = "pending"
				}
			}
			if saveErr := a.dialogs.SaveGoal(q.DialogID, *resumedGoal); saveErr != nil {
				return fmt.Errorf("reopen dialog goal for continuation: %w", saveErr)
			}
		}
	}
	// A goal represents one concrete agent run. A fresh user request replaces a
	// completed/abandoned goal only when the run actually starts using tools.
	if !q.Resume {
		if clearErr := a.dialogs.DeleteGoal(q.DialogID); clearErr != nil {
			return fmt.Errorf("clear previous dialog goal: %w", clearErr)
		}
		if a.publish != nil {
			a.publish(StreamMessage{Type: "goal.clear", Payload: map[string]string{"dialog_id": publicDialogID}})
		}
	}
	if q.Resume && q.ErrorMessageID != "" {
		if resolveErr := a.dialogs.ResolveError(q.DialogID, q.ErrorMessageID); resolveErr != nil {
			return fmt.Errorf("resolve previous error in history: %w", resolveErr)
		}
	}
	existingHistory, historyErr := a.dialogs.History(q.DialogID, 1)
	if historyErr != nil {
		return historyErr
	}
	newConversation := len(existingHistory) == 0
	if q.Resume {
		fullHistory, readErr := a.dialogs.History(q.DialogID, 0)
		if readErr != nil {
			return readErr
		}
		for index := len(fullHistory) - 1; index >= 0; index-- {
			if fullHistory[index].Role == "user" && strings.TrimSpace(fullHistory[index].Content) != "" {
				q.Content = fullHistory[index].Content
				break
			}
		}
		if q.Content == "" {
			return fmt.Errorf("dialog has no user message to continue")
		}
	}
	conversationID := publicDialogID
	if _, e = a.store.ConversationUpsert(models.Conversation{ID: conversationID, ProfileID: profileID, WorkspaceID: q.WorkspaceID, AgentID: q.AgentID, Title: truncateTitle(q.Content)}); e != nil {
		return e
	}
	conversation, e := a.store.Conversation(profileID, conversationID)
	if e != nil {
		return e
	}
	if q.Resume && q.Model != "" {
		conversation.ActiveModel = q.Model
		if e = a.store.ConversationSetActiveModel(profileID, conversationID, q.Model); e != nil {
			return e
		}
	}
	profileProvider, apiKey, e := a.store.DefaultProfileProvider(profileID)
	if q.Provider != "" {
		profileProvider, apiKey, e = a.store.ProfileProviderSecret(profileID, q.Provider)
	}
	if e != nil {
		return fmt.Errorf("provider is not configured: %w", e)
	}
	system := prompts.DefaultAgentSystem
	explicitSkills := make(map[string]bool, len(q.Skills))
	for _, skillID := range q.Skills {
		explicitSkills[skillID] = true
	}
	model := ""
	mainModels := []string(nil)
	var selectedMCPIDs []string
	if q.AgentID != "" {
		if agents, x := a.store.Agents(profileID); x == nil {
			for _, candidate := range agents {
				if candidate.ID == q.AgentID {
					// Keep the built-in operating instructions and prepend the
					// agent-specific prompt as the first part of the system prompt.
					// An empty custom prompt must not add an empty message.
					if strings.TrimSpace(candidate.SystemPrompt) != "" {
						system = candidate.SystemPrompt + "\n\n" + system
					}
					mainModels = append(mainModels, candidate.MainModels...)
					groups, groupErr := a.skills.Groups(profileID)
					if groupErr != nil {
						return groupErr
					}
					allowedGroups := make(map[string]bool, len(candidate.SkillGroupIDs))
					for _, groupID := range candidate.SkillGroupIDs {
						allowedGroups[groupID] = true
					}
					for _, group := range groups {
						if allowedGroups[group.ID] {
							q.Skills = append(q.Skills, group.SkillIDs...)
						}
					}
					selectedMCPIDs = candidate.MCPIDs
					break
				}
			}
		}
	}
	// Explicit skill mentions are resolved before the model request. This makes
	// a skill reference authoritative instead of relying on the model to decide
	// whether it should call skills.get itself.
	managed, _ := a.skills.List(profileID)
	mentionedSkills := make([]models.Skill, 0)
	messageText := strings.ToLower(q.Content)
	for _, skill := range managed {
		if !skill.Enabled || skill.Name == "" {
			continue
		}
		name := strings.ToLower(skill.Name)
		if strings.Contains(messageText, "@"+name) || strings.Contains(messageText, name) {
			mentionedSkills = append(mentionedSkills, skill)
			q.Skills = append(q.Skills, skill.ID)
		}
	}
	extraTools := []toolexecutor.Tool(nil)
	mcpBindings := map[string]toolexecutor.MCPTool(nil)
	builtinAliases := map[string]string(nil)
	builtinAllowed := map[string]bool(nil)
	workspaceAccess := false
	if servers, mcpErr := a.store.ProfileMCP(profileID); mcpErr == nil {
		for _, server := range servers {
			for _, selected := range selectedMCPIDs {
				if selected == server.ID && server.Instructions != "" {
					system += prompts.MCPInstructions(server.Name, server.Instructions)
				}
			}
		}
		extraTools, mcpBindings = mcp.AgentTools(servers, selectedMCPIDs)
	}
	if q.Mode == "consult" {
		allowed := map[string]bool{"fs.list_dir": true, "fs.read_file": true, "fs.file_info": true, "fs.grep": true, "git.status": true, "git.diff": true, "git.log": true}
		filtered := extraTools[:0]
		for _, tool := range extraTools {
			if allowed[tool.Name] {
				filtered = append(filtered, tool)
			}
		}
		extraTools = filtered
		for name := range mcpBindings {
			if !allowed[name] {
				delete(mcpBindings, name)
			}
		}
	}
	// A Builder receives only the resources declared by its immutable task
	// snapshot. The scheduler, rather than the Builder model, owns everything
	// else in the workflow.
	if child, ok := a.childRun(publicDialogID); ok && child.Role == childRoleBuilder {
		parentGoal, goalErr := a.dialogs.Goal(dialog.SessionKey(profileID, child.ParentDialogID))
		if goalErr != nil || parentGoal == nil {
			return fmt.Errorf("load workflow child goal: %w", goalErr)
		}
		var task *goal.GoalTask
		for index := range parentGoal.Tasks {
			if parentGoal.Tasks[index].ID == child.TaskID {
				task = &parentGoal.Tasks[index]
				break
			}
		}
		if task == nil {
			return fmt.Errorf("workflow child task not found")
		}
		allowed := make(map[string]bool, len(task.Tools))
		for _, name := range task.Tools {
			allowed[name] = true
		}
		filterTools := extraTools[:0]
		for _, tool := range extraTools {
			if allowed[tool.Name] {
				filterTools = append(filterTools, tool)
			}
		}
		extraTools = filterTools
		for name := range mcpBindings {
			if !allowed[name] {
				delete(mcpBindings, name)
			}
		}
		for alias, target := range builtinAliases {
			if !allowed[alias] && !allowed[target] {
				delete(builtinAliases, alias)
				delete(builtinAllowed, target)
			}
		}
		q.Skills = append([]string(nil), task.Skills...)
		mentionedSkills = nil
	}
	system += prompts.Mode(q.Mode)
	if q.Mode == "workflow" {
		if a.workflows == nil {
			return fmt.Errorf("workflow service unavailable")
		}
		definition, workflowErr := a.workflows.Get(profileID, q.WorkflowID)
		if workflowErr != nil {
			return fmt.Errorf("load workflow: %w", workflowErr)
		}
		if !definition.Enabled {
			return fmt.Errorf("workflow %q is disabled", q.WorkflowID)
		}
	}
	if builtins, builtinErr := a.builtinMCP(profileID); builtinErr == nil {
		selected := make(map[string]bool, len(selectedMCPIDs))
		for _, id := range selectedMCPIDs {
			selected[id] = true
		}
		builtinAliases, builtinAllowed = make(map[string]string), make(map[string]bool)
		for _, server := range builtins {
			// Built-in MCP servers are regular agent links. The global profile
			// enabled flag is not enough: a server must also be selected on the
			// agent, otherwise neither its prompt schema nor its executor binding
			// is exposed to this dialogue.
			// User interaction is a platform capability, not an optional model
			// tool: without it the model can only print choices as prose and the
			// UI cannot render a dialog. Always expose it to the agent.
			if !selected[server.ID] && server.BuiltinKey != "user" {
				continue
			}
			tools, aliases := builtinPromptTools(server)
			if server.BuiltinKey == "workspace" && len(tools) > 0 {
				workspaceAccess = true
			}
			extraTools = append(extraTools, tools...)
			for alias, target := range aliases {
				builtinAliases[alias], builtinAllowed[target] = target, true
			}
		}
	}
	if q.Mode == "consult" {
		allowed := map[string]bool{"fs.list_dir": true, "fs.read_file": true, "fs.file_info": true, "fs.grep": true, "git.status": true, "git.diff": true, "git.log": true}
		filtered := extraTools[:0]
		for _, tool := range extraTools {
			if allowed[tool.Name] {
				filtered = append(filtered, tool)
			}
		}
		extraTools = filtered
		for alias, target := range builtinAliases {
			if !allowed[alias] && !allowed[target] {
				delete(builtinAliases, alias)
				delete(builtinAllowed, target)
			}
		}
	}
	if child, ok := a.childRun(publicDialogID); ok && child.Role == childRoleBuilder {
		parentGoal, goalErr := a.dialogs.Goal(dialog.SessionKey(profileID, child.ParentDialogID))
		if goalErr != nil || parentGoal == nil {
			return fmt.Errorf("load workflow child goal: %w", goalErr)
		}
		allowed := map[string]bool{}
		for _, task := range parentGoal.Tasks {
			if task.ID == child.TaskID {
				for _, name := range task.Tools {
					allowed[name] = true
				}
				break
			}
		}
		filtered := extraTools[:0]
		for _, tool := range extraTools {
			if allowed[tool.Name] {
				filtered = append(filtered, tool)
			}
		}
		extraTools = filtered
		for alias, target := range builtinAliases {
			if !allowed[alias] && !allowed[target] {
				delete(builtinAliases, alias)
				delete(builtinAllowed, target)
			}
		}
	}
	if workspaceAccess {
		system += prompts.WorkspaceAccess
		gitBranch := ""
		if head, headErr := workspaceService.ReadFile(".git/HEAD"); headErr == nil {
			gitBranch = strings.TrimPrefix(strings.TrimSpace(head), "ref: refs/heads/")
		}
		system += prompts.Environment(q.WorkspaceID, workspaceService.Path, runtime.GOOS+"/"+runtime.GOARCH, time.Now().UTC().Format("2006-01-02"), gitBranch)
	}
	if child, ok := a.childRun(publicDialogID); ok {
		if child.Role == childRoleReviewer {
			extraTools = append(extraTools, toolexecutor.Tool{Name: "workflow.review_result", Description: "Submit the independent workflow review verdict."})
		} else {
			extraTools = append(extraTools, toolexecutor.Tool{Name: "agent.result", Description: "Submit the canonical result to the parent agent."})
		}
	} else {
		extraTools = append(extraTools,
			toolexecutor.Tool{Name: "agent.list", Description: "List available profile agents without secret configuration."},
			toolexecutor.Tool{Name: "agent.run", Description: "Delegate one bounded task to a profile-scoped agent."},
		)
	}
	if q.Mode != "consult" {
		extraTools = append(extraTools,
			toolexecutor.Tool{Name: "artifact.create", Description: "Create an immutable artifact snapshot from a workspace file."},
			toolexecutor.Tool{Name: "artifact.get", Description: "Inspect immutable artifact metadata."},
			toolexecutor.Tool{Name: "artifact.read", Description: "Read an immutable artifact snapshot."},
		)
	}
	if a.tools != nil {
		system += a.tools.Prompt(extraTools)
	}
	if memory, memoryErr := a.memory.Get(profileID, q.WorkspaceID, publicDialogID); memoryErr == nil {
		system += prompts.SessionMemory(memory.Title, memory.Summary)
	}
	selectedSkillCatalog := make([]string, 0, len(q.Skills))
	for _, selected := range q.Skills {
		for _, skill := range managed {
			if skill.Enabled && (skill.ID == selected || skill.Name == selected) {
				selectedSkillCatalog = append(selectedSkillCatalog, fmt.Sprintf("%s (id: %s): %s", skill.Name, skill.ID, skill.Description))
				break
			}
		}
	}
	if len(selectedSkillCatalog) > 0 {
		system += prompts.SkillCatalog(selectedSkillCatalog)
	}
	for _, skill := range mentionedSkills {
		if content, readErr := a.skills.Get(profileID, skill.ID); readErr == nil && strings.TrimSpace(content) != "" {
			system += prompts.Skill(skill.Name, content)
		}
	}
	if newConversation {
		system += prompts.StartupIntroduction
	}
	mainModels = uniqueStrings(mainModels)
	if conversation.ActiveModel != "" && (len(mainModels) == 0 || containsString(mainModels, conversation.ActiveModel)) {
		model = conversation.ActiveModel
	} else if len(mainModels) > 0 {
		model = mainModels[0]
		if conversation.ActiveModel != "" {
			// Discard selections from the removed fallback-model format.
			_ = a.store.ConversationSetActiveModel(profileID, conversationID, "")
		}
	} else if len(profileProvider.Models) > 0 {
		model = profileProvider.Models[0]
	}
	// Agent model selections use the stable public form model@provider.
	// Resolve the provider here and pass only the model name to the LLM client.
	if at := strings.LastIndex(model, "@"); at > 0 && at < len(model)-1 {
		modelName, providerName := model[:at], model[at+1:]
		selected, selectedKey, lookupErr := a.store.ProfileProviderSecret(profileID, providerName)
		if lookupErr != nil {
			return fmt.Errorf("provider %q is not configured: %w", providerName, lookupErr)
		}
		profileProvider, apiKey, model = selected, selectedKey, modelName
	} else if slash := strings.Index(model, "/"); slash > 0 {
		// Accept the old provider/model form for agents saved by older builds.
		if selected, selectedKey, lookupErr := a.store.ProfileProviderSecret(profileID, model[:slash]); lookupErr == nil {
			profileProvider, apiKey, model = selected, selectedKey, model[slash+1:]
		}
	}
	var provider llm.Provider = llm.New(profileProvider.Kind, profileProvider.BaseURL, apiKey, model, a.providerProxy(profileID, profileProvider.ProxyID), llm.WithRPM(profileProvider.ID, profileProvider.RPM))
	activeRateLimitKey, activeRPM := profileProvider.ID, profileProvider.RPM
	activeGoal := resumedGoal
	if q.Mode == "workflow" && !q.Resume {
		definition, workflowErr := a.workflows.Get(profileID, q.WorkflowID)
		if workflowErr != nil {
			return fmt.Errorf("load workflow snapshot: %w", workflowErr)
		}
		if q.AgentID == "" {
			return fmt.Errorf("reviewer agent is required for workflow mode")
		}
		agents, agentsErr := a.store.Agents(profileID)
		if agentsErr != nil {
			return fmt.Errorf("load workflow agents: %w", agentsErr)
		}
		if !containsAgent(agents, q.AgentID) {
			return fmt.Errorf("reviewer agent %q not found", q.AgentID)
		}
		for _, step := range definition.Steps {
			if !containsAgent(agents, step.AgentID) {
				return fmt.Errorf("builder agent %q for workflow step %q not found", step.AgentID, step.ID)
			}
		}
		workflowHash, hashErr := workflow.Hash(definition)
		if hashErr != nil {
			return fmt.Errorf("hash workflow snapshot: %w", hashErr)
		}
		snapshot, marshalErr := json.Marshal(definition)
		if marshalErr != nil {
			return fmt.Errorf("encode workflow snapshot: %w", marshalErr)
		}
		activeGoal = defaultGoal(publicDialogID, q.Content)
		activeGoal.WorkflowID, activeGoal.WorkflowVersion, activeGoal.WorkflowHash = definition.ID, definition.Version, workflowHash
		activeGoal.Status = "awaiting_approval"
		activeGoal.Tasks = make([]goal.GoalTask, 0, len(definition.Steps))
		workflowGroups, groupsErr := a.skills.Groups(profileID)
		if groupsErr != nil {
			return fmt.Errorf("load workflow skill groups: %w", groupsErr)
		}
		for _, step := range definition.Steps {
			skillIDs := make([]string, 0)
			for _, groupID := range step.RequiredSkills {
				for _, group := range workflowGroups {
					if group.ID == groupID {
						skillIDs = append(skillIDs, group.SkillIDs...)
						break
					}
				}
			}
			activeGoal.Tasks = append(activeGoal.Tasks, goal.GoalTask{ID: dialog.NewMessageID(), Label: step.Title, Tools: append([]string(nil), step.RequiredTools...), Skills: skillIDs, DependsOn: append([]string(nil), step.Dependencies...), WorkflowStepID: step.ID, WorkflowSnapshot: append([]byte(nil), snapshot...), ExecutorAgentID: step.AgentID, ReviewerAgentID: q.AgentID, Acceptance: step.Acceptance, MaxAttempts: step.Retry.MaxAttempts, Status: "pending"})
		}
	}
	publishGoal := func(goal *goal.Goal) {
		if a.publish != nil && goal != nil {
			a.publish(StreamMessage{Type: "goal", Payload: map[string]any{"dialog_id": publicDialogID, "goal": goal}})
		}
	}
	if activeGoal != nil {
		publishGoal(activeGoal)
	}
	planningUsage := llm.Usage{}
	if q.Mode == "plan" && !q.Resume {
		activeGoal, planningUsage = goalPlan(ctx, provider, publicDialogID, q.Content, extraTools)
		activeGoal.Status = "awaiting_approval"
	}
	if (q.Mode == "plan" || q.Mode == "workflow") && !q.Resume {
		if saveErr := a.dialogs.SaveGoal(q.DialogID, *activeGoal); saveErr != nil {
			return fmt.Errorf("save dialog goal: %w", saveErr)
		}
		publishGoal(activeGoal)
		approvalID := streamMessageID()
		activeGoal.Approval = &goal.GoalApproval{ID: approvalID, Kind: "approval", Title: "Approve execution plan", Detail: activeGoal.Goal, Command: "goal.start", CreatedAt: time.Now().UTC()}
		if saveErr := a.dialogs.SaveGoal(q.DialogID, *activeGoal); saveErr != nil {
			return fmt.Errorf("save plan approval: %w", saveErr)
		}
		publishGoal(activeGoal)
		answer, approvalErr := a.requestUser(ctx, connectionID, publicDialogID, map[string]any{"reqId": approvalID, "kind": "approval", "title": "Approve execution plan", "detail": activeGoal.Goal, "command": "goal.start"})
		if approvalErr != nil {
			return approvalErr
		}
		accepted, ok := answer.(bool)
		if !ok || !accepted {
			activeGoal.Status = "paused"
			activeGoal.Approval = nil
			if saveErr := a.dialogs.SaveGoal(q.DialogID, *activeGoal); saveErr != nil {
				return saveErr
			}
			publishGoal(activeGoal)
			return nil
		}
		activeGoal.Status = "running"
		activeGoal.Approval = nil
		if saveErr := a.dialogs.SaveGoal(q.DialogID, *activeGoal); saveErr != nil {
			return saveErr
		}
	}
	if q.Mode == "workflow" && activeGoal != nil {
		if q.AgentID == "" {
			return fmt.Errorf("reviewer agent is required for workflow mode")
		}
		return a.scheduleWorkflow(ctx, profileID, publicDialogID, q.WorkspaceID, activeGoal)
	}
	if activeGoal != nil {
		tasks := make([]string, 0, len(activeGoal.Tasks))
		for index, task := range activeGoal.Tasks {
			tasks = append(tasks, fmt.Sprintf("%d. %s", index+1, task.Label))
		}
		system += prompts.GoalExecution(activeGoal.Goal, tasks)
	}
	engine := a.engines.Acquire(provider, appToolExecutor{registry: a.tools, scope: toolexecutor.Scope{WorkspaceID: q.WorkspaceID, ProfileID: profileID, DialogID: q.DialogID, MCPTools: mcpBindings, Aliases: builtinAliases, Allowed: builtinAllowed, Mode: q.Mode, MCPCall: a.callMCPTool, Scoped: a.agentTools(profileID, publicDialogID, publicDialogID, q.WorkspaceID, q.Mode), Request: func(ctx context.Context, args map[string]any) (any, error) {
		return a.requestUser(ctx, connectionID, publicDialogID, args)
	}}}, a.dialogs, system)
	engine.Model = model
	engine.ProviderName = profileProvider.Name
	engine.Resume = q.Resume
	engine.ToolDefinitions = make([]llm.ToolDefinition, 0, len(extraTools))
	for _, tool := range extraTools {
		engine.ToolDefinitions = append(engine.ToolDefinitions, llm.ToolDefinition{Name: tool.Name, Description: tool.Description, Parameters: tool.InputSchema})
	}
	if instructions, readErr := workspaceService.ReadFile("AGENTS.md"); readErr == nil && strings.TrimSpace(instructions) != "" {
		engine.SystemContext = "Repository instructions from the active workspace root (AGENTS.md):\n\n" + instructions
	}
	defer a.engines.Release(engine)
	engine.OnTool = func(toolModel, toolProvider, name string, args map[string]any, result string, x error) {
		toolLabel := name
		if description := a.tools.Description(name); description != "" {
			toolLabel = description
		} else {
			for _, definition := range extraTools {
				if definition.Name == name && definition.Description != "" {
					toolLabel = definition.Description
					break
				}
			}
		}
		if result == "" && x == nil && activeGoal != nil {
			if q.Mode != "workflow" {
				started := false
				for index := range activeGoal.Tasks {
					task := &activeGoal.Tasks[index]
					if task.Status != "pending" || (len(task.Tools) > 0 && !containsString(task.Tools, name)) {
						continue
					}
					task.Status = "running"
					// Planned tasks may have been created from the model's technical
					// tool name. Replace it with the registered human-readable purpose
					// whenever the tool actually starts, including goals restored from
					// an older history.
					task.Label = toolLabel
					task.Attempts++
					task.LastTool = name
					task.Arguments = cloneToolArguments(args)
					task.Error = ""
					task.StartedAt = time.Now().UTC()
					started = true
					break
				}
				if !started {
					activeGoal.Tasks = append(activeGoal.Tasks, goal.GoalTask{ID: dialog.NewMessageID(), Label: toolLabel, Tools: []string{name}, Attempts: 1, MaxAttempts: 3, Status: "running", StartedAt: time.Now().UTC()})
				}
				if saveErr := a.dialogs.SaveGoal(q.DialogID, *activeGoal); saveErr != nil {
					logx.Error("failed to save dialog goal", "dialog_id", publicDialogID, "err", saveErr)
				} else {
					publishGoal(activeGoal)
				}
			}
		} else if activeGoal != nil {
			for index := len(activeGoal.Tasks) - 1; index >= 0; index-- {
				task := &activeGoal.Tasks[index]
				if task.Status != "running" || (q.Mode == "workflow" && task.LastTool != name) {
					continue
				}
				if x != nil {
					if q.Mode == "workflow" {
						_ = goal.CompleteWorkflowTask(activeGoal, task.ID, "", "", x)
					} else {
						task.Status = "failed"
						task.Error = x.Error()
						if task.MaxAttempts > task.Attempts {
							task.Status = "pending"
						}
					}
				} else {
					if q.Mode == "workflow" {
						_ = goal.CompleteWorkflowTask(activeGoal, task.ID, "", result, nil)
					} else {
						task.Status = "done"
					}
				}
				task.LastResult = truncateGoalResult(result)
				task.FinishedAt = time.Now().UTC()
				break
			}
			if saveErr := a.dialogs.SaveGoal(q.DialogID, *activeGoal); saveErr != nil {
				logx.Error("failed to update dialog goal", "dialog_id", publicDialogID, "err", saveErr)
			} else {
				publishGoal(activeGoal)
			}
		}
		if a.publish != nil {
			phase := "done"
			if result == "" && x == nil {
				phase = "start"
			}
			payload := map[string]any{"dialog_id": publicDialogID, "name": name, "arguments": args, "result": result, "phase": phase, "model": toolModel, "provider": toolProvider}
			if x != nil {
				payload["error"] = x.Error()
			}
			a.publish(StreamMessage{Type: "tool_call", Payload: payload})
		}
	}
	engine.OnReasoning = func(text string) {
		if a.publish != nil {
			a.publish(StreamMessage{Type: "reasoning", Payload: map[string]any{"dialog_id": publicDialogID, "text": text, "model": engine.Model, "provider": engine.ProviderName}})
		}
	}
	replayGoalEvidence := func() error {
		if activeGoal == nil {
			return nil
		}
		for index := range activeGoal.Tasks {
			task := &activeGoal.Tasks[index]
			if task.Status != "pending" || task.LastTool == "" || strings.HasPrefix(task.LastTool, "user.") {
				continue
			}
			if _, replayErr := engine.ExecuteTool(ctx, q.DialogID, llm.ToolCall{Name: task.LastTool, Arguments: cloneToolArguments(task.Arguments)}); replayErr != nil {
				return replayErr
			}
		}
		return nil
	}
	requestProseChoice := func(content string) (bool, error) {
		if activeGoal == nil || goalAllDone(activeGoal) {
			return false, nil
		}
		request, inferred := interaction.InferChoice(content)
		if !inferred {
			return false, nil
		}
		requestID := streamMessageID()
		request["reqId"] = requestID
		call := llm.ToolCall{Name: "user.choice", Arguments: cloneToolArguments(request)}
		engine.OnTool(engine.Model, engine.ProviderName, call.Name, call.Arguments, "", nil)
		if appendErr := a.dialogs.Append(q.DialogID, dialog.Message{Role: "tool_call", Tool: call.Name, Arguments: call.Arguments, Model: engine.Model, Provider: engine.ProviderName}); appendErr != nil {
			return true, appendErr
		}
		answer, requestErr := a.requestUser(ctx, connectionID, publicDialogID, request)
		if requestErr != nil {
			return true, requestErr
		}
		encoded, marshalErr := json.Marshal(map[string]any{"selection": answer})
		if marshalErr != nil {
			return true, marshalErr
		}
		result := string(encoded)
		engine.OnTool(engine.Model, engine.ProviderName, call.Name, call.Arguments, result, nil)
		if appendErr := a.dialogs.Append(q.DialogID, dialog.Message{Role: "tool_result", Tool: call.Name, Content: result, Model: engine.Model, Provider: engine.ProviderName}); appendErr != nil {
			return true, appendErr
		}
		return true, nil
	}
	streamed := false
	engine.OnChunk = func(text string) {
		streamed = true
		if a.publish != nil {
			a.publish(StreamMessage{Type: "chat_stream", Payload: map[string]string{"dialog_id": publicDialogID, "chunk": text}})
			a.publish(StreamMessage{Type: "chunk", Payload: map[string]string{"dialog_id": publicDialogID, "text": text}})
		}
	}
	if q.Resume && activeGoal != nil && goalHasExecutablePending(activeGoal) {
		// A previously interrupted or declined goal can be resumed after a page
		// or backend restart. Reuse the recorded evidence-gathering calls before
		// asking the model for its next turn.
		if replayErr := replayGoalEvidence(); replayErr != nil {
			return fmt.Errorf("replay goal evidence: %w", replayErr)
		}
	}
	if a.publish != nil {
		a.publish(StreamMessage{Type: "status", Payload: map[string]string{"dialog_id": publicDialogID, "agent_id": q.AgentID, "status": "thinking"}})
		a.publish(StreamMessage{Type: "msg.start", Payload: map[string]string{"dialog_id": publicDialogID, "agent_id": q.AgentID, "model": engine.Model, "provider": engine.ProviderName}})
	}
	out, usage, e := engine.Run(ctx, q.DialogID, q.Content)
	usage = addChatUsage(planningUsage, usage)
	if e == nil {
		if requested, requestErr := requestProseChoice(out); requestErr != nil {
			e = requestErr
		} else if requested {
			// The model produced a prose menu instead of user.choice. The
			// synthetic tool result is now in history, so continue the same goal
			// with the selected value rather than ending it at the question.
			engine.Resume = true
			var continuationUsage llm.Usage
			out, continuationUsage, e = engine.Run(ctx, q.DialogID, "")
			usage = addChatUsage(usage, continuationUsage)
		}
	}
	for retry := 0; e == nil && retry < 2 && activeGoal != nil && goalHasExecutablePending(activeGoal); retry++ {
		// Resume from the persisted conversation without adding a synthetic
		// user message. The goal instructions remain in the system prompt.
		engine.Resume = true
		_, retryUsage, retryErr := engine.Run(ctx, q.DialogID, "")
		usage = addChatUsage(usage, retryUsage)
		e = retryErr
	}
	completionApproved := false
	if e == nil && activeGoal != nil && goalAllDone(activeGoal) {
		for approvalAttempt := 0; approvalAttempt < 2; approvalAttempt++ {
			// A declined approval may require several model/tool turns before all
			// reopened tasks are actually completed. Never ask for approval again
			// while the persisted plan still contains unfinished work.
			if !goalAllDone(activeGoal) {
				break
			}
			approvalID := streamMessageID()
			activeGoal.Status = "awaiting_approval"
			activeGoal.Approval = &goal.GoalApproval{
				ID: approvalID, Kind: "approval", Title: "Confirm goal completion",
				Detail: activeGoal.Goal, Command: "goal.complete", CreatedAt: time.Now().UTC(),
			}
			if saveErr := a.dialogs.SaveGoal(q.DialogID, *activeGoal); saveErr != nil {
				return fmt.Errorf("save goal approval: %w", saveErr)
			}
			publishGoal(activeGoal)
			if a.publish != nil {
				a.publish(StreamMessage{Type: "status", Payload: map[string]string{"dialog_id": publicDialogID, "agent_id": q.AgentID, "status": "awaiting_approval"}})
			}
			answer, requestErr := a.requestUser(ctx, connectionID, publicDialogID, map[string]any{
				"reqId": approvalID, "kind": "approval", "title": "Confirm goal completion", "detail": activeGoal.Goal,
				"command": "goal.complete",
			})
			if requestErr != nil {
				e = requestErr
				break
			}
			accepted, ok := answer.(bool)
			if ok && accepted {
				completionApproved = true
				activeGoal.Approval = nil
				_ = a.dialogs.SaveGoal(q.DialogID, *activeGoal)
				break
			}
			// A decline reopens every executable task and makes one verification
			// pass. The model must inspect the workspace again before asking for
			// completion approval a second time.
			for index := range activeGoal.Tasks {
				task := &activeGoal.Tasks[index]
				if len(task.Tools) == 0 {
					continue
				}
				task.Status, task.Error, task.LastResult = "pending", "", ""
				task.Attempts = 0
			}
			activeGoal.Status = "running"
			activeGoal.Approval = nil
			_ = a.dialogs.SaveGoal(q.DialogID, *activeGoal)
			publishGoal(activeGoal)
			engine.SystemPrompt += prompts.GoalVerification
			engine.Resume = true
			// Re-run the exact evidence-gathering operations from the completed
			// plan. Asking the model to remember which tools it used is unreliable
			// and previously left every task pending after a declined confirmation.
			if replayErr := replayGoalEvidence(); replayErr != nil {
				e = replayErr
				break
			}
			for verificationAttempt := 0; verificationAttempt < 3 && e == nil && !goalAllDone(activeGoal); verificationAttempt++ {
				_, verifyUsage, verifyErr := engine.Run(ctx, q.DialogID, "")
				usage = addChatUsage(usage, verifyUsage)
				e = verifyErr
				if e == nil && !goalAllDone(activeGoal) && !goalHasExecutablePending(activeGoal) {
					// There is no executable step left for another useful turn.
					break
				}
			}
		}
	}
	if activeGoal != nil {
		if e == nil && completionApproved {
			activeGoal.Status = "done"
			for index := range activeGoal.Tasks {
				switch activeGoal.Tasks[index].Status {
				case "running":
					activeGoal.Tasks[index].Status = "done"
				case "pending", "skipped":
					activeGoal.Status = "incomplete"
				}
			}
		} else if e == nil {
			activeGoal.Status = "incomplete"
		} else if errors.Is(e, context.Canceled) {
			// Pause/stop commands persist their desired state before cancelling
			// the job; do not turn a paused goal into stopped here.
			if activeGoal.Status != "paused" && activeGoal.Status != "stopped" {
				activeGoal.Status = "stopped"
			}
			for index := range activeGoal.Tasks {
				if activeGoal.Tasks[index].Status == "running" {
					activeGoal.Tasks[index].Status = "skipped"
				}
			}
		} else {
			activeGoal.Status = "failed"
			for index := range activeGoal.Tasks {
				if activeGoal.Tasks[index].Status == "running" {
					activeGoal.Tasks[index].Status = "failed"
				}
			}
		}
		if saveErr := a.dialogs.SaveGoal(q.DialogID, *activeGoal); saveErr != nil {
			logx.Error("failed to finalize dialog goal", "dialog_id", publicDialogID, "err", saveErr)
		} else {
			publishGoal(activeGoal)
		}
	}
	if e != nil {
		if errors.Is(e, context.Canceled) {
			if a.publish != nil {
				a.publish(StreamMessage{Type: "status", Payload: map[string]string{"dialog_id": publicDialogID, "agent_id": q.AgentID, "status": "idle"}})
				a.publish(StreamMessage{Type: "done", Payload: map[string]any{"dialog_id": publicDialogID, "tokens": usage.TotalTokens, "contextTokens": usage.LastPromptTokens + usage.LastCompletionTokens, "latency": time.Since(startedAt).Milliseconds(), "rpm": llm.RequestsPerMinute(activeRateLimitKey), "rpmLimit": activeRPM, "stopped": true}})
			}
			return nil
		}
		errorID := dialog.NewMessageID()
		if appendErr := a.dialogs.Append(q.DialogID, dialog.Message{ID: errorID, Role: "assistant", Content: e.Error(), Model: engine.Model, Provider: engine.ProviderName, Error: true}); appendErr != nil {
			logx.Error("failed to persist chat error", "dialog_id", publicDialogID, "err", appendErr)
			errorID = ""
		}
		if a.publish != nil {
			a.publish(StreamMessage{Type: "status", Payload: map[string]string{"dialog_id": publicDialogID, "agent_id": q.AgentID, "status": "idle"}})
			a.publish(StreamMessage{Type: "error", Payload: map[string]string{"dialog_id": publicDialogID, "message": e.Error(), "message_id": errorID, "model": engine.Model, "provider": engine.ProviderName}})
		}
		return e
	}
	if a.publish != nil {
		if !streamed {
			a.publish(StreamMessage{Type: "chat_stream", Payload: map[string]string{"dialog_id": publicDialogID, "chunk": out}})
			a.publish(StreamMessage{Type: "chunk", Payload: map[string]string{"dialog_id": publicDialogID, "text": out}})
		}
		a.publish(StreamMessage{Type: "status", Payload: map[string]string{"dialog_id": publicDialogID, "agent_id": q.AgentID, "status": "idle"}})
		a.publish(StreamMessage{Type: "done", Payload: map[string]any{"dialog_id": publicDialogID, "content": out, "model": engine.Model, "provider": engine.ProviderName, "tokens": usage.TotalTokens, "contextTokens": usage.LastPromptTokens + usage.LastCompletionTokens, "latency": time.Since(startedAt).Milliseconds(), "rpm": llm.RequestsPerMinute(activeRateLimitKey), "rpmLimit": activeRPM}})
	}
	if encode != nil {
		return encode(map[string]string{"content": out})
	}
	return nil
}
func truncateTitle(value string) string {
	runes := []rune(value)
	if len(runes) > 48 {
		return string(runes[:48]) + "…"
	}
	return value
}
