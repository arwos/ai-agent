import type {
  AgentEntry,
  Conversation,
  KbDoc,
  LongTermNote,
  McpServer,
  MemoryPage,
  Preset,
  Profile,
  Provider,
  Proxy,
  Skill,
  SkillGroup,
  TopicMemory,
  Workspace,
} from "./data";

export type Db = {
  version: string;
  workspaces: Workspace[];
  conversations: Conversation[];
  skills: Skill[];
  skillGroups: SkillGroup[];
  providers: Provider[];
  mcpServers: McpServer[];
  proxies: Proxy[];
  kbDocs: KbDoc[];
  kbQuotaBytes: number;
  presets: Preset[];
  agents: AgentEntry[];
  longTermNotes: LongTermNote[];
  topicMemories: TopicMemory[];
};

export type AppData = Db & { profiles: Profile[]; activeProfileId: string };

export type DiscoveredSkill = {
  tempId: string;
  name: string;
  description: string;
  icon: Skill["icon"];
  accent: Skill["accent"];
  checked: boolean;
  origin: string;
};

export type AgentRequest = {
  reqId: string;
  kind: "approval" | "question" | "choice" | "multichoice";
  title: string;
  detail?: string;
  command?: string;
  question?: string;
  placeholder?: string;
  options?: { id: string; label: string; hint?: string }[];
};

export type PlanTask = {
  id: string;
  label: string;
  tools?: string[];
  dependsOn?: string[];
  attempts?: number;
  maxAttempts?: number;
  lastTool?: string;
  lastResult?: string;
  error?: string;
  startedAt?: string;
  finishedAt?: string;
  status: "pending" | "running" | "done" | "failed" | "skipped";
};

export type GoalState = {
  id: string;
  dialogId: string;
  goal: string;
  status: "running" | "awaiting_approval" | "paused" | "done" | "failed" | "stopped" | "incomplete";
  tasks: PlanTask[];
  approval?: { id: string; kind: "approval"; title: string; detail?: string; command?: string; createdAt?: string };
  createdAt?: string;
  updatedAt?: string;
};

export type BgTask = { id: string; label: string; progress: number };

export type ChatSocket = { send: (frame: Record<string, unknown>) => void; close: () => void };

type FrameHandler = (frame: Record<string, unknown>) => void;
type PendingRequest = { resolve: (value: unknown) => void; reject: (reason: Error) => void };

type WsEvent = number;

// Keep these values in sync with internal/app/api_websocket.go. Use named
// constants for events shared across screens so a newly appended backend event
// cannot silently turn a request into an unrelated handler call.
export const WS_EVENT_CONVERSATION_GET: WsEvent = 42;

export const WS_EVENT_CONVERSATION_RUN_STATUS: WsEvent = 108;

export const WS_EVENT_CONVERSATION_CREATE: WsEvent = 51;

export const WS_EVENT_SETTINGS_CLEANUP: WsEvent = 109;

export const WS_EVENT_OLLAMA_MODELS_REFRESH: WsEvent = 125;

export const WS_EVENT_OLLAMA_MODELS_LIST: WsEvent = 126;

export const WS_EVENT_OLLAMA_MODEL_PULL: WsEvent = 127;

export const WS_EVENT_OLLAMA_MODEL_REMOVE: WsEvent = 128;

export const WS_EVENT_LLAMA_MODELS_REFRESH: WsEvent = 129;

export const WS_EVENT_LLAMA_MODELS_LIST: WsEvent = 130;

export const WS_EVENT_LLAMA_MODEL_PULL: WsEvent = 131;

export const WS_EVENT_LLAMA_MODEL_REMOVE: WsEvent = 132;

class SocketTransport {
  private socket: WebSocket | null = null;
  private opening: Promise<void> | null = null;
  private retry: number | undefined;
  private closed = false;
  private pending = new Map<number, PendingRequest[]>();
  private listeners = new Set<FrameHandler>();

  subscribe(listener: FrameHandler): () => void {
    this.listeners.add(listener);
    void this.open();
    return () => this.listeners.delete(listener);
  }

  async request<T>(event: WsEvent, params: Record<string, unknown>): Promise<T> {
    await this.open();
    if (!event) throw new Error("Invalid WebSocket event");
    return new Promise<T>((resolve, reject) => {
      const pending: PendingRequest = { resolve: (value) => resolve(value as T), reject };
      this.pending.set(event, [...(this.pending.get(event) ?? []), pending]);
      this.socket?.send(JSON.stringify({ e: event, d: params }));
    });
  }

  sendChat(frame: Record<string, unknown>): void {
    if (!this.socket || this.socket.readyState !== WebSocket.OPEN) {
      this.emit({ type: "error", convId: frame.convId, message: "WebSocket is not connected" });
      return;
    }
    this.socket.send(JSON.stringify({ e: 2, d: frame }));
  }

  private async open(): Promise<void> {
    if (this.socket?.readyState === WebSocket.OPEN) return;
    if (this.opening) return this.opening;
    this.closed = false;
    this.opening = new Promise<void>((resolve, reject) => {
      const socket = new WebSocket(`${location.protocol === "https:" ? "wss:" : "ws:"}//${location.host}/api/ws`);
      this.socket = socket;
      socket.onopen = () => {
        if (this.retry !== undefined) {
          window.clearTimeout(this.retry);
          this.retry = undefined;
        }
        this.opening = null;
        this.emit({ type: "open" });
        resolve();
      };
      socket.onmessage = (event) => this.receive(event.data);
      socket.onerror = () => {
        if (socket.readyState !== WebSocket.OPEN) reject(new Error("WebSocket connection failed"));
      };
      socket.onclose = () => {
        if (this.socket === socket) this.socket = null;
        this.opening = null;
        for (const queue of this.pending.values()) {
          for (const pending of queue) pending.reject(new Error("WebSocket connection closed"));
        }
        this.pending.clear();
        this.emit({ type: "connection", status: "disconnected" });
        if (!this.closed) this.retry = window.setTimeout(() => void this.open().catch(() => undefined), 5000);
      };
    });
    try {
      await this.opening;
    } catch (error) {
      this.opening = null;
      throw error;
    }
  }

  private receive(raw: unknown): void {
    try {
      const envelope = JSON.parse(String(raw)) as { e?: number; d?: Record<string, unknown>; err?: string };
      if (envelope.e !== 1 && envelope.e !== 2) {
        const queue = this.pending.get(envelope.e ?? 0);
        const pending = queue?.shift();
        if (!pending) return;
        if (queue && queue.length === 0) this.pending.delete(envelope.e ?? 0);
        if (envelope.err) pending.reject(new Error(envelope.err));
        else pending.resolve(envelope.d);
        return;
      }
      if (envelope.e !== 1) return;
      const message = envelope.d;
      if (!message || typeof message.type !== "string") return;
      const payload = (message.payload ?? {}) as Record<string, unknown>;
      this.emit({
        ...payload,
        type: message.type,
        convId: payload.dialog_id ?? payload.dialogId,
        agentId: payload.agent_id ?? payload.agentId,
      });
    } catch {
      /* invalid frame */
    }
  }

  private emit(frame: Record<string, unknown>): void {
    this.listeners.forEach((listener) => listener(frame));
  }
}

const transport = new SocketTransport();

export function wsRequest<T = unknown>(event: WsEvent, params: Record<string, unknown> = {}): Promise<T> {
  return transport.request<T>(event, params);
}

export function sendChat(frame: Record<string, unknown>): void {
  transport.sendChat(frame);
}

const accent = (value: string): Skill["accent"] =>
  ["indigo", "cyan", "violet", "emerald", "amber"].includes(value) ? (value as Skill["accent"]) : "indigo";
const icon = (value: string): Skill["icon"] =>
  ["code", "chart", "feather", "lock", "search", "bot"].includes(value) ? (value as Skill["icon"]) : "bot";

export async function loadAll(): Promise<AppData> {
  const [
    workspaces,
    skills,
    skillGroups,
    providers,
    mcpServers,
    proxies,
    kb,
    presets,
    agents,
    profiles,
    longTermNotes,
    topicMemories,
    build,
  ] = await Promise.all([
    wsRequest<Workspace[]>(63),
    wsRequest<Skill[]>(72),
    wsRequest<SkillGroup[]>(111),
    wsRequest<Provider[]>(84),
    wsRequest<McpServer[]>(76),
    wsRequest<Proxy[]>(94),
    wsRequest<{ docs: KbDoc[]; quotaBytes: number }>(34),
    wsRequest<Preset[]>(21),
    wsRequest<AgentEntry[]>(17),
    wsRequest<{ profiles: Profile[]; activeId: string }>(12),
    wsRequest<MemoryPage<LongTermNote>>(44, { limit: 20 }),
    wsRequest<MemoryPage<TopicMemory>>(47, { limit: 20 }),
    wsRequest<{ version: string }>(102),
  ]);
  return {
    version: build.version,
    workspaces: Array.isArray(workspaces) ? workspaces : [],
    conversations: [],
    skillGroups: Array.isArray(skillGroups) ? skillGroups : [],
    proxies: Array.isArray(proxies) ? proxies : [],
    skills: (Array.isArray(skills) ? skills : []).map((item) => ({
      ...item,
      icon: icon(item.icon),
      accent: accent(item.accent),
    })),
    providers: (Array.isArray(providers) ? providers : []).map((item) => ({ ...item, apiKey: "" })),
    mcpServers: Array.isArray(mcpServers) ? mcpServers : [],
    kbDocs: (Array.isArray(kb.docs) ? kb.docs : []).map((doc) => ({
      ...doc,
      tags: Array.isArray(doc.tags) ? doc.tags : [],
      content: doc.content ?? "",
      source: doc.source ?? "",
      kind: doc.kind || "doc",
    })),
    kbQuotaBytes: kb.quotaBytes ?? 0,
    presets: Array.isArray(presets) ? presets : [],
    agents: (Array.isArray(agents) ? agents : []).map((item) => ({
      ...item,
      iconKey: icon(item.iconKey),
      accent: accent(item.accent),
    })),
    profiles: (Array.isArray(profiles.profiles) ? profiles.profiles : []).map((item) => ({
      ...item,
      accent: accent(item.accent),
    })),
    activeProfileId: profiles.activeId,
    longTermNotes: Array.isArray(longTermNotes.items) ? longTermNotes.items : [],
    topicMemories: Array.isArray(topicMemories.items) ? topicMemories.items : [],
  };
}

export function connectChat(_convID: string, _agentID: string, onFrame: FrameHandler): ChatSocket {
  const unsubscribe = transport.subscribe(onFrame);
  return {
    send(frame) {
      if (
        frame.type === "user.message" ||
        frame.type === "user.continue" ||
        frame.type === "user.response" ||
        frame.type === "user.stop" ||
        frame.type === "goal.pause" ||
        frame.type === "goal.stop"
      ) {
        transport.sendChat(frame);
      }
    },
    close() {
      unsubscribe();
    },
  };
}

export function subscribeConnection(onStatus: (connected: boolean) => void): () => void {
  return transport.subscribe((frame) => {
    if (frame.type === "open") onStatus(true);
    if (frame.type === "connection") onStatus(frame.status === "connected");
  });
}

export function subscribeFrames(listener: FrameHandler): () => void {
  return transport.subscribe(listener);
}

// Browser file APIs never reveal an absolute path. The native picker runs in
// the backend and returns only the workspace descriptor needed by the UI.
// The backend picker already opens and persists the selected workspace. Return
// its stable descriptor instead of creating it a second time from the sidebar.
export async function pickFolder(): Promise<Workspace | null> {
  try {
    const operation = await wsRequest<{ operationId: string }>(57);
    for (;;) {
      await new Promise((resolve) => window.setTimeout(resolve, 500));
      const status = await wsRequest<{
        status: "pending" | "completed" | "error";
        workspace?: Workspace;
        message?: string;
      }>(58, { operationId: operation.operationId });
      if (status.status === "pending") continue;
      if (status.status === "error") throw new Error(status.message ?? "Folder selection failed");
      const result = status.workspace;
      if (!result) throw new Error("Folder picker returned no workspace");
      return result;
    }
  } catch (error) {
    if (error instanceof Error && /cancel/i.test(error.message)) return null;
    throw error;
  }
}

export async function pickSkillFolder(): Promise<string | null> {
  try {
    const operation = await wsRequest<{ operationId: string }>(105);
    for (;;) {
      await new Promise((resolve) => window.setTimeout(resolve, 500));
      const status = await wsRequest<{ status: "pending" | "completed" | "error"; path?: string; message?: string }>(
        106,
        { operationId: operation.operationId },
      );
      if (status.status === "pending") continue;
      if (status.status === "error") throw new Error(status.message ?? "Folder selection failed");
      return status.path ?? null;
    }
  } catch (error) {
    if (error instanceof Error && /cancel/i.test(error.message)) return null;
    throw error;
  }
}
