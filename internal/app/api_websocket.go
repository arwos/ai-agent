/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package app

import (
	"crypto/rand"
	"fmt"
	"time"

	"go.osspkg.com/goppy/v3/plugins/ws/event"
)

const StreamEventID event.Id = 1
const StreamInputEventID event.Id = 2

// Browser request event IDs. These values are part of the frontend/backend
// protocol; append new IDs only, never reorder existing entries.
const (
	EventConfigGet event.Id = 10 + iota
	EventConfigSet
	EventProfileList
	EventProfileCreate
	EventProfileUpdate
	EventProfileSetActive
	EventProfileDelete
	EventAgentsList
	EventAgentsCreate
	EventAgentsUpdate
	EventAgentsDelete
	EventPresetsList
	EventPresetsCreate
	EventPresetsUpdate
	EventPresetsDelete
	EventManagedSkillsList
	EventManagedSkillsSet    // Deprecated: retained for protocol compatibility.
	EventManagedSkillsDelete // Deprecated: retained for protocol compatibility.
	EventSkillsCreate        // Deprecated: retained for protocol compatibility.
	EventSkillsUpdate        // Deprecated: retained for protocol compatibility.
	EventSkillsDelete        // Deprecated: retained for protocol compatibility.
	EventSkillsDiscover
	EventSkillsImportMany
	EventSkillsFilesystemList
	EventKBList
	EventKBCreate
	EventKBImportLink
	EventKBScanFolder
	EventKBImportFiles
	EventKBDelete
	EventKBReindex
	EventConversationList
	EventConversationGet
	EventConversationMemory
	EventMemoryNotesList
	EventMemoryNotesSave
	EventMemoryNotesDelete
	EventMemoryTopicsList
	EventMemoryTopicsSave
	EventMemoryTopicsDelete
	EventMemoryReindex
	EventConversationCreate
	EventConversationAppend
	EventConversationCompact
	EventConversationDelete
	EventConversationClear
	EventWorkspacePick
	EventWorkspacePickStart
	EventWorkspacePickStatus
	EventWorkspaceOpen
	EventWorkspaceCreate
	EventWorkspaceGet
	EventWorkspaceClose
	EventWorkspaceListOpen
	EventWorkspaceList
	EventWorkspaceRead
	EventWorkspaceWrite
	EventFilesList
	EventFilesRead
	EventFilesWrite
	EventFilesAdd
	EventFilesRemove
	EventSkillsList
	EventSkillsGet
	EventDialogHistory
	EventChatSend
	EventMCPList
	EventMCPCreate
	EventMCPUpdate
	EventMCPSet
	EventMCPDelete
	EventMCPHealth
	EventMCPTools
	EventMCPFetchTools
	EventProvidersList
	EventProvidersCreate
	EventProvidersUpdate
	EventProvidersModels
	EventProvidersContextWindow
	EventProvidersFetchModels
	EventProvidersCheckConnection
	EventProvidersTest
	EventProvidersSet
	EventProvidersDelete
	EventProxiesList
	EventProxiesCreate
	EventProxiesUpdate
	EventProxiesDelete
	EventProxiesResetPassword
	EventProxiesTest
	EventSettingsExport
	EventSettingsImport
	EventVersion
	EventConversationSetModel
	EventSkillsReindex
	EventSkillsPickStart
	EventSkillsPickStatus
	EventSkillsOpenFolder
	EventConversationRunStatus
	EventSettingsCleanup
	EventConversationDeleteMessage
	EventSkillGroupsList
	EventSkillGroupSave
	EventSkillGroupDelete
	EventSkillGroupAssign
	EventUpdateStatus
	EventUpdateApply
	EventSystemInfo
	EventOllamaInstall
	EventLlamaInstall
	EventLocalLLMList
	EventLocalLLMUpsert
	EventLocalLLMDelete
	EventOllamaStart
	EventLlamaStart
	EventOllamaModelsRefresh
	EventOllamaModelsList
	EventOllamaModelPull
	EventOllamaModelRemove
	EventLlamaModelsRefresh
	EventLlamaModelsList
	EventLlamaModelPull
	EventLlamaModelRemove
)

type StreamMessage struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Payload any    `json:"payload"`
}

func streamMessageID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%08x-0000-4000-8000-%012x", time.Now().UnixNano(), time.Now().UnixNano())
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func withStreamMessageID(message StreamMessage) StreamMessage {
	if message.ID == "" {
		message.ID = streamMessageID()
	}
	return message
}

func (a *App) Publish(message StreamMessage) error {
	return a.wss.BroadcastEvent(StreamEventID, withStreamMessageID(message))
}
