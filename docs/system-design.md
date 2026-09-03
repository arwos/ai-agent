# Arwos AI Agent — System Design

## 1. Purpose and scope

Arwos AI Agent is a local-first, profile-aware AI workspace. It combines an embedded React frontend with a Go service that orchestrates conversations, tools, workspaces, knowledge, memory, MCP servers, providers, and local LLM runtimes.

The service is intended to run on localhost. Browser screens communicate with the backend only through the WebSocket endpoint `/api/ws`.

## 2. Requirements

### Functional

- Manage profiles, agents, presets, providers, proxies, skills, MCP servers, workspaces, and local LLM settings.
- Send model requests and stream reasoning, text, tool calls, approvals, goals, errors, and completion metrics.
- Execute built-in and MCP tools with workspace and user-interaction boundaries.
- Persist conversations as JSONL and store configuration in SQLite.
- Import and search knowledge, skills, and durable memory using file-backed stores and Bleve indexes.
- Install, start, stop, refresh, download, and remove Ollama and llama.cpp models.
- Serve the production frontend from embedded assets with SPA fallback.

### Non-functional

- Keep the application local-first and avoid exposing secrets in responses.
- Enforce workspace path boundaries and reject traversal or unsafe symlinks.
- Preserve append-only numeric WebSocket event IDs for protocol compatibility.
- Support graceful lifecycle management, context cancellation, bounded response reads, and structured logging.
- Keep `main.go` limited to application wiring; domain and technical behavior belongs in `internal/` packages.

## 3. High-level architecture

```text
Browser (React/Vite)
        │ WebSocket /api/ws
        ▼
Go application (internal/app)
        │
        ├── dialog / agent orchestration
        ├── RPC event handlers
        ├── embedded SPA serving
        └── plugin and lifecycle wiring
        │
        ├── agent and tool executor
        ├── LLM providers / streaming client
        ├── MCP manager and builtin MCP tools
        ├── workspace and Git services
        ├── knowledge, skills, and memory stores
        ├── Ollama / llama.cpp supervisors
        └── configstore and SQLite migrations
```

The application is assembled through goppy dependency injection. Technical services live under `internal/pkg`; application lifecycle and WebSocket transport live under `internal/app`.

## 4. Main components

| Component | Responsibility | Primary persistence or boundary |
| --- | --- | --- |
| `internal/app` | Lifecycle, RPC handlers, chat orchestration, stream publication, SPA serving | WebSocket and application boundary |
| `internal/pkg/agent` | Agent execution loop, model calls, tool callbacks, goals | Dialog store and tool registry |
| `internal/pkg/llm` | OpenAI-compatible requests, native/text tool calling, streaming, usage | External provider endpoints |
| `internal/pkg/toolexecutor` | Stable tool registry, schemas, builtin MCP catalog | Context-aware handlers |
| `internal/pkg/mcp` | HTTP, SSE, and stdio MCP integration | Configured MCP servers |
| `internal/pkg/dialog` | JSONL history, session memory, goals, registries | `datasource/dialogs/{profileId}/{sessionId}` |
| `internal/pkg/configstore` | Profiles, providers, agents, settings, integrations | `datasource/db/sqlite.db` |
| `internal/pkg/workspace` | Open workspace roots, relative file operations, picker | `os.Root` boundary |
| `internal/pkg/knowledge` | Documents, imports, indexing and search | `datasource/knowledge/{profileId}` |
| `internal/pkg/skills` | Skill discovery, import, groups and indexing | `datasource/skills/{profileId}` |
| `internal/pkg/memorytools` | Explicit durable-memory search tools | `datasource/memory/{profileId}` |
| `internal/pkg/ollama` | Ollama installation, process control and models | `datasource/llm` and Ollama API |
| `internal/pkg/llama` | llama.cpp installation, cache/model management and process control | `datasource/llm` and llama API |
| `web` | React UI, localization, WebSocket client and embedded build | `web/dist`, embedded at build time |

## 5. Core data flow: chat

1. The browser sends event `75` (`chat.send`) with a dialog ID, content, optional workspace, agent, provider, model, skills, goal, or resume parameters.
2. `internal/app` validates the request, loads profile and dialog state, resolves the opened workspace, and constructs an agent engine.
3. The engine assembles the system prompt, selected skills, workspace instructions, conversation history, and available tool definitions.
4. The LLM client sends a request to the selected provider. Streaming providers emit incremental frames.
5. A tool call is validated against the registry and schema, executed with a scoped context, and persisted with its result.
6. The result is returned to the model for the next iteration until the model completes or the iteration limit is reached.
7. The service publishes stream frames on event `1`, including `msg.start`, `reasoning`, `chunk`, `tool_call`, `request`, `goal`, `error`, and `done`.
8. Assistant and tool messages are persisted in the dialog JSONL history. Provider secrets are never returned.

## 6. Tool execution and interaction safety

Tools are owned by their packages and registered through `toolexecutor.Registry`. Each tool has an exact stable name, description, input schema, and context-aware handler. Builtin MCP tools are exposed through the same registry as external MCP tools.

Workspace and Git tools operate through the active opened root. Relative paths are required, traversal is rejected, and destructive or approval-sensitive operations use the interaction tools. When a model must ask, choose, or obtain approval, the corresponding user-interaction tool is used instead of presenting an untracked prose choice.

For models without native tool calling, the agent supplies a machine-readable JSON contract and validates the returned tool name and arguments before dispatch. Invalid or incomplete calls are treated as protocol errors and can be retried privately.

## 7. WebSocket API

Requests use the stable envelope:

```json
{"e": 75, "d": {"dialog_id": "conversation-1", "content": "Inspect this project"}}
```

Event ranges are grouped by domain: configuration `10–11`, profiles `12–16`, agents `17–20`, skills and knowledge `25–40`, conversations and memory `41–55`, workspaces `56–71`, MCP `76–83`, providers `84–93`, proxies `94–99`, settings and system `100–117`, and local LLM runtimes `118–132`.

The complete public contract is maintained in [docs/api.md](docs/api.md). Event IDs are append-only and clients must not infer IDs from operation names.

## 8. Persistence and storage layout

```text
datasource/db/sqlite.db
datasource/dialogs/{profileId}/{sessionId}/history.jsonl
datasource/dialogs/{profileId}/{sessionId}/memory.json
datasource/dialogs/{profileId}/{sessionId}/goal.json
datasource/dialogs/{profileId}/{sessionId}/goals.jsonl
datasource/memory/{profileId}/note/ and topics/
datasource/knowledge/{profileId}/documents/ and index/
datasource/skills/{profileId}/
datasource/llm/
```

SQLite schema changes are applied through embedded, uniquely numbered migrations. File-backed data is profile-scoped. Search indexes are rebuildable from their source collections.

## 9. Local LLM runtimes

Ollama and llama.cpp are managed as local supervised processes. Runtime settings identify binary paths, ports, model locations, and enabled state. Model operations publish progress to the stream and refresh installed-model state. llama.cpp router mode requires a restart after model removal so its cached model list is refreshed.

The service remains bound to localhost by default. Runtime servers must not be exposed to untrusted networks without explicit authentication and access control.

## 10. Frontend delivery

The React/Vite frontend is built into `web/dist` and embedded by `web/embed.go`. The backend serves static assets with no-cache headers, returns `index.html` for client-side routes without file extensions, and returns 404 for missing assets and non-WebSocket `/api` paths. Browser API calls use shared helpers in `web/src/lib/api.ts`; visible text is localized across the supported locale files.

## 11. Operational considerations

- Configuration defaults are supplied by `config/config.dev.yaml`; production deployments should provide an appropriate local configuration.
- The service should be run with a per-user service manager when background startup is required: systemd user units on Linux, launchd agents on macOS, and Windows service integration on Windows.
- Graceful shutdown must stop supervised local runtimes and release database, index, WebSocket, and MCP resources.
- External HTTP operations use timeouts and bounded response reads; transient downloads may retry while avoiding corrupted partial files.
- Logs should include lifecycle, provider/runtime, tool, and failure context without API keys, passwords, or MCP headers.
- Live provider, browser, WebSocket, and local-runtime behavior requires environment-specific validation in addition to repository tests.

## 12. Trade-offs and risks

| Decision | Benefit | Cost or risk |
| --- | --- | --- |
| WebSocket-only browser transport | One bidirectional channel supports streaming and interaction | Clients must handle reconnects and event filtering |
| JSONL conversation history | Simple append-only audit trail and easy recovery | Large histories require limits and compaction |
| SQLite for configuration | Transactional, local, dependency-light storage | Schema evolution requires disciplined migrations |
| Explicit knowledge and memory tools | Controls prompt size and avoids automatic data leakage | Models must choose to search when context is needed |
| Embedded frontend | Single deployable binary and consistent assets | Frontend changes require a rebuild of the Go binary |
| Local runtime supervision | Works offline and keeps models under user control | Runtime availability, ports, caches, and model formats vary by host |
| Scoped workspace roots | Strong protection against path escape | Operations must use relative paths and an opened workspace |

## 13. Verification strategy

Repository checks should include:

```sh
GOCACHE=/tmp/arwos-go-cache go test ./...
pnpm --dir web typecheck
pnpm --dir web build
git diff --check
```

The system design is based on the current repository structure and documented WebSocket API. Provider connectivity, native runtime behavior, service-manager startup, and browser interaction remain deployment-environment checks rather than purely static repository checks.
