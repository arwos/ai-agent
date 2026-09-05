/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package app

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"go.osspkg.com/goppy/v3/plugins/ws"
	"go.osspkg.com/goppy/v3/plugins/ws/event"
	"go.osspkg.com/logx"

	"github.com/arwos/ai-agent/internal/pkg/configstore"
	"github.com/arwos/ai-agent/internal/pkg/dialog"
	"github.com/arwos/ai-agent/internal/pkg/llm"
	"github.com/arwos/ai-agent/internal/pkg/models"
	"github.com/arwos/ai-agent/internal/pkg/prompts"
)

func (a *App) profileList(ev event.Event, _ ws.Meta) error {
	if a.store == nil {
		return ev.Encode(map[string]any{"profiles": []any{}, "activeId": "default"})
	}
	profiles, active, err := a.store.Profiles()
	if err != nil {
		return err
	}
	return ev.Encode(map[string]any{"profiles": profiles, "activeId": active})
}
func (a *App) conversationList(ev event.Event, _ ws.Meta) error {
	var q struct {
		WorkspaceID string `json:"workspaceId"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	if q.WorkspaceID == "" {
		q.WorkspaceID = "default"
	}
	profile, err := a.activeProfile()
	if err != nil {
		return err
	}
	items, err := a.store.Conversations(profile, q.WorkspaceID)
	if err != nil {
		return err
	}
	return ev.Encode(items)
}
func (a *App) conversationGet(ev event.Event, _ ws.Meta) error {
	var q struct {
		ID string `json:"id"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	if q.ID == "" {
		return fmt.Errorf("id is required")
	}
	profile, err := a.activeProfile()
	if err != nil {
		return err
	}
	item, err := a.store.Conversation(profile, q.ID)
	if err != nil {
		return err
	}
	history, err := a.dialogs.History(dialog.SessionKey(profile, item.ID), 0)
	if err != nil {
		return err
	}
	memory, err := a.memory.Get(profile, item.WorkspaceID, item.ID)
	if err != nil {
		return err
	}
	goal, err := a.dialogs.Goal(dialog.SessionKey(profile, item.ID))
	if err != nil {
		return err
	}
	goals, err := a.dialogs.Goals(dialog.SessionKey(profile, item.ID))
	if err != nil {
		return err
	}
	resolvedErrors := make(map[string]bool)
	for _, message := range history {
		if message.Role == "error_resolution" && message.Resolves != "" {
			resolvedErrors[message.Resolves] = true
		}
	}
	messages := make([]map[string]any, 0, len(history))
	for index, message := range history {
		if message.Role == "error_resolution" || (message.Error && resolvedErrors[message.ID]) {
			continue
		}
		role := "agent"
		if message.Role == "user" {
			role = "user"
		}
		if message.Role == "tool_call" || message.Role == "tool_result" {
			toolResult := message.Role == "tool_result"
			messages = append(messages, map[string]any{"id": message.ID, "role": "tool", "time": message.Timestamp.Format(time.RFC3339), "toolName": message.Tool, "toolArguments": message.Arguments, "toolResult": message.Content, "toolRunning": !toolResult, "model": message.Model, "provider": message.Provider, "parts": []map[string]string{}})
			continue
		}
		messageID := message.ID
		if messageID == "" { // compatibility for JSONL files created before IDs
			messageID = fmt.Sprintf("%s-%d", item.ID, index)
		}
		messages = append(messages, map[string]any{"id": messageID, "role": role, "time": message.Timestamp.Format(time.RFC3339), "compact": message.Compact, "contextSize": message.ContextSize, "tokens": message.Tokens, "model": message.Model, "provider": message.Provider, "retryableError": message.Error, "parts": []map[string]string{{"type": "text", "content": message.Content}}})
	}
	return ev.Encode(map[string]any{"id": item.ID, "workspaceId": item.WorkspaceID, "agentId": item.AgentID, "title": item.Title, "updatedAt": item.UpdatedAt, "activeModel": item.ActiveModel, "messages": messages, "memory": memory, "goal": goal, "goals": goals})
}
func (a *App) conversationMemory(ev event.Event, meta ws.Meta) error {
	var q struct {
		ID, WorkspaceID string
		Title           string
		Summary         string
		Topics          []string
		Save            bool
		Model           string
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	if q.ID == "" {
		return fmt.Errorf("id is required")
	}
	profile, err := a.activeProfile()
	if err != nil {
		return err
	}
	item, err := a.store.Conversation(profile, q.ID)
	if err != nil {
		return err
	}
	if q.WorkspaceID == "" {
		q.WorkspaceID = item.WorkspaceID
	}
	if q.Save {
		if err := a.memory.Save(profile, q.WorkspaceID, q.ID, dialog.Memory{Title: q.Title, Summary: q.Summary, Topics: q.Topics}); err != nil {
			return err
		}
		memory, err := a.memory.Get(profile, q.WorkspaceID, q.ID)
		if err != nil {
			return err
		}
		return ev.Encode(memory)
	}
	if q.Model == "" {
		for _, agent := range mustAgents(a.store, profile) {
			if agent.ID == item.AgentID {
				q.Model = agent.MemoryModel
				if q.Model == "" {
					q.Model = item.ActiveModel
					if q.Model == "" && len(agent.MainModels) > 0 {
						q.Model = agent.MainModels[0]
					}
				}
				break
			}
		}
	}
	if q.Model == "" {
		memory, err := a.memory.Get(profile, q.WorkspaceID, q.ID)
		if err != nil {
			return err
		}
		return ev.Encode(memory)
	}
	history, err := a.dialogs.History(dialog.SessionKey(profile, item.ID), 0)
	if err != nil {
		return err
	}
	messages := make([]llm.Message, 0, len(history)+1)
	for _, m := range history {
		if m.Role == "user" || m.Role == "assistant" {
			messages = append(messages, llm.Message{Role: m.Role, Content: m.Content})
		}
	}
	messages = append(messages, llm.Message{Role: "user", Content: prompts.MemoryRequest})
	providerConfig, apiKey, err := a.store.DefaultProfileProvider(profile)
	if at := strings.LastIndex(q.Model, "@"); at > 0 {
		providerConfig, apiKey, err = a.store.ProfileProviderSecret(profile, q.Model[at+1:])
		q.Model = q.Model[:at]
	}
	if err != nil {
		return err
	}
	response, err := llm.New(providerConfig.Kind, providerConfig.BaseURL, apiKey, q.Model, a.providerProxy(profile, providerConfig.ProxyID), llm.WithRPM(providerConfig.ID, providerConfig.RPM)).ChatCompletion(context.Background(), prompts.MemorySystem, messages, false)
	if err != nil {
		return err
	}
	generated := parseGeneratedMemory(response.Content)
	memory := generated.Memory
	if err := a.memory.Save(profile, q.WorkspaceID, q.ID, memory); err != nil {
		return err
	}
	if err := a.saveExtractedMemories(profile, q.WorkspaceID, generated); err != nil {
		return err
	}
	return ev.Encode(memory)
}
func (a *App) memoryNotesList(ev event.Event, _ ws.Meta) error {
	var q struct {
		WorkspaceID string `json:"workspaceId"`
		Cursor      string `json:"cursor"`
		Limit       int    `json:"limit"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	profile, err := a.activeProfile()
	if err != nil {
		return err
	}
	page, err := a.memory.NotesPage(profile, q.Cursor, q.Limit)
	if err != nil {
		return err
	}
	return ev.Encode(page)
}
func (a *App) memoryNoteSave(ev event.Event, _ ws.Meta) error {
	var q struct {
		WorkspaceID string `json:"workspaceId"`
		dialog.LongTermNote
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	profile, err := a.activeProfile()
	if err != nil {
		return err
	}
	if err = a.memory.SaveNote(profile, q.WorkspaceID, q.LongTermNote); err != nil {
		return err
	}
	return ev.Encode(q.LongTermNote)
}
func (a *App) memoryNoteDelete(ev event.Event, _ ws.Meta) error {
	var q struct {
		WorkspaceID string `json:"workspaceId"`
		ID          string `json:"id"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	profile, err := a.activeProfile()
	if err != nil {
		return err
	}
	if err := a.memory.DeleteNote(profile, q.WorkspaceID, q.ID); err != nil {
		return err
	}
	return ev.Encode(map[string]bool{"ok": true})
}
func (a *App) memoryTopicsList(ev event.Event, _ ws.Meta) error {
	var q struct {
		WorkspaceID string `json:"workspaceId"`
		Cursor      string `json:"cursor"`
		Limit       int    `json:"limit"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	profile, err := a.activeProfile()
	if err != nil {
		return err
	}
	page, err := a.memory.TopicsPage(profile, q.Cursor, q.Limit)
	if err != nil {
		return err
	}
	return ev.Encode(page)
}
func (a *App) memoryTopicSave(ev event.Event, _ ws.Meta) error {
	var q struct {
		WorkspaceID string `json:"workspaceId"`
		dialog.TopicMemory
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	profile, err := a.activeProfile()
	if err != nil {
		return err
	}
	if err = a.memory.SaveTopic(profile, q.WorkspaceID, q.TopicMemory); err != nil {
		return err
	}
	return ev.Encode(q.TopicMemory)
}
func (a *App) memoryTopicDelete(ev event.Event, _ ws.Meta) error {
	var q struct {
		WorkspaceID string `json:"workspaceId"`
		ID          string `json:"id"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	profile, err := a.activeProfile()
	if err != nil {
		return err
	}
	if err := a.memory.DeleteTopic(profile, q.WorkspaceID, q.ID); err != nil {
		return err
	}
	return ev.Encode(map[string]bool{"ok": true})
}
func (a *App) memoryReindex(ev event.Event, _ ws.Meta) error {
	var q struct {
		ProfileID string `json:"profileId"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	if q.ProfileID == "" {
		profile, err := a.activeProfile()
		if err != nil {
			return err
		}
		q.ProfileID = profile
	}
	notes, topics, err := a.memory.Reindex(q.ProfileID)
	if err != nil {
		return err
	}
	return ev.Encode(map[string]int{"notes": notes, "topics": topics})
}
func mustAgents(store *configstore.Store, profile string) []models.Agent {
	agents, _ := store.Agents(profile)
	return agents
}

type generatedMemory struct {
	dialog.Memory
	Notes         []dialog.LongTermNote `json:"notes"`
	TopicMemories []dialog.TopicMemory  `json:"topicMemories"`
}

func parseGeneratedMemory(content string) generatedMemory {
	clean := strings.TrimSpace(content)
	clean = strings.TrimPrefix(clean, "```json")
	clean = strings.TrimPrefix(clean, "```")
	clean = strings.TrimSuffix(strings.TrimSpace(clean), "```")
	// Decode every optional field separately. A provider may produce a valid
	// summary but violate one optional array schema (for example objects in
	// `topics`). That must not turn the entire JSON response into raw text in
	// the stored session memory.
	var raw struct {
		Title         string          `json:"title"`
		Summary       string          `json:"summary"`
		Topics        json.RawMessage `json:"topics"`
		Notes         json.RawMessage `json:"notes"`
		TopicMemories json.RawMessage `json:"topicMemories"`
	}
	if err := json.Unmarshal([]byte(clean), &raw); err == nil && strings.TrimSpace(raw.Summary) != "" {
		memory := generatedMemory{Memory: dialog.Memory{Title: raw.Title, Summary: raw.Summary, Topics: []string{}}, Notes: []dialog.LongTermNote{}, TopicMemories: []dialog.TopicMemory{}}
		if memory.Title == "" {
			memory.Title = memory.Summary
		}
		var topics []string
		if json.Unmarshal(raw.Topics, &topics) == nil {
			memory.Topics = topics
		}
		var notes []dialog.LongTermNote
		if json.Unmarshal(raw.Notes, &notes) == nil {
			memory.Notes = notes
		}
		var topicMemories []dialog.TopicMemory
		if json.Unmarshal(raw.TopicMemories, &topicMemories) == nil {
			memory.TopicMemories = topicMemories
		}
		return memory
	}
	firstLine := strings.TrimSpace(strings.SplitN(content, "\n", 2)[0])
	return generatedMemory{Memory: dialog.Memory{Title: firstLine, Summary: content, Topics: []string{}}, Notes: []dialog.LongTermNote{}, TopicMemories: []dialog.TopicMemory{}}
}
func extractedMemoryID(prefix, title string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(title))))
	return fmt.Sprintf("%s-%x", prefix, sum[:8])
}
func (a *App) conversationAppend(ev event.Event, _ ws.Meta) error {
	var q struct {
		ConvID  string `json:"convId"`
		Message struct {
			Role  string `json:"role"`
			Parts []struct {
				Content string `json:"content"`
			} `json:"parts"`
		} `json:"message"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	if q.ConvID == "" || len(q.Message.Parts) == 0 {
		return fmt.Errorf("convId and message are required")
	}
	profile, err := a.activeProfile()
	if err != nil {
		return err
	}
	item, err := a.store.Conversation(profile, q.ConvID)
	if err != nil {
		return err
	}
	content := q.Message.Parts[0].Content
	if err = a.dialogs.Append(dialog.SessionKey(profile, item.ID), dialog.Message{Role: q.Message.Role, Content: content}); err != nil {
		return err
	}
	if q.Message.Role == "user" && item.Title == "New conversation" {
		item.Title = truncateTitle(content)
	}
	item, err = a.store.ConversationUpsert(item)
	if err != nil {
		return err
	}
	return ev.Encode(map[string]string{"title": item.Title, "updatedAt": item.UpdatedAt})
}
func (a *App) conversationCompact(ev event.Event, meta ws.Meta) error {
	var q struct {
		ConvID string `json:"convId"`
		Model  string `json:"model"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	profile, err := a.activeProfile()
	if err != nil {
		return err
	}
	item, err := a.store.Conversation(profile, q.ConvID)
	if err != nil {
		return err
	}
	history, err := a.dialogs.History(dialog.SessionKey(profile, item.ID), 0)
	if err != nil {
		return err
	}
	var conversationAgent models.Agent
	for _, candidate := range mustAgents(a.store, profile) {
		if candidate.ID == item.AgentID {
			conversationAgent = candidate
			break
		}
	}
	if q.Model == "" {
		if conversationAgent.ID != "" {
			candidate := conversationAgent
			if candidate.ID == item.AgentID {
				q.Model = item.ActiveModel
				if q.Model == "" && len(candidate.MainModels) > 0 {
					q.Model = candidate.MainModels[0]
				}
			}
		}
		if q.Model == "" {
			return fmt.Errorf("compaction model is required")
		}
	}
	providerConfig, apiKey, err := a.store.DefaultProfileProvider(profile)
	if at := strings.LastIndex(q.Model, "@"); at > 0 && at < len(q.Model)-1 {
		providerConfig, apiKey, err = a.store.ProfileProviderSecret(profile, q.Model[at+1:])
		q.Model = q.Model[:at]
	}
	if err != nil {
		return fmt.Errorf("compaction provider is not configured: %w", err)
	}
	messages := make([]llm.Message, 0, len(history)+1)
	for _, message := range history {
		role := message.Role
		if role == "system" && message.Compact {
			// A prior compaction is the durable boundary context. It must be
			// available when the dialog is compacted again, otherwise each new
			// compaction silently loses everything before the last boundary.
			messages = append(messages, llm.Message{Role: role, Content: message.Content})
			continue
		}
		if role != "user" && role != "assistant" && role != "tool" {
			continue
		}
		messages = append(messages, llm.Message{Role: role, Content: message.Content})
	}
	messages = append(messages, llm.Message{Role: "user", Content: prompts.CompactionRequestFor(conversationAgent.CompactionLevel)})
	var provider llm.Provider = llm.New(providerConfig.Kind, providerConfig.BaseURL, apiKey, q.Model, a.providerProxy(profile, providerConfig.ProxyID), llm.WithRPM(providerConfig.ID, providerConfig.RPM))
	response, err := provider.ChatCompletion(meta.Context(), prompts.CompactionSystemFor(conversationAgent.CompactionLevel), messages, false)
	if err != nil {
		return err
	}
	if response.Content == "" {
		return fmt.Errorf("compaction provider returned an empty summary")
	}
	generated := parseGeneratedMemory(response.Content)
	memory := generated.Memory
	if err = a.dialogs.Append(dialog.SessionKey(profile, item.ID), dialog.Message{Role: "system", Content: memory.Summary, Model: q.Model, Provider: providerConfig.Name, ContextSize: response.Usage.CompletionTokens, Tokens: response.Usage.PromptTokens + response.Usage.CompletionTokens, Compact: true}); err != nil {
		return err
	}
	before := 0
	for _, message := range history {
		before += message.ContextSize
	}
	tokens := response.Usage.TotalTokens
	if tokens == 0 {
		tokens = response.Usage.PromptTokens + response.Usage.CompletionTokens
	}
	if err := a.memory.Save(profile, item.WorkspaceID, item.ID, memory); err != nil {
		return err
	}
	if err := a.saveExtractedMemories(profile, item.WorkspaceID, generated); err != nil {
		return err
	}
	return ev.Encode(map[string]any{"before": before, "after": response.Usage.CompletionTokens, "title": memory.Title, "summary": memory.Summary, "topics": memory.Topics, "tokens": tokens})
}
func (a *App) conversationUpsert(ev event.Event, _ ws.Meta) error {
	var q models.Conversation
	if err := ev.Decode(&q); err != nil {
		return err
	}
	if q.ProfileID == "" {
		id, err := a.activeProfile()
		if err != nil {
			return err
		}
		q.ProfileID = id
	}
	if q.WorkspaceID == "" {
		q.WorkspaceID = "default"
	}
	if q.ID == "" {
		q.ID = entityID("conversation")
	}
	conversation, err := a.store.ConversationUpsert(q)
	if err != nil {
		return err
	}
	return ev.Encode(conversation)
}
func (a *App) conversationSetModel(ev event.Event, _ ws.Meta) error {
	var q struct {
		ID    string `json:"id"`
		Mode  string `json:"mode"`
		Model string `json:"model"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	if q.ID == "" || (q.Mode != "main" && q.Mode != "model" && q.Mode != "fallback") {
		return fmt.Errorf("id and model mode are required")
	}
	profileID, err := a.activeProfile()
	if err != nil {
		return err
	}
	conversation, err := a.store.Conversation(profileID, q.ID)
	if err != nil {
		return err
	}
	// Deprecated: the fallback mode is retained only to return a clear
	// compatibility error to clients of older frontend versions.
	if q.Mode == "fallback" {
		return fmt.Errorf("fallback models are no longer supported")
	}
	if q.Model != "" {
		for _, candidate := range mustAgents(a.store, profileID) {
			if candidate.ID == conversation.AgentID && !containsString(candidate.MainModels, q.Model) {
				return fmt.Errorf("model %q is not configured as an agent main model", q.Model)
			}
		}
		conversation.ActiveModel = q.Model
	} else {
		conversation.ActiveModel = ""
	}
	if err := a.store.ConversationSetActiveModel(profileID, q.ID, conversation.ActiveModel); err != nil {
		return err
	}
	return ev.Encode(conversation)
}
func (a *App) conversationRunStatus(ev event.Event, _ ws.Meta) error {
	var q struct {
		ID string `json:"id"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	if q.ID == "" {
		return fmt.Errorf("conversation id is required")
	}
	a.chatMu.RLock()
	_, running := a.chatJobs[q.ID]
	a.chatMu.RUnlock()
	a.requestMu.Lock()
	var request map[string]any
	for _, pending := range a.requests {
		if pending.dialogID == q.ID {
			request = pending.request
			break
		}
	}
	a.requestMu.Unlock()
	return ev.Encode(map[string]any{"running": running, "request": request})
}
func (a *App) conversationDelete(ev event.Event, _ ws.Meta) error {
	var q struct {
		ID string `json:"id"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	profile, err := a.activeProfile()
	if err != nil {
		return err
	}
	conversation, err := a.store.Conversation(profile, q.ID)
	if err != nil {
		return err
	}
	if err := a.dialogs.Delete(dialog.SessionKey(profile, conversation.ID)); err != nil {
		return err
	}
	if err := a.memory.Delete(profile, conversation.WorkspaceID, conversation.ID); err != nil {
		return err
	}
	if err := a.dialogs.DeleteGoals(dialog.SessionKey(profile, conversation.ID)); err != nil {
		return err
	}
	if err := a.store.ConversationDelete(profile, q.ID); err != nil {
		return err
	}
	return ev.Encode(map[string]bool{"ok": true})
}
func (a *App) conversationClear(ev event.Event, _ ws.Meta) error {
	var q struct {
		ID string `json:"id"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	if q.ID == "" {
		return fmt.Errorf("id is required")
	}
	profile, err := a.activeProfile()
	if err != nil {
		return err
	}
	conversation, err := a.store.Conversation(profile, q.ID)
	if err != nil {
		return err
	}
	if err := a.dialogs.Delete(dialog.SessionKey(profile, conversation.ID)); err != nil {
		return err
	}
	if err := a.memory.Delete(profile, conversation.WorkspaceID, conversation.ID); err != nil {
		return err
	}
	if err := a.dialogs.DeleteGoals(dialog.SessionKey(profile, conversation.ID)); err != nil {
		return err
	}
	return ev.Encode(map[string]bool{"ok": true})
}
func (a *App) conversationDeleteMessage(ev event.Event, _ ws.Meta) error {
	var q struct {
		ID        string `json:"id"`
		MessageID string `json:"messageId"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	if q.ID == "" || q.MessageID == "" {
		return fmt.Errorf("conversation id and message id are required")
	}
	profile, err := a.activeProfile()
	if err != nil {
		return err
	}
	conversation, err := a.store.Conversation(profile, q.ID)
	if err != nil {
		return err
	}
	if err := a.dialogs.DeleteMessage(dialog.SessionKey(profile, conversation.ID), q.MessageID); err != nil {
		return err
	}
	return ev.Encode(map[string]bool{"ok": true})
}
func (a *App) profileCreate(ev event.Event, _ ws.Meta) error {
	var q models.Profile
	if err := ev.Decode(&q); err != nil {
		return err
	}
	if q.ID == "" {
		q.ID = entityID("profile")
	}
	created, err := a.store.ProfileCreate(q)
	if err != nil {
		return err
	}
	if err := a.store.ProfileSetActive(created.ID); err != nil {
		return err
	}
	return ev.Encode(created)
}
func (a *App) profileUpdate(ev event.Event, _ ws.Meta) error {
	var q struct {
		ID    string         `json:"id"`
		Patch models.Profile `json:"patch"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	q.Patch.ID = q.ID
	if err := a.store.ProfileUpdate(q.Patch); err != nil {
		return err
	}
	return ev.Encode(q.Patch)
}
func (a *App) profileSetActive(ev event.Event, _ ws.Meta) error {
	var q struct {
		ID string `json:"id"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	if err := a.store.ProfileSetActive(q.ID); err != nil {
		return err
	}
	return ev.Encode(map[string]bool{"ok": true})
}
func (a *App) profileDelete(ev event.Event, _ ws.Meta) error {
	var q struct {
		ID string `json:"id"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	profiles, _, err := a.store.Profiles()
	if err != nil {
		return err
	}
	if len(profiles) <= 1 {
		return fmt.Errorf("cannot delete last profile")
	}
	if q.ID == "" {
		return fmt.Errorf("profile id is required")
	}
	found := false
	for _, profile := range profiles {
		if profile.ID == q.ID {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("profile %q not found", q.ID)
	}
	conversations, err := a.store.ProfileConversations(q.ID)
	if err != nil {
		return err
	}
	workspaces, err := a.store.Workspaces(q.ID)
	if err != nil {
		return err
	}
	a.stopProfileChats(conversations)
	// Workspaces only register folders opened by the user. Close their roots but
	// never remove the user-owned folders themselves.
	if a.workspaces != nil {
		for _, item := range workspaces {
			if closeErr := a.workspaces.Close(item.ID); closeErr != nil {
				logx.Error("failed to close profile workspace", "profile_id", q.ID, "workspace_id", item.ID, "err", closeErr)
			}
		}
	}
	if a.dialogs != nil {
		if err := a.dialogs.DeleteProfile(q.ID); err != nil {
			return fmt.Errorf("delete profile dialog history: %w", err)
		}
	}
	if a.memory != nil {
		if err := a.memory.DeleteProfile(q.ID); err != nil {
			return fmt.Errorf("delete profile memory: %w", err)
		}
	}
	if a.knowledge != nil {
		if err := a.knowledge.DeleteProfile(q.ID); err != nil {
			return fmt.Errorf("delete profile knowledge: %w", err)
		}
	}
	if err := a.skills.DeleteProfile(q.ID); err != nil {
		return fmt.Errorf("delete profile skills: %w", err)
	}
	if err := a.store.ProfileDelete(q.ID); err != nil {
		return err
	}
	return ev.Encode(map[string]bool{"ok": true})
}
func (a *App) stopProfileChats(conversations []models.Conversation) {
	ids := make(map[string]struct{}, len(conversations))
	for _, conversation := range conversations {
		ids[conversation.ID] = struct{}{}
	}
	a.chatMu.RLock()
	jobs := make([]*chatJob, 0, len(ids))
	for id := range ids {
		if job := a.chatJobs[id]; job != nil {
			jobs = append(jobs, job)
		}
	}
	a.chatMu.RUnlock()
	for _, job := range jobs {
		job.cancel()
	}
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for _, job := range jobs {
		select {
		case <-job.done:
		case <-deadline.C:
			logx.Error("timed out waiting for profile chat to stop")
			return
		}
	}
}
func (a *App) activeProfile() (string, error) {
	if a.store == nil {
		return "", fmt.Errorf("configuration store unavailable")
	}
	_, id, err := a.store.Profiles()
	return id, err
}
func profileImportID(id, profileID string) string {
	if id == "" || profileID == "" {
		return id
	}
	if match := profileScopedID.FindStringSubmatch(id); len(match) == 4 {
		return match[1] + "-" + profileID + "-" + match[3]
	}
	if match := entityIDPattern.FindStringSubmatch(id); len(match) == 3 {
		return match[1] + "-" + profileID + "-" + match[2]
	}
	return id
}
func (a *App) saveExtractedMemories(profileID, workspaceID string, generated generatedMemory) error {
	for index, note := range generated.Notes {
		if index >= 3 || strings.TrimSpace(note.Title) == "" || strings.TrimSpace(note.Content) == "" {
			continue
		}
		note.ID = extractedMemoryID("note", note.Title)
		if err := a.memory.SaveNote(profileID, "", note); err != nil {
			return err
		}
	}
	for index, topic := range generated.TopicMemories {
		if index >= 3 || strings.TrimSpace(topic.Title) == "" || strings.TrimSpace(topic.Summary) == "" {
			continue
		}
		topic.ID = extractedMemoryID("topic", topic.Title)
		if err := a.memory.SaveTopic(profileID, workspaceID, topic); err != nil {
			return err
		}
	}
	return nil
}
