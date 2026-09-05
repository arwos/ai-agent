/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package app

import "go.osspkg.com/goppy/v3/plugins/ws/event"

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
	EventWorkflowList
	EventWorkflowGet
	EventWorkflowSave
	EventWorkflowDelete
	EventWorkflowImport
	EventWorkflowExport
)
