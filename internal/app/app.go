/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"go.osspkg.com/goppy/v3/plugins/web"
	"go.osspkg.com/goppy/v3/plugins/ws"
	"go.osspkg.com/goppy/v3/plugins/ws/event"
	"go.osspkg.com/logx"

	"github.com/arwos/ai-agent/internal/pkg/agent"
	"github.com/arwos/ai-agent/internal/pkg/configstore"
	"github.com/arwos/ai-agent/internal/pkg/dialog"
	"github.com/arwos/ai-agent/internal/pkg/interaction"
	"github.com/arwos/ai-agent/internal/pkg/jobs"
	"github.com/arwos/ai-agent/internal/pkg/knowledge"
	"github.com/arwos/ai-agent/internal/pkg/llama"
	"github.com/arwos/ai-agent/internal/pkg/mcp"
	"github.com/arwos/ai-agent/internal/pkg/memorytools"
	"github.com/arwos/ai-agent/internal/pkg/models"
	"github.com/arwos/ai-agent/internal/pkg/ollama"
	"github.com/arwos/ai-agent/internal/pkg/proxy"
	"github.com/arwos/ai-agent/internal/pkg/skills"
	"github.com/arwos/ai-agent/internal/pkg/systeminfo"
	"github.com/arwos/ai-agent/internal/pkg/toolexecutor"
	"github.com/arwos/ai-agent/internal/pkg/updater"
	"github.com/arwos/ai-agent/internal/pkg/version"
	"github.com/arwos/ai-agent/internal/pkg/workspace"
)

type App struct {
	mu             sync.RWMutex
	settings       map[string]string
	store          *configstore.Store
	workspaces     *workspace.Registry
	picker         *workspace.Picker
	skills         *skills.Service
	dialogs        *dialog.Store
	memory         *dialog.MemoryStore
	dialogRegistry *dialog.Registry
	mcpManager     *mcp.Manager
	proxies        *proxy.Store
	knowledge      *knowledge.Store
	engines        *agent.Registry
	tools          *toolexecutor.Registry
	updater        *updater.Service
	ollama         *ollama.Service
	llama          *llama.Service
	systemInfo     *systeminfo.Service
	mcp            []mcp.Config
	publish        func(StreamMessage)
	route          web.Router
	wss            ws.Server
	pickMu         sync.RWMutex
	pickJobs       map[string]*pickJob
	requestMu      sync.Mutex
	requests       map[string]pendingRequest
	chatMu         sync.RWMutex
	chatJobs       map[string]*chatJob
	llmMu          sync.Mutex
	llmCancel      context.CancelFunc
	llmRootCtx     context.Context
	llmRuntimeStop map[string]context.CancelFunc
	backgroundJobs *jobs.Registry
}

// pendingRequest belongs to a dialog, not a browser connection. This lets a
// user reload the page while an approval is pending and still resume it.
type pendingRequest struct {
	dialogID string
	request  map[string]any
	answer   chan any
}

type pickJob struct {
	done    bool
	result  any
	errText string
}

// chatJob owns a request independently from the browser WebSocket. A page
// reload only replaces the subscriber; it must never cancel provider work.
type chatJob struct {
	cancel context.CancelFunc
	done   chan struct{}
}

func (a *App) requestUser(ctx context.Context, _ string, dialogID string, args map[string]any) (any, error) {
	reqID := streamMessageID()
	if requestedID, ok := args["reqId"].(string); ok && requestedID != "" {
		reqID = requestedID
	}
	ch := make(chan any, 1)
	request := map[string]any{"reqId": reqID, "kind": args["kind"], "title": args["title"], "detail": args["detail"], "question": args["question"], "placeholder": args["placeholder"], "command": args["command"], "options": args["options"]}
	a.requestMu.Lock()
	a.requests[reqID] = pendingRequest{dialogID: dialogID, request: request, answer: ch}
	a.requestMu.Unlock()
	logx.Info("interactive request created", "request_id", reqID, "dialog_id", dialogID, "kind", args["kind"])
	message := withStreamMessageID(StreamMessage{Type: "request", Payload: map[string]any{"dialog_id": dialogID, "request": request}})
	if err := a.Publish(message); err != nil {
		a.requestMu.Lock()
		delete(a.requests, reqID)
		a.requestMu.Unlock()
		return nil, err
	}
	select {
	case value := <-ch:
		logx.Info("interactive request answered", "request_id", reqID, "dialog_id", dialogID)
		return value, nil
	case <-ctx.Done():
		a.requestMu.Lock()
		delete(a.requests, reqID)
		a.requestMu.Unlock()
		logx.Error("interactive request cancelled", "request_id", reqID, "dialog_id", dialogID, "err", ctx.Err())
		return nil, ctx.Err()
	}
}

func NewApp(
	routes web.ServerPool,
	wss ws.Server,
	store *configstore.Store,
	workspaces *workspace.Registry,
	picker *workspace.Picker,
	skillsService *skills.Service,
	dialogs *dialog.Store,
	dialogRegistry *dialog.Registry,
	mcpManager *mcp.Manager,
	proxies *proxy.Store,
	knowledgeStore *knowledge.Store,
	engines *agent.Registry,
	tools *toolexecutor.Registry,
	updateService *updater.Service,
	ollamaService *ollama.Service,
	llamaService *llama.Service,
	systemInfoService *systeminfo.Service,
	backgroundJobs *jobs.Registry,
) (*App, error) {
	route, ok := routes.Main()
	if !ok {
		return nil, errors.New("http server with tag `main` not found")
	}

	a := &App{
		settings:       make(map[string]string, 10),
		store:          store,
		workspaces:     workspaces,
		picker:         picker,
		skills:         skillsService,
		dialogs:        dialogs,
		memory:         dialog.NewMemoryStore(dialogs.Root),
		dialogRegistry: dialogRegistry,
		mcpManager:     mcpManager,
		proxies:        proxies,
		knowledge:      knowledgeStore,
		engines:        engines,
		tools:          tools,
		updater:        updateService,
		ollama:         ollamaService,
		llama:          llamaService,
		systemInfo:     systemInfoService,
		backgroundJobs: backgroundJobs,
		route:          route,
		wss:            wss,
		pickJobs:       make(map[string]*pickJob),
		requests:       make(map[string]pendingRequest),
		chatJobs:       make(map[string]*chatJob),
	}
	go func() {
		if _, err := a.updater.Check(context.Background()); err != nil {
			logx.Debug("release check failed", "err", err)
		}
	}()
	if err := memorytools.RegisterTools(tools, a.memory); err != nil {
		return nil, err
	}
	if err := knowledge.RegisterTools(tools, knowledgeStore); err != nil {
		return nil, err
	}
	if backgroundJobs != nil {
		for _, runtime := range []string{"ollama", "llama"} {
			name := "local-llm-" + runtime
			if err := backgroundJobs.Register(name, jobs.Options{
				Mode:       jobs.Forever,
				RetryDelay: 15 * time.Second,
			}, func(ctx context.Context) error {
				if err := a.startConfiguredLocalLLM(ctx, runtime); err != nil {
					return err
				}
				<-ctx.Done()
				return nil
			}); err != nil {
				return nil, err
			}
		}
	}
	if err := interaction.RegisterTools(tools); err != nil {
		return nil, err
	}

	a.publish = func(m StreamMessage) {
		m = withStreamMessageID(m)
		if err := wss.BroadcastEvent(StreamEventID, m); err != nil {
			logx.Error("failed to broadcast event",
				"err", err,
				"StreamEventID", StreamEventID,
				"event", m,
			)
			return
		}
	}

	return a, nil
}

type chatWebSocketInput struct {
	Type           string   `json:"type"`
	ConvID         string   `json:"convId"`
	AgentID        string   `json:"agentId"`
	WorkspaceID    string   `json:"workspaceId"`
	Text           string   `json:"text"`
	Files          []string `json:"files"`
	SkillNames     []string `json:"skillNames"`
	ErrorMessageID string   `json:"errorMessageId"`
	Model          string   `json:"model"`
	AsGoal         bool     `json:"asGoal"`
	ReqID          string   `json:"reqId"`
	Value          any      `json:"value"`
}

func (a *App) handleWebSocketInput(ev event.Event, meta ws.Meta) error { //nolint:gocyclo // transport protocol dispatch
	var input chatWebSocketInput
	if err := ev.Decode(&input); err != nil {
		logx.Error("websocket request decode failed", "event_id", ev.ID(), "err", err)
		return err
	}
	if input.Type == "user.response" {
		if input.ReqID == "" {
			return errors.New("request id is required")
		}
		a.requestMu.Lock()
		response, found := a.requests[input.ReqID]
		if found && response.dialogID != input.ConvID {
			a.requestMu.Unlock()
			return errors.New("interactive request belongs to another conversation")
		}
		delete(a.requests, input.ReqID)
		a.requestMu.Unlock()
		if !found {
			// The in-memory request registry is rebuilt after a backend restart.
			// A goal approval is persisted, so accept it without requiring the
			// original goroutine to still exist.
			profileID, profileErr := a.activeProfile()
			if profileErr != nil {
				return profileErr
			}
			goal, goalErr := a.dialogs.Goal(dialog.SessionKey(profileID, input.ConvID))
			if goalErr != nil {
				return goalErr
			}
			if goal == nil || goal.Approval == nil || goal.Approval.ID != input.ReqID {
				return errors.New("interactive request is expired or unknown")
			}
			accepted, isBool := input.Value.(bool)
			if !isBool {
				return errors.New("approval response must be boolean")
			}
			goal.Approval = nil
			if accepted {
				goal.Status = "done"
			} else {
				goal.Status = "paused"
				for index := range goal.Tasks {
					if len(goal.Tasks[index].Tools) > 0 {
						goal.Tasks[index].Status = "pending"
					}
				}
			}
			if saveErr := a.dialogs.SaveGoal(dialog.SessionKey(profileID, input.ConvID), *goal); saveErr != nil {
				return saveErr
			}
			a.publish(StreamMessage{Type: "goal", Payload: map[string]any{"dialog_id": input.ConvID, "goal": goal}})
			return ev.Encode(map[string]any{"dialog_id": input.ConvID, "status": goal.Status, "restored": true})
		}
		logx.Info("interactive response received", "request_id", input.ReqID, "dialog_id", input.ConvID)
		response.answer <- input.Value
		return nil
	}
	if input.Type == "user.stop" {
		if input.ConvID == "" {
			return errors.New("conversation id is required")
		}
		a.chatMu.RLock()
		job := a.chatJobs[input.ConvID]
		a.chatMu.RUnlock()
		if job == nil {
			return errors.New("conversation has no running request")
		}
		job.cancel()
		return ev.Encode(map[string]any{"dialog_id": input.ConvID, "stopped": true})
	}
	if input.Type == "goal.pause" || input.Type == "goal.stop" {
		if input.ConvID == "" {
			return errors.New("conversation id is required")
		}
		profileID, err := a.activeProfile()
		if err != nil {
			return err
		}
		goal, err := a.dialogs.Goal(dialog.SessionKey(profileID, input.ConvID))
		if err != nil {
			return err
		}
		if goal == nil {
			// Historical goal cards can outlive the current goal.json. Treat
			// lifecycle commands as idempotent when the goal was finalized or
			// cleared already.
			return ev.Encode(map[string]any{"dialog_id": input.ConvID, "status": "none"})
		}
		if input.Type == "goal.pause" {
			goal.Status = "paused"
		} else {
			goal.Status = "stopped"
		}
		goal.Approval = nil
		if err := a.dialogs.SaveGoal(dialog.SessionKey(profileID, input.ConvID), *goal); err != nil {
			return err
		}
		a.publish(StreamMessage{Type: "goal", Payload: map[string]any{"dialog_id": input.ConvID, "goal": goal}})
		a.chatMu.RLock()
		job := a.chatJobs[input.ConvID]
		a.chatMu.RUnlock()
		if job != nil {
			job.cancel()
		}
		return ev.Encode(map[string]any{"dialog_id": input.ConvID, "status": goal.Status})
	}
	if input.Type != "user.message" && input.Type != "user.continue" {
		err := errors.New("unsupported websocket message type")
		logx.Error("websocket request rejected", "event_id", ev.ID(), "type", input.Type, "err", err)
		return err
	}
	payload := map[string]any{
		"dialog_id":    input.ConvID,
		"agentId":      input.AgentID,
		"workspace_id": input.WorkspaceID,
		"content":      input.Text,
		"files":        input.Files,
		"skills":       input.SkillNames,
		"model":        input.Model,
		"asGoal":       input.AsGoal,
	}
	if err := ev.Encode(payload); err != nil {
		logx.Error("websocket response encode failed", "event_id", ev.ID(), "err", err)
		return err
	}
	// A running chat belongs to a dialog rather than a WebSocket connection.
	// This keeps it alive across a browser reload while preventing concurrent
	// requests from corrupting one conversation's JSONL history.
	query := chatSendQuery{DialogID: input.ConvID, AgentID: input.AgentID, WorkspaceID: input.WorkspaceID, Content: input.Text, Model: input.Model, Skills: append([]string(nil), input.SkillNames...), Resume: input.Type == "user.continue", ErrorMessageID: input.ErrorMessageID, AsGoal: input.AsGoal}
	ctx, cancel := context.WithCancel(context.Background())
	job := &chatJob{cancel: cancel, done: make(chan struct{})}
	a.chatMu.Lock()
	if _, running := a.chatJobs[input.ConvID]; running {
		a.chatMu.Unlock()
		cancel()
		return errors.New("conversation already has a running request")
	}
	a.chatJobs[input.ConvID] = job
	a.chatMu.Unlock()
	connectionID, eventID := meta.ConnectID(), ev.ID()
	go func() {
		defer func() {
			a.chatMu.Lock()
			if a.chatJobs[input.ConvID] == job {
				delete(a.chatJobs, input.ConvID)
			}
			a.chatMu.Unlock()
			cancel()
			close(job.done)
		}()
		if err := a.runChatSend(query, ctx, connectionID, nil); err != nil {
			logx.Error("chat request failed", "event_id", eventID, "dialog_id", input.ConvID, "agent_id", input.AgentID, "err", err)
		}
	}()
	return nil
}

func (a *App) Up(ctx context.Context) error {
	logx.Info("Application main", "do", "start")
	if a.store != nil && a.workspaces != nil {
		if profile, profileErr := a.activeProfile(); profileErr == nil {
			if saved, err := a.store.Workspaces(profile); err == nil {
				for _, item := range saved {
					if _, err := a.workspaces.Open(item.ID, item.FolderPath); err != nil {
						logx.Error("failed to restore workspace", "workspace_id", item.ID, "path", item.FolderPath, "err", err)
					}
				}
			} else {
				logx.Error("failed to load saved workspaces", "profile_id", profile, "err", err)
			}
		} else {
			logx.Error("failed to resolve active profile", "err", profileErr)
		}
	}
	if a.backgroundJobs != nil {
		for _, name := range []string{"local-llm-ollama", "local-llm-llama"} {
			if err := a.backgroundJobs.Start(ctx, name); err != nil {
				logx.Error("failed to start local LLM job", "job", name, "err", err)
			}
		}
	}

	// Each application operation owns its event ID; no method-name dispatcher.
	a.wss.SetEventHandler(a.configGet, EventConfigGet)
	a.wss.SetEventHandler(a.configSet, EventConfigSet)
	a.wss.SetEventHandler(a.profileList, EventProfileList)
	a.wss.SetEventHandler(a.profileCreate, EventProfileCreate)
	a.wss.SetEventHandler(a.profileUpdate, EventProfileUpdate)
	a.wss.SetEventHandler(a.profileSetActive, EventProfileSetActive)
	a.wss.SetEventHandler(a.profileDelete, EventProfileDelete)
	a.wss.SetEventHandler(a.agentsList, EventAgentsList)
	a.wss.SetEventHandler(a.agentsUpsert, EventAgentsCreate, EventAgentsUpdate)
	a.wss.SetEventHandler(a.agentsDelete, EventAgentsDelete)
	a.wss.SetEventHandler(a.presetsList, EventPresetsList)
	a.wss.SetEventHandler(a.presetsUpsert, EventPresetsCreate, EventPresetsUpdate)
	a.wss.SetEventHandler(a.presetsDelete, EventPresetsDelete)
	a.wss.SetEventHandler(a.managedSkillsList, EventManagedSkillsList)
	a.wss.SetEventHandler(a.managedSkillsUpsert, EventManagedSkillsSet, EventSkillsCreate, EventSkillsUpdate)
	a.wss.SetEventHandler(a.managedSkillsDelete, EventManagedSkillsDelete, EventSkillsDelete)
	a.wss.SetEventHandler(a.skillsDiscover, EventSkillsDiscover)
	a.wss.SetEventHandler(a.skillsImportMany, EventSkillsImportMany)
	a.wss.SetEventHandler(a.skillGroupsList, EventSkillGroupsList)
	a.wss.SetEventHandler(a.skillGroupSave, EventSkillGroupSave)
	a.wss.SetEventHandler(a.skillGroupDelete, EventSkillGroupDelete)
	a.wss.SetEventHandler(a.skillGroupAssign, EventSkillGroupAssign)
	a.wss.SetEventHandler(a.skillsReindex, EventSkillsReindex)
	a.wss.SetEventHandler(a.skillsPickStart, EventSkillsPickStart)
	a.wss.SetEventHandler(a.skillsPickStatus, EventSkillsPickStatus)
	a.wss.SetEventHandler(a.skillsOpenFolder, EventSkillsOpenFolder)
	a.wss.SetEventHandler(a.filesystemSkillsList, EventSkillsFilesystemList)
	a.wss.SetEventHandler(a.kbList, EventKBList)
	a.wss.SetEventHandler(a.kbUpsert, EventKBCreate)
	a.wss.SetEventHandler(a.kbImportLink, EventKBImportLink)
	a.wss.SetEventHandler(a.kbScanFolder, EventKBScanFolder)
	a.wss.SetEventHandler(a.kbImportFiles, EventKBImportFiles)
	a.wss.SetEventHandler(a.kbDelete, EventKBDelete)
	a.wss.SetEventHandler(a.kbReindex, EventKBReindex)
	a.wss.SetEventHandler(a.conversationList, EventConversationList)
	a.wss.SetEventHandler(a.conversationGet, EventConversationGet)
	a.wss.SetEventHandler(a.conversationMemory, EventConversationMemory)
	a.wss.SetEventHandler(a.memoryNotesList, EventMemoryNotesList)
	a.wss.SetEventHandler(a.memoryNoteSave, EventMemoryNotesSave)
	a.wss.SetEventHandler(a.memoryNoteDelete, EventMemoryNotesDelete)
	a.wss.SetEventHandler(a.memoryTopicsList, EventMemoryTopicsList)
	a.wss.SetEventHandler(a.memoryTopicSave, EventMemoryTopicsSave)
	a.wss.SetEventHandler(a.memoryTopicDelete, EventMemoryTopicsDelete)
	a.wss.SetEventHandler(a.memoryReindex, EventMemoryReindex)
	a.wss.SetEventHandler(a.conversationUpsert, EventConversationCreate)
	a.wss.SetEventHandler(a.conversationAppend, EventConversationAppend)
	a.wss.SetEventHandler(a.conversationCompact, EventConversationCompact)
	a.wss.SetEventHandler(a.conversationDelete, EventConversationDelete)
	a.wss.SetEventHandler(a.conversationClear, EventConversationClear)
	a.wss.SetEventHandler(a.conversationDeleteMessage, EventConversationDeleteMessage)
	a.wss.SetEventHandler(a.conversationSetModel, EventConversationSetModel)
	a.wss.SetEventHandler(a.conversationRunStatus, EventConversationRunStatus)
	a.wss.SetEventHandler(a.workspacePick, EventWorkspacePick)
	a.wss.SetEventHandler(a.workspacePickStart, EventWorkspacePickStart)
	a.wss.SetEventHandler(a.workspacePickStatus, EventWorkspacePickStatus)
	a.wss.SetEventHandler(a.workspaceOpen, EventWorkspaceOpen)
	a.wss.SetEventHandler(a.workspaceCreate, EventWorkspaceCreate)
	a.wss.SetEventHandler(a.workspaceGet, EventWorkspaceGet)
	a.wss.SetEventHandler(a.workspaceClose, EventWorkspaceClose)
	a.wss.SetEventHandler(a.workspaceListOpen, EventWorkspaceListOpen)
	a.wss.SetEventHandler(a.workspaceList, EventWorkspaceList)
	a.wss.SetEventHandler(a.workspaceRead, EventWorkspaceRead)
	a.wss.SetEventHandler(a.workspaceWrite, EventWorkspaceWrite)
	a.wss.SetEventHandler(a.filesList, EventFilesList)
	a.wss.SetEventHandler(a.filesRead, EventFilesRead)
	a.wss.SetEventHandler(a.filesWrite, EventFilesWrite)
	a.wss.SetEventHandler(a.filesAdd, EventFilesAdd)
	a.wss.SetEventHandler(a.filesRemove, EventFilesRemove)
	a.wss.SetEventHandler(a.skillsList, EventSkillsList)
	a.wss.SetEventHandler(a.skillsGet, EventSkillsGet)
	a.wss.SetEventHandler(a.dialogHistory, EventDialogHistory)
	a.wss.SetEventHandler(a.chatSend, EventChatSend)
	a.wss.SetEventHandler(a.mcpList, EventMCPList)
	a.wss.SetEventHandler(a.profileMCPUpsert, EventMCPCreate, EventMCPUpdate)
	a.wss.SetEventHandler(a.mcpSet, EventMCPSet)
	a.wss.SetEventHandler(a.mcpDelete, EventMCPDelete)
	a.wss.SetEventHandler(a.mcpHealth, EventMCPHealth)
	a.wss.SetEventHandler(a.mcpTools, EventMCPTools, EventMCPFetchTools)
	a.wss.SetEventHandler(a.providersList, EventProvidersList)
	a.wss.SetEventHandler(a.profileProviderUpsert, EventProvidersCreate, EventProvidersUpdate)
	a.wss.SetEventHandler(a.providerModels, EventProvidersModels, EventProvidersFetchModels)
	a.wss.SetEventHandler(a.providerContextWindow, EventProvidersContextWindow)
	a.wss.SetEventHandler(a.providerCheckConnection, EventProvidersCheckConnection, EventProvidersTest)
	a.wss.SetEventHandler(a.providerSet, EventProvidersSet)
	a.wss.SetEventHandler(a.providerDelete, EventProvidersDelete)
	a.wss.SetEventHandler(a.proxiesList, EventProxiesList)
	a.wss.SetEventHandler(a.proxyUpsert, EventProxiesCreate, EventProxiesUpdate)
	a.wss.SetEventHandler(a.proxyDelete, EventProxiesDelete)
	a.wss.SetEventHandler(a.proxyResetPassword, EventProxiesResetPassword)
	a.wss.SetEventHandler(a.proxyTest, EventProxiesTest)
	a.wss.SetEventHandler(a.settingsExport, EventSettingsExport)
	a.wss.SetEventHandler(a.settingsImport, EventSettingsImport)
	a.wss.SetEventHandler(a.settingsCleanup, EventSettingsCleanup)
	a.wss.SetEventHandler(a.updateStatus, EventUpdateStatus)
	a.wss.SetEventHandler(a.updateApply, EventUpdateApply)
	a.wss.SetEventHandler(func(ev event.Event, _ ws.Meta) error { return ev.Encode(map[string]string{"version": version.Value}) }, EventVersion)
	a.wss.SetEventHandler(func(ev event.Event, meta ws.Meta) error { return ev.Encode(a.systemInfo.Collect(meta.Context())) }, EventSystemInfo)
	a.wss.SetEventHandler(a.ollamaInstall, EventOllamaInstall)
	a.wss.SetEventHandler(a.llamaInstall, EventLlamaInstall)
	a.wss.SetEventHandler(a.localLLMList, EventLocalLLMList)
	a.wss.SetEventHandler(a.localLLMUpsert, EventLocalLLMUpsert)
	a.wss.SetEventHandler(a.localLLMDelete, EventLocalLLMDelete)
	a.wss.SetEventHandler(a.ollamaStart, EventOllamaStart)
	a.wss.SetEventHandler(a.llamaStart, EventLlamaStart)
	a.wss.SetEventHandler(a.ollamaModelsRefresh, EventOllamaModelsRefresh)
	a.wss.SetEventHandler(a.ollamaModelsList, EventOllamaModelsList)
	a.wss.SetEventHandler(a.ollamaModelPull, EventOllamaModelPull)
	a.wss.SetEventHandler(a.ollamaModelRemove, EventOllamaModelRemove)
	a.wss.SetEventHandler(a.llamaModelsRefresh, EventLlamaModelsRefresh)
	a.wss.SetEventHandler(a.llamaModelsList, EventLlamaModelsList)
	a.wss.SetEventHandler(a.llamaModelPull, EventLlamaModelPull)
	a.wss.SetEventHandler(a.llamaModelRemove, EventLlamaModelRemove)
	a.wss.SetEventHandler(a.handleWebSocketInput, StreamInputEventID)

	// React/Vite SPA and its static assets. The not-found handler covers every
	// client-side route while ApiIndex keeps /api/* and missing static files 404.
	a.route.Get("#", a.ApiIndex)
	a.route.Get("/#", a.ApiIndex)
	a.route.Get("/api/ws", a.wss.Handling)
	a.route.NotFoundHandler(a.ApiIndex)

	return nil
}

func (a *App) Down() error {
	if a.backgroundJobs != nil {
		for _, name := range []string{"local-llm-ollama", "local-llm-llama"} {
			_ = a.backgroundJobs.Cancel(name)
		}
	}
	a.llmMu.Lock()
	if a.llmCancel != nil {
		a.llmCancel()
		a.llmCancel = nil
	}
	a.llmRuntimeStop = nil
	a.llmRootCtx = nil
	a.llmMu.Unlock()
	a.chatMu.RLock()
	jobs := make([]*chatJob, 0, len(a.chatJobs))
	for _, job := range a.chatJobs {
		jobs = append(jobs, job)
	}
	a.chatMu.RUnlock()
	for _, job := range jobs {
		job.cancel()
	}
	logx.Info("Application main", "do", "stop")
	return nil
}

func (a *App) startLocalLLMSupervisor(settings models.LocalLLMSettings) {
	a.llmMu.Lock()
	ctx, cancel := context.WithCancel(a.llmRootCtx)
	if previous := a.llmRuntimeStop[settings.Runtime]; previous != nil {
		previous()
	}
	a.llmRuntimeStop[settings.Runtime] = cancel
	a.llmMu.Unlock()
	go a.superviseLocalLLM(ctx, settings)
}

func (a *App) restartConfiguredLocalLLM(settings models.LocalLLMSettings) {
	a.llmMu.Lock()
	root := a.llmRootCtx
	if stop := a.llmRuntimeStop[settings.Runtime]; stop != nil {
		stop()
		delete(a.llmRuntimeStop, settings.Runtime)
	}
	a.llmMu.Unlock()
	if root == nil || !settings.Enabled || settings.BinaryPath == "" {
		return
	}
	if _, err := os.Stat(settings.BinaryPath); err != nil {
		logx.Error("local LLM restart skipped; binary is unavailable", "runtime", settings.Runtime, "path", settings.BinaryPath, "err", err)
		return
	}
	// llama.app needs a short amount of time to close its HTTP listener after
	// cancellation. Starting immediately races with socket cleanup and causes
	// intermittent "couldn't bind HTTP server socket" failures.
	go func() {
		timer := time.NewTimer(750 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-root.Done():
			return
		case <-timer.C:
			a.startLocalLLMSupervisor(settings)
		}
	}()
}

func (a *App) startConfiguredLocalLLM(parent context.Context, runtime string) error {
	profileID, err := a.activeProfile()
	if err != nil {
		return err
	}
	settings, err := a.store.LocalLLMList(profileID)
	if err != nil {
		return err
	}
	supervisorCtx, cancel := context.WithCancel(parent)
	a.llmMu.Lock()
	a.llmCancel = cancel
	a.llmRootCtx = supervisorCtx
	a.llmRuntimeStop = make(map[string]context.CancelFunc)
	a.llmMu.Unlock()
	for _, item := range settings {
		if item.Runtime != runtime {
			continue
		}
		if !item.Enabled {
			continue
		}
		if item.BinaryPath == "" {
			logx.Error("enabled local LLM has no binary path", "runtime", item.Runtime)
			continue
		}
		if _, err := os.Stat(item.BinaryPath); err != nil {
			logx.Error("local LLM binary is unavailable", "runtime", item.Runtime, "path", item.BinaryPath, "err", err)
			continue
		}
		a.startLocalLLMSupervisor(item)
	}
	return nil
}

func (a *App) superviseLocalLLM(ctx context.Context, settings models.LocalLLMSettings) {
	for {
		var process *os.Process
		var err error
		switch settings.Runtime {
		case "ollama":
			process, err = a.ollama.Start(ctx, settings)
		case "llama":
			process, err = a.llama.Start(ctx, settings)
		default:
			return
		}
		if err != nil {
			logx.Error("failed to start local LLM", "runtime", settings.Runtime, "path", settings.BinaryPath, "err", err)
		} else {
			logx.Info("local LLM started", "runtime", settings.Runtime, "pid", process.Pid, "path", settings.BinaryPath)
			if _, err := process.Wait(); err != nil {
				logx.Error("local LLM exited", "runtime", settings.Runtime, "err", err)
			} else {
				logx.Info("local LLM exited", "runtime", settings.Runtime)
			}
		}
		timer := time.NewTimer(15 * time.Second)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return
		case <-timer.C:
		}
		if _, err := os.Stat(filepath.Clean(settings.BinaryPath)); err != nil {
			logx.Error("local LLM restart skipped; binary is unavailable", "runtime", settings.Runtime, "path", settings.BinaryPath, "err", err)
			return
		}
	}
}
