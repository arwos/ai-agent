# Arwos AI Agent

Arwos AI Agent is a local, profile-aware AI workspace for working with files, code, skills, knowledge, memory, MCP tools, and OpenAI-compatible language models.

The application runs as a single Go service with an embedded React frontend. Browser communication uses the goppy WebSocket transport at `/api/ws`.

> Arwos is actively evolving. It is intended for local development and experimentation. Review the security notes before exposing the service outside the local machine.

## Highlights

- Local-first Go backend with an embedded React/Vite UI.
- OpenAI-compatible providers, including managed local Ollama and Llama.cpp runtimes.
- Streaming responses, reasoning events, tool calls, approvals, and live status over WebSocket.
- Multiple profiles with isolated agents, providers, MCP settings, conversations, skills, knowledge, and memory.
- Multiple user-selected workspaces with safe file access through Go's `os.OpenRoot`.
- Built-in filesystem and Git tools scoped to the active workspace.
- MCP integrations over HTTP, SSE, and stdio.
- File-backed skills, knowledge, and memory with persistent Bleve indexes.
- Conversation history in JSONL, session memory, execution goals, compaction, and token metrics.
- UI localization in Bengali, English, Spanish, French, Hindi, Italian, Korean, Portuguese, Russian, and Chinese.
- Local-runtime installation, model download, start controls, system information, and self-update controls.
- Import and export of profile settings with Base64-encoded secrets.

## Contents

- [Requirements](#requirements)
- [Quick start](#quick-start)
- [Configuration](#configuration)
- [Using the application](#using-the-application)
- [Architecture](#architecture)
- [Data and storage](#data-and-storage)
- [WebSocket API](#websocket-api)
- [Development](#development)
- [Security](#security)
- [Contributing](#contributing)
- [License](#license)

## Requirements

- Go 1.26 or newer.
- Node.js with pnpm for frontend development.
- An OpenAI-compatible provider or a local Ollama/Llama.cpp server for chat.

The SQLite FTS5 build tag is enabled by default through `GO_TAGS=sqlite_fts5`.

## Quick start

```sh
git clone https://github.com/arwos/ai-agent.git
cd agent
make web-install
make build
./build/arwos-agent --config=./build/config.yaml --config-recovery
```

The binary contains the default contents of `config/config.dev.yaml`. During
`make build` the same file is also placed beside the binary as `build/config.yaml`.
The service can install itself as a per-user systemd service (without root):

```sh
./build/arwos-agent install-as-service
```

Удаление службы:

```sh
./build/arwos-agent uninstall-as-service
```

The command detects the binary location, creates the neighboring `config.yaml`
from the embedded default if it does not exist, and starts it with
`--config=<path>`. Check it with
`systemctl --user status arwos-agent.service`.

Open <http://127.0.0.1:8080>.

For frontend development:

```sh
make web-dev
```

For a local backend run with the frontend rebuilt first:

```sh
make run-local
```

## Configuration

The development configuration is [config/config.dev.yaml](config/config.dev.yaml). It configures the HTTP listener, SQLite database, file-backed stores, logging, and agent limits.

The default listener is `127.0.0.1:8080`. The application creates data directories when needed. Configure providers, agents, MCP servers, proxies, skills, and workspaces from the Settings screen.

Provider model names use the public form `<model>@<provider>`; the backend resolves that value into the stored provider and model. Credentials and MCP secrets are stored locally and are never returned in normal API responses.

## Using the application

1. Start the service and open the web interface.
2. Create or select a profile.
3. Add a provider and load its models, or install/configure a local Ollama or Llama.cpp runtime.
4. Create an agent and select its main model and optional supporting models.
5. Open a workspace directory.
6. Create a conversation and send a message.

### Goals

Enable **Goal** in the composer to ask the active model for an execution plan before processing a request. The plan is stored separately from chat history. Actual tool calls update task status, results, errors, attempts, dependencies, and timestamps. When all executable tasks finish, the backend asks for completion approval; declining starts a verification pass.

### Skills, knowledge, and memory

Skills can be created manually, discovered, and imported from user-selected directories, Git repositories, and supported archives. They can also be organized into groups. Knowledge documents and profile memory are indexed locally with Bleve. Knowledge and memory search are explicit agent tools and are not silently injected into every prompt.

### Workspace safety

Workspace paths sent by the browser are relative paths. File operations are resolved through the selected workspace's `os.OpenRoot`; traversal, root escapes, and symlinks are rejected. Skills and knowledge imports are separate workflows and may read user-selected paths outside a workspace.

## Architecture

```text
cmd/arwos-agent/main.go
        |
        v
internal/app       lifecycle and WebSocket event handlers
        |
        +-- internal/pkg/agent       AgentEngine and goals
        +-- internal/pkg/llm         OpenAI-compatible/Ollama client
        +-- internal/pkg/workspace   os.OpenRoot filesystem boundary
        +-- internal/pkg/toolexecutor shared tool registration and dispatch
        +-- internal/pkg/mcp          HTTP/SSE/stdio MCP clients
        +-- internal/pkg/dialog       JSONL history and session memory
        +-- internal/pkg/skills       file-backed skills, groups, and indexes
        +-- internal/pkg/knowledge    file-backed knowledge and indexes
        +-- internal/pkg/configstore  SQLite configuration repository
        +-- internal/pkg/prompts      built-in English model prompts
        +-- internal/pkg/ollama       local Ollama install and runtime control
        +-- internal/pkg/llama        local Llama.cpp install and runtime control
        +-- internal/pkg/updater      release update checks and apply workflow
        |
        v
web/                 React 18 + TypeScript + Vite + Tailwind
```

Modules are initialized through goppy dependency injection. Static module settings are typed configuration groups; runtime-scoped objects such as dialog engines are acquired from registries and released after use.

## Data and storage

```text
datasource/db/sqlite.db
datasource/dialogs/<profileId>/<sessionId>/history.jsonl
datasource/dialogs/<profileId>/<sessionId>/memory.json
datasource/dialogs/<profileId>/<sessionId>/goal.json
datasource/dialogs/<profileId>/<sessionId>/goals.jsonl
datasource/memory/<profileId>/note/
datasource/memory/<profileId>/topics/
datasource/knowledge/<profileId>/documents/
datasource/knowledge/<profileId>/index/
datasource/skills/<profileId>/
```

`goals.jsonl` contains the latest append-only snapshot for each goal in a conversation. SQLite migrations are embedded into the binary and installed through goppy ORM migration support. Applied migrations must not be edited; add a new numbered migration when a schema change is required.

## WebSocket API

The frontend does not use REST or HTTP JSON-RPC. Browser requests use numeric event IDs registered in [internal/app/api_websocket.go](internal/app/api_websocket.go):

```json
{"e":42,"d":{"id":"conversation-1"}}
```

Chat input is sent through event `2`:

```json
{"e":2,"d":{"type":"user.message","convId":"conversation-1","workspaceId":"workspace-1","text":"Inspect this project","asGoal":true}}
```

Streaming event `1` carries typed payloads such as `status`, `msg.start`, `reasoning`, `chunk`, `tool_call`, `request`, `goal`, `error`, and `done`. See [docs/api.md](docs/api.md) for the event catalog and payloads.

## Development

```sh
make web-install
GOCACHE=/tmp/arwos-go-cache go test ./...
pnpm --dir web typecheck
pnpm --dir web build
git diff --check
make build
```

| Target | Description |
| --- | --- |
| `make web-install` | Install frontend dependencies with pnpm |
| `make web-dev` | Start the Vite development server |
| `make web-build` | Build the frontend bundle |
| `make web-test` | Run frontend typecheck, then the configured frontend test script |
| `make build` | Build the frontend and Go binary |
| `make run-local` | Build the frontend and run the backend with race detection |
| `make lint` | Run goppy linting |
| `make ci` | Run the project validation pipeline |

The frontend bundle is emitted under `web/dist` and embedded into the Go binary by [web/embed.go](web/embed.go).

`web/package.json` currently has no `test` script, so use `pnpm --dir web typecheck` and `pnpm --dir web build` for the available frontend checks. `make web-test` is intended for use once a test script is added.

## Security

- Keep the service bound to localhost unless access control is provided externally.
- Treat exported settings as sensitive: Base64 is encoding, not encryption.
- Never commit `datasource/`, provider credentials, MCP headers, or proxy passwords.
- Workspace tools are restricted to the active `os.OpenRoot`.
- Destructive filesystem and Git operations require explicit approval.
- External provider and MCP requests use context cancellation and timeouts.

If you find a security issue, avoid publishing sensitive details in a public issue. Contact the maintainers privately when a security contact is available.

## Contributing

Contributions are welcome. Before opening a pull request:

1. Keep backend business logic in `internal/app` and reusable modules in `internal/pkg`.
2. Use goppy WebSocket events for browser communication.
3. Preserve profile isolation and workspace root boundaries.
4. Add or update tests for behavior changes.
5. Update [docs/api.md](docs/api.md) and this README when public behavior changes.
6. Run the checks listed in [Development](#development).

Please keep pull requests focused and describe compatibility or migration impact clearly.

## License

No `LICENSE` file is currently present in this repository. Add the project's chosen license before distributing Arwos as an Open Source release.
