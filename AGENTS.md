# Arwos AI Agent — guide for AI developers

## Project

Arwos AI Agent is a local, profile-aware AI workspace: a Go 1.26 service with an embedded React 18/Vite/Tailwind frontend. It manages OpenAI-compatible providers, agents, workspaces, skills, knowledge, memory, MCP servers, proxies, local LLM runtimes, and JSONL conversations.

The service is local-first. Keep it bound to localhost unless external access control is explicitly added. `/api/ws` is the only browser transport; do not add REST or HTTP JSON-RPC APIs for screens.

## Repository map

```text
cmd/arwos-agent/main.go          goppy/web/WebSocket/SQLite plugin wiring only
internal/app/                    lifecycle, WebSocket handlers, chat orchestration, SPA serving
internal/pkg/
  agent/                         AgentEngine registry and limits
  configstore/                   SQLite configuration and profiles
  dialog/                        JSONL history, session memory, goals and registries
  gittools/                      go-git tools scoped to an opened workspace
  interaction/                   model-to-user approvals, questions and choices
  knowledge/                     file-backed documents and Bleve index
  llama/, ollama/                local runtime install, processes and models
  llm/                           OpenAI-compatible streaming client
  mcp/                           HTTP/SSE/stdio MCP integration
  memorytools/                   explicit durable-memory search tools
  proxy/                         profile proxy storage and connectivity
  prompts/                       built-in English model prompts
  skills/                        skill files, import/discovery and groups
  systeminfo/, updater/          system data and release update workflow
  toolexecutor/                  common tool registry and builtin MCP catalog
  workspace/                     os.OpenRoot boundary and native picker
web/                             React UI, locales, WebSocket client and Vite config
datasource/migration/            embedded SQLite migrations
docs/api.md                      public WebSocket protocol reference
```

Keep `main.go` thin. Modules are constructed with `plugin.Inject` in `internal/pkg/plugin.go` or the app plugin. Static settings use typed goppy configuration groups with YAML tags, defaults, and validation. Per-dialog objects belong in registries/pools with acquire/release; do not create manual singletons.

## Frontend

The frontend is exclusively in `web/`; Angular is not used. The production build in `web/dist` is embedded by `web/embed.go`.

- Use `web/src/lib/api.ts`: `wsRequest` for RPC, `sendChat` for chat input, and `connectChat` / subscriptions for stream frames. Do not use `fetch` for browser API calls.
- Requests are `{ "e": eventId, "d": params }`. Keep shared IDs synchronized with `internal/app/api_websocket.go`; event IDs are append-only.
- Event `1` is the broadcast stream and event `2` is stream input. Stream payloads use `dialog_id`; filter frames for the active `convId`/`dialogId`.
- Wrap dynamic requests in `try/catch/finally`. Add UI models in `web/src/lib/data.ts` and API-facing types in `web/src/lib/api.ts`.
- Localize all visible text using `useT()` and the ten locale files under `web/src/locales/`: `bn`, `en`, `es`, `fr`, `hi`, `it`, `ko`, `pt`, `ru`, `zh`.
- Render model Markdown with `react-markdown` and `remark-gfm`. Heavy screens use `React.lazy`/`Suspense`; retain Vite `manualChunks` and source maps.

Preserve the ARWOS dark sci-fi style: abyss background, translucent glass cards, rounded geometry, indigo/cyan/violet accents, `font-display` headings, `font-sans` prose, and `font-mono` paths/models/code. Long URLs and paths need `min-w-0`, `truncate`, or `break-all`.

Use `SModal`, `SField`, `SBtn`, `SToggle`, `ScreenHeader`, and `Row`. Switchable state uses `SToggle`, never a native checkbox. Inputs disable spellcheck, autocorrect, and autocapitalize. Edit/delete list buttons follow the Provider-model pattern: `h-7 w-7`, icon/title, indigo edit hover, rose delete hover.

## WebSocket contract

Register each handler directly with `a.wss.SetEventHandler` in `App.Up`; do not make a string dispatcher or handler map. Transport handlers have `func(event.Event, ws.Meta) error` and should delegate domain behavior to separately named operations where practical. Return explicit parameter, not-found, and internal errors.

Current event ranges: configuration 10–11; profiles 12–16; agents 17–20; presets 21–24; skills/import 25–33; knowledge 34–40; conversations/memory 41–55; workspace/files 56–71; skills/chat 72–75; MCP 76–83; providers 84–93; proxies 94–99; settings/conversation utilities 100–110; skill groups 111–114; update/system 115–117; local Ollama/Llama.cpp 118–132. Consult `docs/api.md` before changing public events.

`chat.send` is event 75. It accepts `dialog_id`, `content`, `workspace_id`, `agentId`, `provider`, `model`, `skills`, `asGoal`, `resume`, and `errorMessageId`. Stream types include `status`, `msg.start`, `reasoning`, `chat_stream`, `chunk`, `tool_call`, `request`, `goal`, `goal.clear`, `error`, `done`, and `local_llm.install`. `done` reports total provider usage, current context size, latency and rate data; never mix total tokens with context size.

Never return provider API keys, proxy passwords, or MCP headers. Return only identifiers and flags such as `hasApiKey` / `hasPassword`.

## Tools and model behavior

Register tools from their owning package through `toolexecutor.Registry`. Every tool needs a stable exact name, useful description, JSON schema where applicable, and a context-aware handler. Do not add an application-level switch over tool names.

Builtin MCP servers use `toolexecutor.RegisterBuiltin` with a stable key, default prefix and underlying tools. Workspace, Git, skills, knowledge, memory, and user interaction are builtin servers. System MCP entries cannot be deleted; profile enabled state and aliases are in `profile_builtin_mcp_settings`. Agent-dependent MCP tools resolve via `mcp.AgentTools` as `prefix.alias`; expose only enabled servers selected for the agent.

All built-in prompt text belongs in `internal/pkg/prompts/prompts.go` as English constants. External operations accept `context.Context`, use timeouts, and close resources. Read HTTP/stdio response bodies through `internal/pkg/utils.ReadAllResponse` (100 MiB limit).

`memoryModel` controls automatic session-memory updates; empty disables them. `compactionModel` is only for context compaction. `compactionLevel` is `brief`, `balanced`, `detailed`, `comprehensive`, or `epic`. Automatic memory refresh runs after five user messages and must not interrupt chat on failure. Only `conversation.compact` with `compact=true` establishes the model-context boundary.

## Filesystem, data and goals

Workspaces are profile-scoped and can be open in parallel. Browser paths are relative and all workspace/Git operations use the active `os.Root` / `os.OpenRoot`. Reject traversal, root escapes, and unsafe symlinks. Reject force pushes; require explicit confirmation before discarding working-tree changes.

If an opened workspace has a root `AGENTS.md`, read it through `os.OpenRoot` and send it as a separate system message after the main system prompt and before the user request. A missing file is ignored.

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

With `asGoal: true`, generate and persist a concise plan before execution without inserting planning prose into history. Advance tasks only from actual AgentEngine tool callbacks. Successful but unneeded planned tasks are `skipped`, never `done`. Without `asGoal`, create a goal only after the first real tool callback. `goal.json` is current state and `goals.jsonl` is append-only history; `conversation.get` returns session memory and both goal forms.

Skills and knowledge imports deliberately accept user-selected paths outside a workspace. KB folder import supports Markdown, text, YAML, JSON, Go, HTML and JS/TS-family sources. Knowledge and durable memory use persistent Bleve indexes. `memory.search` and `knowledge.search` are explicit tools: never inject their records automatically into every prompt.

## SQLite, configuration and delivery

### Required backend libraries

Use the project-standard libraries for backend development; do not replace their responsibilities with competing frameworks or utility packages without an explicit architectural decision:

- `go.osspkg.com/goppy/v3` — application lifecycle, dependency injection, configuration, web, WebSocket, ORM, migrations, and SQLite plugins.
- `go.osspkg.com/logx` — structured application logging.
- `go.osspkg.com/algorithms`, `go.osspkg.com/bb`, `go.osspkg.com/cast`, `go.osspkg.com/do`, `go.osspkg.com/errors`, `go.osspkg.com/events`, `go.osspkg.com/ioutils`, `go.osspkg.com/network`, `go.osspkg.com/routine`, and `go.osspkg.com/syncing` — standard project utilities; prefer them when their capability fits the task.
- `github.com/blevesearch/bleve/v2` — persistent full-text indexes for knowledge, skills, and durable memory.

Use goppy ORM and the shared configuration store only. Migrations are embedded by `datasource.Migrations()` and installed by `orm.WithMigration(datasource.Migrations())`. The current sequence ends at `002_local_llm_enabled.sql`; inspect `datasource/migration/` before adding the next unique `NNN_short_description.sql` file.

Never rename or edit an applied migration. Change tables through new migrations, prefer `IF NOT EXISTS`, do not delete data unless explicitly required, and do not open a second SQLite connection.

`config/config.dev.yaml` supplies the localhost listener, SQLite database, knowledge/skills/dialog/local-LLM folders, knowledge page/search limits, dialog history limit, and agent iteration limit.

Run relevant checks:

```sh
GOCACHE=/tmp/arwos-go-cache go test ./...
pnpm --dir web typecheck
pnpm --dir web build
git diff --check
```

`make build` builds the frontend and Go binary with `sqlite_fts5`; `make run-local` starts the backend under the race detector. `make web-test` expects a `test` script, but none is currently defined in `web/package.json`; do not claim frontend unit tests passed until one exists.

Keep SPA fallback correct: serve `index.html` directly for routes without an extension, return 404 for missing assets and `/api/*` non-WebSocket paths, and send no-cache headers for every frontend response. Update `README.md` and `docs/api.md` with public protocol or operational changes.

Do not claim live browser, WebSocket, provider, updater, native-picker, Docker, or local-runtime validation unless actually performed. Add focused tests for RPC, storage, and UI behavior when changing them.
