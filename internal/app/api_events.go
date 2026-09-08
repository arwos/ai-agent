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
	EventConfigGet                 event.Id = 10
	EventConfigSet                 event.Id = 11
	EventProfileList               event.Id = 12
	EventProfileCreate             event.Id = 13
	EventProfileUpdate             event.Id = 14
	EventProfileSetActive          event.Id = 15
	EventProfileDelete             event.Id = 16
	EventAgentsList                event.Id = 17
	EventAgentsCreate              event.Id = 18
	EventAgentsUpdate              event.Id = 19
	EventAgentsDelete              event.Id = 20
	EventPresetsList               event.Id = 21
	EventPresetsCreate             event.Id = 22
	EventPresetsUpdate             event.Id = 23
	EventPresetsDelete             event.Id = 24
	EventManagedSkillsList         event.Id = 25
	EventManagedSkillsSet          event.Id = 26 // Deprecated: retained for protocol compatibility.
	EventManagedSkillsDelete       event.Id = 27 // Deprecated: retained for protocol compatibility.
	EventSkillsCreate              event.Id = 28 // Deprecated: retained for protocol compatibility.
	EventSkillsUpdate              event.Id = 29 // Deprecated: retained for protocol compatibility.
	EventSkillsDelete              event.Id = 30 // Deprecated: retained for protocol compatibility.
	EventSkillsDiscover            event.Id = 31
	EventSkillsImportMany          event.Id = 32
	EventSkillsFilesystemList      event.Id = 33
	EventKBList                    event.Id = 34
	EventKBCreate                  event.Id = 35
	EventKBImportLink              event.Id = 36
	EventKBScanFolder              event.Id = 37
	EventKBImportFiles             event.Id = 38
	EventKBDelete                  event.Id = 39
	EventKBReindex                 event.Id = 40
	EventConversationList          event.Id = 41
	EventConversationGet           event.Id = 42
	EventConversationMemory        event.Id = 43
	EventMemoryNotesList           event.Id = 44
	EventMemoryNotesSave           event.Id = 45
	EventMemoryNotesDelete         event.Id = 46
	EventMemoryTopicsList          event.Id = 47
	EventMemoryTopicsSave          event.Id = 48
	EventMemoryTopicsDelete        event.Id = 49
	EventMemoryReindex             event.Id = 50
	EventConversationCreate        event.Id = 51
	EventConversationAppend        event.Id = 52
	EventConversationCompact       event.Id = 53
	EventConversationDelete        event.Id = 54
	EventConversationClear         event.Id = 55
	EventWorkspacePick             event.Id = 56
	EventWorkspacePickStart        event.Id = 57
	EventWorkspacePickStatus       event.Id = 58
	EventWorkspaceOpen             event.Id = 59
	EventWorkspaceCreate           event.Id = 60
	EventWorkspaceGet              event.Id = 61
	EventWorkspaceClose            event.Id = 62
	EventWorkspaceListOpen         event.Id = 63
	EventWorkspaceList             event.Id = 64
	EventWorkspaceRead             event.Id = 65
	EventWorkspaceWrite            event.Id = 66
	EventFilesList                 event.Id = 67
	EventFilesRead                 event.Id = 68
	EventFilesWrite                event.Id = 69
	EventFilesAdd                  event.Id = 70
	EventFilesRemove               event.Id = 71
	EventSkillsList                event.Id = 72
	EventSkillsGet                 event.Id = 73
	EventDialogHistory             event.Id = 74
	EventChatSend                  event.Id = 75
	EventMCPList                   event.Id = 76
	EventMCPCreate                 event.Id = 77
	EventMCPUpdate                 event.Id = 78
	EventMCPSet                    event.Id = 79
	EventMCPDelete                 event.Id = 80
	EventMCPHealth                 event.Id = 81
	EventMCPTools                  event.Id = 82
	EventMCPFetchTools             event.Id = 83
	EventProvidersList             event.Id = 84
	EventProvidersCreate           event.Id = 85
	EventProvidersUpdate           event.Id = 86
	EventProvidersModels           event.Id = 87
	EventProvidersContextWindow    event.Id = 88
	EventProvidersFetchModels      event.Id = 89
	EventProvidersCheckConnection  event.Id = 90
	EventProvidersTest             event.Id = 91
	EventProvidersSet              event.Id = 92
	EventProvidersDelete           event.Id = 93
	EventProxiesList               event.Id = 94
	EventProxiesCreate             event.Id = 95
	EventProxiesUpdate             event.Id = 96
	EventProxiesDelete             event.Id = 97
	EventProxiesResetPassword      event.Id = 98
	EventProxiesTest               event.Id = 99
	EventSettingsExport            event.Id = 100
	EventSettingsImport            event.Id = 101
	EventVersion                   event.Id = 102
	EventConversationSetModel      event.Id = 103
	EventSkillsReindex             event.Id = 104
	EventSkillsPickStart           event.Id = 105
	EventSkillsPickStatus          event.Id = 106
	EventSkillsOpenFolder          event.Id = 107
	EventConversationRunStatus     event.Id = 108
	EventSettingsCleanup           event.Id = 109
	EventConversationDeleteMessage event.Id = 110
	EventSkillGroupsList           event.Id = 111
	EventSkillGroupSave            event.Id = 112
	EventSkillGroupDelete          event.Id = 113
	EventSkillGroupAssign          event.Id = 114
	EventUpdateStatus              event.Id = 115
	EventUpdateApply               event.Id = 116
	EventSystemInfo                event.Id = 117
	EventOllamaInstall             event.Id = 118
	EventLlamaInstall              event.Id = 119
	EventLocalLLMList              event.Id = 120
	EventLocalLLMUpsert            event.Id = 121
	EventLocalLLMDelete            event.Id = 122
	EventOllamaStart               event.Id = 123
	EventLlamaStart                event.Id = 124
	EventOllamaModelsRefresh       event.Id = 125
	EventOllamaModelsList          event.Id = 126
	EventOllamaModelPull           event.Id = 127
	EventOllamaModelRemove         event.Id = 128
	EventLlamaModelsRefresh        event.Id = 129
	EventLlamaModelsList           event.Id = 130
	EventLlamaModelPull            event.Id = 131
	EventLlamaModelRemove          event.Id = 132
	EventWorkflowList              event.Id = 133
	EventWorkflowGet               event.Id = 134
	EventWorkflowSave              event.Id = 135
	EventWorkflowDelete            event.Id = 136
	EventWorkflowImport            event.Id = 137
	EventWorkflowExport            event.Id = 138
	EventWorkflowPause             event.Id = 139
	EventWorkflowContinue          event.Id = 140
	EventWorkflowStop              event.Id = 141
)
