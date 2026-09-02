# WebSocket API

The browser API is available only through WebSocket at `/api/ws`. There is no REST or HTTP JSON-RPC API for application screens.

The protocol uses stable numeric event IDs from `internal/app/api_websocket.go`. Send a request as `{"e": <event-id>, "d": <parameters>}`. A successful RPC response carries the handler result in `d`; a failed request carries `err`. Event IDs are append-only: clients must use the IDs from the matching application version and must not infer an ID from a method name.

```json
{"e": 42, "d": {"id": "conversation-1"}}
```

Secrets are write-only: provider API keys, proxy passwords, and MCP headers are never returned by list/get responses. Public models use `hasApiKey` and `hasPassword` flags instead.

## Events

| IDs | Area | Operations |
| --- | --- | --- |
| 10–11 | Configuration | `config.get`, `config.set` |
| 12–16 | Profiles | `list`, `create`, `update`, `setActive`, `delete` |
| 17–20 | Agents | `list`, `create`, `update`, `delete` |
| 21–24 | Presets | `list`, `create`, `update`, `delete` |
| 25–33 | Skills import | managed-skill compatibility operations; `discover`, `importMany`, `filesystemList` |
| 34–40 | Knowledge base | `list`, `create`, `importLink`, `scanFolder`, `importFiles`, `delete`, `reindex` |
| 41–55 | Conversations and memory | conversation `list/get/memory/create/append/compact/delete/clear`; notes/topics `list/save/delete`; memory `reindex` |
| 56–71 | Workspaces and files | workspace picker/open/create/get/close/list/read/write; files `list/read/write/add/remove` |
| 72–75 | Skills and chat | skills `list/get`; dialog `history`; `chat.send` |
| 76–83 | MCP | `list/create/update/set/delete/health/tools/fetchTools` |
| 84–93 | Providers | `list/create/update/models/contextWindow/fetchModels/checkConnection/test/set/delete` |
| 94–99 | Proxies | `list/create/update/delete/resetPassword/test` |
| 100–110 | Settings and conversations | settings `export/import/cleanup`; `version`; conversation `setModel/runStatus/deleteMessage`; skills `reindex/pickStart/pickStatus/openFolder` |
| 111–114 | Skill groups | `list/save/delete/assign` |
| 115–117 | Application | `update.status`, `update.apply`, `system.info` |
| 118–132 | Local LLM runtimes | Ollama/Llama.cpp install, settings CRUD, start, model refresh/list/pull/remove |

The historical managed-skill events at IDs 26–30 are retained for protocol compatibility. New UI code uses the current skill workflows and IDs rather than treating the compatibility entries as a separate API.

## Chat and streaming

Send a chat request on event `75`:

```json
{
  "e": 75,
  "d": {
    "dialog_id": "conversation-1",
    "workspace_id": "workspace-1",
    "content": "Inspect this project",
    "agentId": "agent-1",
    "skills": ["repository-review"],
    "asGoal": true
  }
}
```

`dialog_id` and `content` are required for a new request. Optional fields are `workspace_id`, `agentId`, `provider`, `model`, `skills`, `asGoal`, `resume`, and `errorMessageId`. A resume request reuses the prior user message and may resolve a displayed error by its message ID. The workspace must be open; when omitted the backend uses the `default` workspace ID.

Event `1` is a broadcast stream. Its payload is an object with a UUID `id`, a `type`, and a `payload`; stream payloads identify the conversation as `dialog_id`. Clients must ignore frames for another active conversation.

| Stream type | Meaning |
| --- | --- |
| `status` | Agent lifecycle state, including `thinking`, `awaiting_approval`, and `idle`. |
| `msg.start` | Starts an assistant message and identifies its model/provider. |
| `reasoning` | Reasoning text emitted by a provider. |
| `chat_stream`, `chunk` | Incremental assistant text. Both are published for compatibility. |
| `tool_call` | Completed or failed tool invocation with task-related details. |
| `request` | A user interaction request, such as approval, question, choice, or multichoice. |
| `goal`, `goal.clear` | Current tool-driven goal update or removal. |
| `error` | Chat error with a stable `message_id` when available. |
| `done` | Completion state, model/provider, token counts, context size, latency, and request-rate data. |
| `local_llm.install` | Progress/status published while installing a local runtime. |

Event `2` is reserved for the legacy stream input frame and is handled separately from RPC event `75`; browser code should use the shared WebSocket helpers in `web/src/lib/api.ts`.

## Goals, storage, and safety

When `asGoal` is true, the active provider creates a concise execution plan before the agent runs. The plan is not inserted into chat history. Task state changes only from actual tool callbacks. A successful run marks unneeded planned tasks as `skipped`, not `done`. The current goal and append-only snapshots are stored per dialog:

```text
datasource/dialogs/{profileId}/{sessionId}/goal.json
datasource/dialogs/{profileId}/{sessionId}/goals.jsonl
```

`conversation.get` returns the current session memory, goal, and goal snapshot list. `conversation.compact` refreshes session memory; `compact=true` is the boundary for model context compaction. Long-term notes, topics, and knowledge are searched only by explicit tools and are not injected automatically.

Workspace operations use an opened workspace and relative paths resolved through `os.OpenRoot`. Traversal, root escapes, and unsafe symlinks are rejected. Skills and knowledge imports are deliberately separate user-selected filesystem workflows and may read selected paths outside the workspace.

## Frontend delivery

The embedded React frontend is served from `/`; unknown client-side routes receive `index.html`, while `/api` paths and missing asset paths return 404. The application sends `Cache-Control: no-store, no-cache, must-revalidate, max-age=0` for every frontend response.
