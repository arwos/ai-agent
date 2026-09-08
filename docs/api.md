# WebSocket API

The browser API is available only through WebSocket at `/api/ws`. There is no REST or HTTP JSON-RPC API for application screens.

The protocol uses stable numeric event IDs from `internal/app/api_events.go`. Send a request as `{"e": <event-id>, "d": <parameters>}`. A successful RPC response carries the handler result in `d`; a failed request carries `err`. Event IDs are append-only: clients must use the IDs from the matching application version and must not infer an ID from a method name.

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
| 118–138 | Local LLM runtimes | Ollama/Llama.cpp install, settings CRUD, start, model refresh/list/pull/remove |
| 133–141 | Workflows | `workflow.list`, `workflow.get`, `workflow.save`, `workflow.delete`, `workflow.import`, `workflow.export`, `workflow.pause`, `workflow.continue`, `workflow.stop` |

Workflow management uses append-only events `133–141`. Running a workflow uses `chat.send` with `mode: "workflow"` and `workflow_id`; pause, continue, and stop use their dedicated workflow events.

The historical managed-skill events at IDs 26–30 are retained for protocol compatibility. New UI code uses the current skill workflows and IDs rather than treating the compatibility entries as a separate API.

## Chat and streaming

Send a chat request through the RPC event `75`:

```json
{
  "e": 75,
  "d": {
    "dialog_id": "conversation-1",
    "workspace_id": "workspace-1",
    "content": "Inspect this project",
    "agentId": "agent-1",
    "skills": ["repository-review"],
    "asGoal": true,
    "mode": "normal"
  }
}
```

`dialog_id` and `content` are required for a new request. Optional fields are `workspace_id`, `agentId`, `provider`, `model`, `skills`, `asGoal`, `resume`, `errorMessageId`, `mode`, and `workflow_id`. Valid modes are `normal`, `plan`, `consult`, and `workflow`; `workflow_id` is required for `workflow` and rejected for unrelated modes. A resume request reuses the prior user message and may resolve a displayed error by its message ID. The workspace must be open; when omitted the backend uses the `default` workspace ID.

Mode semantics are per message. `normal` permits the selected agent's configured capabilities. `plan` generates and persists a goal plan in `awaiting_approval` before executable tools run; approval resumes that goal without a synthetic user message. `consult` is read-only and permits only `fs.list_dir`, `fs.read_file`, `fs.file_info`, `fs.grep`, `git.status`, `git.diff`, and `git.log`; MCP and all other tools are unavailable. `workflow` loads a validated immutable YAML snapshot, expands it into ordered goal tasks, requires approval, and then executes one current task at a time.

Event `1` is a broadcast stream. Its payload is an object with a UUID `id`, a `type`, and a `payload`; stream payloads identify the conversation as `dialog_id` when they belong to a conversation. Clients must ignore frames for another active conversation.

| Stream type | Meaning |
| --- | --- |
| `status` | Agent lifecycle state, including `thinking`, `awaiting_approval`, and `idle`. |
| `msg.start` | Starts an assistant message and identifies its model/provider. |
| `reasoning` | Reasoning text emitted by a provider. |
| `chat_stream`, `chunk` | Incremental assistant text. Both are published for compatibility. |
| `tool_call` | Completed or failed tool invocation with task-related details. |
| `request` | A user interaction request, such as approval, question, choice, or multichoice. |
| `goal`, `goal.clear` | Current tool-driven goal update or removal. |
| `goal.update` | Incremental update to the current goal state. |
| `bg` | Background tool or task progress and completion data. |
| `message.deleted` | Notification that a conversation message was deleted. |
| `error` | Chat error with a stable `message_id` when available. |
| `done` | Completion state, model/provider, token counts, context size, latency, and request-rate data. |
| `local_llm.install` | Progress/status published while installing a local runtime. |
| `local_llm.model` | Start, completion, or error state for a local model operation. |
| `local_llm.model_pull` | Progress/status published while downloading a local model. |

Child execution may publish stream frames with the child `dialog_id`. A parent-scoped `subagent` or `bg` payload can advertise child status to the parent UI. The first `subagent` frame includes a safe `conversation` metadata object for the new child, allowing the UI to add it to the dialog tree before history hydration completes. Clients must filter both parent and child frames by dialog ID and must not treat a waiting parent as complete until its callback continuation finishes.

Event `2` is the legacy stream-input frame and is handled separately from RPC event `75`. It carries `user.message`, `user.continue`, `user.stop`, `user.response`, `goal.pause`, and `goal.stop` frames. Browser code should use the shared WebSocket helpers in `web/src/lib/api.ts` rather than constructing envelopes directly.

## Goals, storage, and safety

When `asGoal` is true, the active provider creates a concise execution plan before the agent runs. The plan is not inserted into chat history. Task state changes only from actual tool callbacks. A successful run marks unneeded planned tasks as `skipped`, not `done`. The current goal and append-only snapshots are stored per dialog:

```text
datasource/dialogs/{profileId}/{sessionId}/goal.json
datasource/dialogs/{profileId}/{sessionId}/goals.jsonl
```

`conversation.get` returns the current session memory, goal, and goal snapshot list. `conversation.compact` refreshes session memory; `compact=true` is the boundary for model context compaction. Long-term notes, topics, and knowledge are searched only by explicit tools and are not injected automatically.

Child conversations are persisted separately and linked to their parent by `Conversation.ParentID`; their histories remain available independently. Goal task state stores the workflow version/hash, child dialog, immutable child context, hashed run token, retry state, and bounded result/error envelope. On restart, pending child jobs are reconstructed from the persisted run snapshot with the same token and task context, unless a live job already exists. Active snapshots and artifacts remain when a workflow definition is edited or deleted.

Workspace operations use an opened workspace and relative paths resolved through `os.OpenRoot`. Traversal, root escapes, and unsafe symlinks are rejected. Skills and knowledge imports are deliberately separate user-selected filesystem workflows and may read selected paths outside the workspace.

## Agent MCP contracts

The intended profile-scoped builtin MCP capabilities are:

- `agent.list` returns safe metadata only: agent ID, name, description, and an available capability summary. It never returns credentials or secrets.
- `agent.run` accepts `agent_id`, `task`, expected results, `parent_dialog_id`, and an optional `task_id`. Ad hoc delegation may omit `task_id`; workflow execution must bind it to the current immutable goal task. The selected agent must provide every workflow-required tool and skill before a child is created. The call creates a child dialog and pending run, then immediately returns the child dialog ID and waiting status.
- `agent.result` is child-only and accepts `{run_token, status, result, error?, artifacts?, changed_files?}`. `status` is `success` or `failed`; success requires bounded result text or declared metadata, and failure requires `error`. Artifacts are immutable IDs and metadata. Changed files are backend-validated relative paths with bounded line ranges. The callback envelope is the canonical parent result; child final text is UI/audit data only.
- `workflow.review_result` is reviewer-child-only and accepts `{run_token, verdict, summary, evidence?, feedback?}`. `verdict` is `pass` or `fail`; `fail` requires actionable `feedback`.

The server advertises both callbacks with JSON schemas to child models. `run_token`, `status`/`verdict`, and `summary` are required by the schema where applicable; the token must be copied exactly from the child prompt. Callback arguments are flat JSON fields, not nested under `result`, `review`, or another wrapper. This is also the required format when calling the tools through an OpenAI-compatible provider.

Delegation inherits mode restrictions. Consult children are read-only, and plan children remain read-only before approval. Children cannot call `agent.run` or user-interaction tools and must call `agent.result`; ending without it fails the pending run with a protocol error. Callback acceptance is one-time and validates the token, profile, parent dialog, child dialog, task, and completion state. The child performs its final turn after the callback, then the parent resumes with the structured callback payload and no new user message. Wrong-profile, wrong-task, duplicate, and late callbacks are explicit errors.

Workflow YAML is fixed-schema data, not executable model code. It contains `version`, `id`, `name`, `description`, `enabled`, and ordered `steps`; every step requires a concrete `agent_id`, acceptance criteria, and `retry.max_attempts` between one and ten. It may declare substeps, dependencies, required tools, and required skills. At run start the server snapshots the definition, starts one Builder child per eligible step, then starts an isolated Reviewer child using the agent selected in chat. Builder completion requires `agent.result`; Reviewer completion requires `workflow.review_result` with a `pass` or `fail` verdict. A failed review retries the Builder until its limit, then stops the workflow. Reviewer children use consult-mode read-only tools and cannot modify the workspace. `artifact.create`, `artifact.get`, and `artifact.read` use profile-scoped immutable snapshots under `datasource/workflow/<profileId>/artifacts/`; prompts carry IDs/metadata rather than implicit paths.

## Frontend delivery

The embedded React frontend is served from `/`; unknown client-side routes receive `index.html`, while `/api` paths and missing asset paths return 404. The application sends `Cache-Control: no-store, no-cache, must-revalidate, max-age=0` for every frontend response.
