import { lazy, Suspense, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { AnimatePresence, motion } from "framer-motion";
import {
  CheckCircle2,
  Download,
  Eraser,
  FolderOpen,
  Hexagon,
  Info,
  MessageSquare,
  Upload,
  XCircle,
} from "lucide-react";
import { ACCENTS, SKILL_ICONS, nowTime, uid } from "./lib/data";
import type {
  AccentKey,
  AgentStatus,
  ChatMessage,
  Conversation,
  LiveAgent,
  Preset,
  Profile,
  View,
  Workspace,
} from "./lib/data";
import {
  connectChat,
  loadAll,
  sendChat,
  subscribeConnection,
  WS_EVENT_CONVERSATION_CREATE,
  WS_EVENT_CONVERSATION_GET,
  WS_EVENT_CONVERSATION_RUN_STATUS,
  WS_EVENT_SETTINGS_CLEANUP,
  wsRequest,
} from "./lib/api";
import type { AgentRequest, BgTask, Db, GoalState } from "./lib/api";
import { I18nProvider, useT } from "./lib/i18n";
import type { TVars } from "./lib/i18n";
import { Sidebar } from "./components/Sidebar";
import { Header } from "./components/Header";
import { ChatArea } from "./components/ChatArea";
import { AgentRequestModal, BgPlate, FilesExplorer, GoalPlate, InputBar } from "./components/Workspace";
import { RightPanel } from "./components/RightPanel";
import type { Notify } from "./components/settings/SkillsProviders";
import {
  PresetsScreen,
  ProvidersScreen,
  SkillsScreen,
  SModal,
  SBtn,
  SToggle,
} from "./components/settings/SkillsProviders";

const AgentsScreen = lazy(() =>
  import("./components/settings/AgentsScreen").then((m) => ({ default: m.AgentsScreen })),
);
const KbScreen = lazy(() => import("./components/settings/McpKb").then((m) => ({ default: m.KbScreen })));
const McpScreen = lazy(() => import("./components/settings/McpKb").then((m) => ({ default: m.McpScreen })));
const NetworkScreen = lazy(() =>
  import("./components/settings/NetworkScreen").then((m) => ({ default: m.NetworkScreen })),
);
const MemoryScreen = lazy(() =>
  import("./components/settings/MemoryScreen").then((m) => ({ default: m.MemoryScreen })),
);
const SystemInfoScreen = lazy(() =>
  import("./components/settings/SystemInfoScreen").then((m) => ({ default: m.SystemInfoScreen })),
);

type Toast = { id: string; kind: "ok" | "err" | "info"; msg: string };
const transferCategories = [
  "appSettings",
  "providers",
  "mcp",
  "proxies",
  "agents",
  "presets",
  "skills",
  "kb",
  "notes",
  "topics",
] as const;

const views: View[] = [
  "chat",
  "settings",
  "skills",
  "providers",
  "mcp",
  "agents",
  "presets",
  "kb",
  "network",
  "memory",
  "systemInfo",
];
type SettingsTab = Exclude<View, "chat" | "settings">;
function hashState(): { view: View; conversationId: string | null; settingsTab: SettingsTab } {
  const parts = window.location.hash.replace(/^#\/?/, "").split("/").filter(Boolean).map(decodeURIComponent);
  if (parts[0] === "chat") return { view: "chat", conversationId: parts[1] ?? null, settingsTab: "agents" };
  if (parts[0] === "settings")
    return {
      view: "settings",
      conversationId: null,
      settingsTab:
        views.includes(parts[1] as View) && parts[1] !== "chat" && parts[1] !== "settings"
          ? (parts[1] as SettingsTab)
          : "agents",
    };
  const view = views.includes(parts[0] as View) ? (parts[0] as View) : "chat";
  return { view, conversationId: null, settingsTab: view === "chat" || view === "settings" ? "agents" : view };
}

function writeHash(view: View, conversationId: string | null, settingsTab: SettingsTab) {
  const value =
    view === "chat" && conversationId
      ? `#/chat/${encodeURIComponent(conversationId)}`
      : view === "settings"
        ? settingsTab === "agents"
          ? "#/settings"
          : `#/settings/${settingsTab}`
        : `#/${view}`;
  if (window.location.hash !== value) window.history.replaceState(null, "", value);
}

function lastAgentMessageIndex(messages: ChatMessage[]): number {
  for (let index = messages.length - 1; index >= 0; index--) {
    if (messages[index].role === "agent") return index;
  }
  return -1;
}

function ensureStreamingAgentMessage(
  messages: ChatMessage[],
  model: unknown,
  provider: unknown,
): [ChatMessage[], number] {
  if (messages[messages.length - 1]?.role === "agent") return [messages, messages.length - 1];
  const next = [
    ...messages,
    {
      id: uid(),
      role: "agent" as const,
      time: nowTime(),
      parts: [],
      model: String(model ?? ""),
      provider: String(provider ?? ""),
    },
  ];
  return [next, next.length - 1];
}

function isExactToolCallText(message: ChatMessage | undefined, toolName: string): boolean {
  if (!message || message.role !== "agent") return false;
  const text = message.parts
    .filter((part) => part.type !== "reasoning")
    .map((part) => part.content)
    .join("")
    .trim();
  if (!text) return false;
  try {
    return JSON.parse(text)?.tool === toolName;
  } catch {
    return false;
  }
}

// A tool event can arrive while conversation.get is restoring history after a
// reconnect. Keep any live-only message that is not represented in the JSONL
// snapshot instead of replacing the whole chat with an older snapshot.
function mergeConversationHistory(current: Conversation, restored: Conversation): Conversation {
  const restoredIDs = new Set(restored.messages.map((message) => message.id));
  const pendingLiveMessages = current.messages.filter((message) => {
    if (restoredIDs.has(message.id)) return false;
    if (message.role !== "tool") {
      const content = message.parts.map((part) => part.content).join("\n");
      // Browser-side messages use a temporary UUID. Their persisted JSONL
      // counterpart has another UUID, so compare their semantic content while
      // reconciling a running conversation after reconnect.
      return !restored.messages.some(
        (item) => item.role === message.role && item.parts.map((part) => part.content).join("\n") === content,
      );
    }
    const args = JSON.stringify(message.toolArguments ?? {});
    return !restored.messages.some(
      (item) =>
        item.role === "tool" && item.toolName === message.toolName && JSON.stringify(item.toolArguments ?? {}) === args,
    );
  });
  return {
    ...restored,
    messages: [...restored.messages, ...pendingLiveMessages].sort((left, right) => left.time.localeCompare(right.time)),
  };
}

function Shell() {
  const { t } = useT();
  const initialHash = hashState();
  const [db, setDb] = useState<Db | null>(null);
  const [activeWsId, setActiveWsId] = useState<string>("");
  const [workspaceToDelete, setWorkspaceToDelete] = useState<Workspace | null>(null);
  const [profileToDelete, setProfileToDelete] = useState<Profile | null>(null);
  const [convs, setConvs] = useState<Conversation[]>([]);
  const [activeConvId, setActiveConvId] = useState<string | null>(initialHash.conversationId);
  const restoredTokenConversations = useRef(new Set<string>());
  const [activeAgentId, setActiveAgentId] = useState("code-architect");
  const [view, setView] = useState<View>(initialHash.view);
  const [settingsTab, setSettingsTab] = useState<SettingsTab>(initialHash.settingsTab);
  const [tab, setTab] = useState<"chat" | "files">("chat");
  const [agentStatus, setAgentStatus] = useState<Record<string, AgentStatus>>({});
  const [busy, setBusy] = useState(false);
  const [switchingModel, setSwitchingModel] = useState(false);
  const [pendingModel, setPendingModel] = useState("");
  const [continuingErrorMessageId, setContinuingErrorMessageId] = useState<string | null>(null);
  const [pendingReq, setPendingReq] = useState<AgentRequest | null>(null);
  const [goal, setGoal] = useState<GoalState | null>(null);
  const [goals, setGoals] = useState<GoalState[]>([]);
  const [goalPopup, setGoalPopup] = useState<GoalState | null>(null);
  const [goalCollapsed, setGoalCollapsed] = useState(false);
  const [bgTasks, setBgTasks] = useState<BgTask[]>([]);

  useEffect(() => {
    const configureTextInputs = (root: ParentNode) => {
      root
        .querySelectorAll<HTMLInputElement>(
          'input[type="text"], input[type="search"], input[type="email"], input[type="url"], input[type="tel"], input[type="password"]',
        )
        .forEach((input) => {
          input.spellcheck = false;
          input.setAttribute("autocorrect", "off");
          input.setAttribute("autocapitalize", "none");
        });
    };
    configureTextInputs(document);
    const observer = new MutationObserver((records) => {
      records.forEach((record) =>
        record.addedNodes.forEach((node) => {
          if (node instanceof HTMLElement) configureTextInputs(node);
        }),
      );
    });
    observer.observe(document.body, { childList: true, subtree: true });
    return () => observer.disconnect();
  }, []);

  useEffect(
    () => subscribeConnection((connected) => setConnectionStatus(connected ? "connected" : "disconnected")),
    [],
  );
  useEffect(() => {
    if (!goal || goal.status !== "done") return;
    // Keep the completed state visible briefly, then close the transient
    // progress panel. The persisted goal remains available after reload.
    const timer = window.setTimeout(() => setGoal(null), 1200);
    return () => window.clearTimeout(timer);
  }, [goal]);
  const [tokens, setTokens] = useState(0);
  const [contextTokenOverrides, setContextTokenOverrides] = useState<Record<string, number>>({});
  const [temp, setTemp] = useState(0.7);
  const [topP, setTopP] = useState(0.9);
  const [sidebarOpen, setSidebarOpen] = useState(false);
  const [panelOpen, setPanelOpen] = useState(true);
  const [draft, setDraft] = useState<{ text: string; nonce: number } | null>(null);
  const [toasts, setToasts] = useState<Toast[]>([]);
  const [compressing, setCompressing] = useState(false);
  const [latencyMs, setLatencyMs] = useState<number | null>(null);
  const [requestsPerMinute, setRequestsPerMinute] = useState<number | null>(null);
  const [rpmLimit, setRPMLimit] = useState(0);
  const [connectionStatus, setConnectionStatus] = useState<"connected" | "disconnected">("disconnected");
  const [transferMode, setTransferMode] = useState<"import" | "export" | null>(null);
  const [cleanupOpen, setCleanupOpen] = useState(false);
  const [cleaningUp, setCleaningUp] = useState(false);
  const [transferSelection, setTransferSelection] = useState<Record<string, boolean>>(() =>
    Object.fromEntries(transferCategories.map((key) => [key, false])),
  );
  const [transferAvailable, setTransferAvailable] = useState<string[]>([...transferCategories]);
  const [transferContent, setTransferContent] = useState<string | null>(null);
  const importSettingsRef = useRef<HTMLInputElement | null>(null);
  const [profiles, setProfiles] = useState<Profile[]>([]);
  const [activeProfileId, setActiveProfileId] = useState<string>("");
  const [profileBusy, setProfileBusy] = useState(false);
  const [deleteConversationID, setDeleteConversationID] = useState<string | null>(null);
  const [clearHistoryOpen, setClearHistoryOpen] = useState(false);
  const sockRef = useRef<ReturnType<typeof connectChat> | null>(null);
  const lastViewRef = useRef<View | null>(null);

  useEffect(() => {
    document.title = "ARWOS AGENT";
  }, []);

  // Keep deep links stable across refreshes without requiring a router.
  useEffect(() => {
    const onHashChange = () => {
      const next = hashState();
      setView(next.view);
      setSettingsTab(next.settingsTab);
      if (next.view === "chat" && next.conversationId) setActiveConvId(next.conversationId);
    };
    window.addEventListener("hashchange", onHashChange);
    return () => window.removeEventListener("hashchange", onHashChange);
  }, []);

  useEffect(() => {
    // Keep the URL in sync even when chat has no conversation yet.
    // The previous guard skipped `#/chat`, leaving the old settings hash.
    writeHash(view, activeConvId, settingsTab);
  }, [view, activeConvId, settingsTab]);

  useEffect(() => {
    if (!activeConvId) {
      setTokens(0);
      setRequestsPerMinute(null);
      setRPMLimit(0);
      return;
    }
    if (restoredTokenConversations.current.has(activeConvId)) return;
    const conversation = convs.find((item) => item.id === activeConvId);
    if (!conversation) return;
    restoredTokenConversations.current.add(activeConvId);
    const history = conversation?.messages ?? [];
    const hasStoredUsage = history.some((message) => message.tokens !== undefined);
    setTokens(
      history.reduce(
        (total, message) => total + (hasStoredUsage ? (message.tokens ?? 0) : (message.contextSize ?? 0)),
        0,
      ),
    );
  }, [activeConvId, convs]);

  const notify: Notify = useCallback((kind, msg) => {
    const toast: Toast = { id: uid(), kind, msg };
    setToasts((p) => [...p, toast]);
    setTimeout(() => setToasts((p) => p.filter((x) => x.id !== toast.id)), 3600);
  }, []);

  const patch = useCallback((p: Partial<Db>) => setDb((d) => (d ? { ...d, ...p } : d)), []);

  const refreshSection = useCallback(
    async (section: View) => {
      try {
        switch (section) {
          case "skills":
            patch({ skills: (await wsRequest<Db["skills"]>(72)) ?? [] });
            break;
          case "providers":
            patch({ providers: await wsRequest(84) });
            break;
          case "mcp":
            patch({ mcpServers: await wsRequest(76) });
            break;
          case "network":
            patch({ proxies: await wsRequest(94) });
            break;
          case "systemInfo":
            break;
          case "agents":
            patch({ agents: await wsRequest(17) });
            break;
          case "presets":
            patch({ presets: await wsRequest(21) });
            break;
          case "kb": {
            const result = await wsRequest<{ docs: Db["kbDocs"]; quotaBytes: number }>(34);
            patch({ kbDocs: Array.isArray(result?.docs) ? result.docs : [], kbQuotaBytes: result?.quotaBytes ?? 0 });
            break;
          }
        }
      } catch (error) {
        notify("err", error instanceof Error ? error.message : "Failed to refresh section");
      }
    },
    [notify, patch],
  );

  // Settings screens are intentionally refreshed on every entry. This keeps
  // changes made in another browser tab or by another profile session visible.
  useEffect(() => {
    if (lastViewRef.current === view) return;
    lastViewRef.current = view;
    if (!db || view === "chat" || view === "settings") return;
    void refreshSection(view);
  }, [db, refreshSection, view]);

  /* ------------------------------------------------------------- loading */

  const loadConvs = useCallback(async (wsId: string) => {
    const metas = await wsRequest<(Omit<Conversation, "messages"> & { count?: number })[]>(41, { workspaceId: wsId });
    const full = (await Promise.all(metas.map((m) => wsRequest<Conversation | null>(42, { id: m.id })))).filter(
      (x): x is Conversation => x !== null,
    );
    setConvs(full);
    const linked = hashState().conversationId;
    const first = full.find((conversation) => conversation.id === linked) ?? full[0] ?? null;
    setActiveConvId(first?.id ?? null);
    if (first) setActiveAgentId(first.agentId);
    return full;
  }, []);

  const hydrateProfile = useCallback(
    async (data: Awaited<ReturnType<typeof loadAll>>) => {
      setDb(data);
      setProfiles(data.profiles);
      setActiveProfileId(data.activeProfileId);
      const profile = data.profiles.find((item) => item.id === data.activeProfileId);
      setTemp(profile?.temperature ?? 0.7);
      setTopP(profile?.topP ?? 0.9);
      // при смене профиля сбрасываем выбор диалога/агента
      setActiveConvId(null);
      setPendingReq(null);
      setGoal(null);
      setBgTasks([]);
      const wsId = data.workspaces[0]?.id ?? null;
      if (wsId) {
        setActiveWsId(wsId);
        await loadConvs(wsId);
      } else {
        setActiveWsId("");
        setConvs([]);
      }
    },
    [loadConvs],
  );

  useEffect(() => {
    void (async () => {
      const data = await loadAll();
      await hydrateProfile(data);
    })();
  }, [hydrateProfile]);

  /* ------------------------------------------------------- ws connection */

  const onFrame = useCallback((fr: Record<string, any>) => {
    if (fr.type === "open") {
      setConnectionStatus("connected");
      return;
    }
    if (fr.type === "connection") {
      setConnectionStatus(fr.status === "connected" ? "connected" : "disconnected");
      return;
    }
    switch (fr.type) {
      case "status":
        setAgentStatus((s) => ({ ...s, [fr.agentId as string]: fr.status as AgentStatus }));
        break;
      case "message.deleted":
        setConvs((cs) =>
          cs.map((c) => (c.id === fr.convId ? { ...c, messages: c.messages.filter((m) => m.id !== fr.messageId) } : c)),
        );
        break;
      case "tool_call":
        setConvs((cs) =>
          cs.map((c) => {
            if (c.id !== fr.convId) return c;
            const running = fr.phase === "start";
            const index = c.messages.findIndex((m) => m.role === "tool" && m.toolRunning && m.toolName === fr.name);
            const tool: ChatMessage = {
              id: index >= 0 ? c.messages[index].id : uid(),
              role: "tool",
              time: nowTime(),
              parts: [],
              toolName: String(fr.name ?? ""),
              toolArguments: (fr.arguments as Record<string, unknown>) ?? {},
              toolResult: running ? undefined : String(fr.result ?? ""),
              toolError: fr.error ? String(fr.error) : undefined,
              toolRunning: running,
              model: String(fr.model ?? ""),
              provider: String(fr.provider ?? ""),
            };
            if (index >= 0) {
              const messages = [...c.messages];
              messages[index] = tool;
              return { ...c, messages };
            }
            const messages = [...c.messages];
            if (running) {
              const agentIndex = lastAgentMessageIndex(messages);
              if (isExactToolCallText(messages[agentIndex], String(fr.name ?? ""))) {
                messages[agentIndex] = {
                  ...messages[agentIndex],
                  parts: messages[agentIndex].parts.filter((part) => part.type === "reasoning"),
                };
              }
            }
            return { ...c, messages: [...messages, tool] };
          }),
        );
        break;
      case "msg.start":
        setConvs((cs) =>
          cs.map((c) =>
            c.id !== fr.convId
              ? c
              : {
                  ...c,
                  messages: [
                    ...c.messages,
                    {
                      id: uid(),
                      role: "agent",
                      time: nowTime(),
                      parts: [],
                      model: String(fr.model ?? ""),
                      provider: String(fr.provider ?? ""),
                    } as ChatMessage,
                  ],
                },
          ),
        );
        break;
      case "reasoning":
        setConvs((cs) =>
          cs.map((c) => {
            const text = String(fr.text ?? "");
            if (c.id !== fr.convId || c.messages.length === 0 || !text) return c;
            const [messages, agentIndex] = ensureStreamingAgentMessage([...c.messages], fr.model, fr.provider);
            const last = { ...messages[agentIndex] };
            const parts = [...last.parts];
            const previous = parts[parts.length - 1];
            if (previous?.type === "reasoning") {
              parts[parts.length - 1] = { ...previous, content: previous.content + text };
            } else {
              parts.push({ type: "reasoning", content: text });
            }
            last.parts = parts;
            messages[agentIndex] = last;
            return { ...c, messages };
          }),
        );
        break;
      case "chunk":
        setConvs((cs) =>
          cs.map((c) => {
            if (c.id !== fr.convId || c.messages.length === 0) return c;
            const [msgs, agentIndex] = ensureStreamingAgentMessage([...c.messages], fr.model, fr.provider);
            const last = { ...msgs[agentIndex] };
            const parts = [...last.parts];
            const lp = parts[parts.length - 1];
            if (lp && lp.type === "text")
              parts[parts.length - 1] = { ...lp, content: lp.content + (fr.text as string) };
            else parts.push({ type: "text", content: fr.text as string });
            last.parts = parts;
            msgs[agentIndex] = last;
            return { ...c, messages: msgs };
          }),
        );
        break;
      case "code":
        setConvs((cs) =>
          cs.map((c) => {
            if (c.id !== fr.convId) return c;
            const [messages, agentIndex] = ensureStreamingAgentMessage([...c.messages], fr.model, fr.provider);
            return {
              ...c,
              messages: messages.map((m, i) =>
                i === agentIndex
                  ? {
                      ...m,
                      parts: [
                        ...m.parts,
                        { type: "code" as const, content: fr.code as string, lang: fr.lang as string },
                      ],
                    }
                  : m,
              ),
            };
          }),
        );
        break;
      case "request":
        setPendingReq(fr.request as AgentRequest);
        break;
      case "goal":
        if (fr.convId === activeConvId) {
          const nextGoal = fr.goal as GoalState;
          setGoal(nextGoal);
          setGoals((items) => [...items.filter((item) => item.id !== nextGoal.id), nextGoal]);
        }
        break;
      case "goal.update":
        if (fr.convId === activeConvId)
          setGoal((g) =>
            g
              ? {
                  ...g,
                  tasks: g.tasks.map((tk) =>
                    tk.id === fr.taskId ? { ...tk, status: fr.status as GoalState["tasks"][number]["status"] } : tk,
                  ),
                }
              : g,
          );
        break;
      case "goal.clear":
        if (fr.convId === activeConvId) setGoal(null);
        break;
      case "bg":
        setBgTasks(fr.tasks as BgTask[]);
        break;
      case "bg.update":
        setBgTasks((ts) => ts.map((tk) => (tk.id === fr.id ? { ...tk, progress: fr.progress as number } : tk)));
        break;
      case "bg.clear":
        setBgTasks([]);
        break;
      case "done":
        setBusy(false);
        setContinuingErrorMessageId(null);
        if (typeof fr.content === "string" && fr.content) {
          setConvs((cs) =>
            cs.map((c) => {
              if (c.id !== fr.convId) return c;
              const [messages, agentIndex] = ensureStreamingAgentMessage([...c.messages], fr.model, fr.provider);
              const message = { ...messages[agentIndex] };
              const reasoning = message.parts.filter((part) => part.type === "reasoning");
              message.parts = [...reasoning, { type: "text", content: fr.content }];
              messages[agentIndex] = message;
              return { ...c, messages };
            }),
          );
        }
        if (Number.isFinite(Number(fr.latency))) setLatencyMs(Number(fr.latency));
        if (Number.isFinite(Number(fr.rpm))) setRequestsPerMinute(Number(fr.rpm));
        if (Number.isFinite(Number(fr.rpmLimit))) setRPMLimit(Number(fr.rpmLimit));
        {
          const conversation = convs.find((c) => c.id === fr.convId);
          const userMessages = conversation?.messages.filter((message) => message.role === "user").length ?? 0;
          if (userMessages > 0 && userMessages % 5 === 0) {
            void wsRequest<{ title: string; summary: string; topics: string[] }>(43, {
              id: fr.convId,
              model: activeAgent.memoryModel,
            })
              .then((memory) =>
                setConvs((cs) =>
                  cs.map((c) =>
                    c.id === fr.convId
                      ? { ...c, memory: { title: memory.title, summary: memory.summary, topics: memory.topics ?? [] } }
                      : c,
                  ),
                ),
              )
              .catch(() => undefined);
          }
        }
        const contextTokens = Number(fr.contextTokens);
        const hasContextTokens = Number.isFinite(contextTokens) && contextTokens > 0;
        setTokens((total) => total + (Number(fr.tokens) || 0));
        if (hasContextTokens)
          setContextTokenOverrides((values) => ({ ...values, [String(fr.convId ?? "")]: contextTokens }));
        setConvs((cs) =>
          cs.map((c) => {
            if (c.id !== fr.convId || c.messages.length === 0) return c;
            const messages = [...c.messages];
            const assistantIndex = lastAgentMessageIndex(messages);
            if (assistantIndex >= 0) {
              messages[assistantIndex] = {
                ...messages[assistantIndex],
                ...(hasContextTokens ? { contextSize: contextTokens } : {}),
                tokens: Number(fr.tokens) || 0,
              };
            }
            return { ...c, messages };
          }),
        );
        break;
      case "error":
        setBusy(false);
        setContinuingErrorMessageId(null);
        setConvs((cs) =>
          cs.map((c) => {
            if (c.id !== fr.convId) return c;
            const message = String(fr.message ?? "Unknown error");
            const msgs = [...c.messages];
            const last = msgs[msgs.length - 1];
            // chat.send can report the same backend failure both as an RPC
            // error and as a WebSocket event. Do not add it twice.
            if (last?.role === "agent" && last.parts.map((part) => part.content).join("") === message) {
              const messages = [...c.messages];
              messages[messages.length - 1] = { ...last, id: String(fr.message_id ?? last.id), retryableError: true };
              return { ...c, messages };
            }
            if (last?.role === "agent" && last.parts.length === 0) {
              msgs[msgs.length - 1] = {
                ...last,
                id: String(fr.message_id ?? last.id),
                model: String(fr.model ?? last.model ?? ""),
                provider: String(fr.provider ?? last.provider ?? ""),
                parts: [{ type: "text", content: message }],
                retryableError: true,
              };
            } else {
              msgs.push({
                id: String(fr.message_id ?? uid()),
                role: "agent",
                time: nowTime(),
                model: String(fr.model ?? ""),
                provider: String(fr.provider ?? ""),
                parts: [{ type: "text", content: message }],
                retryableError: true,
              });
            }
            return { ...c, messages: msgs, updatedAt: nowTime() };
          }),
        );
        break;
    }
  }, []);

  useEffect(() => {
    if (!activeConvId) return;
    setBusy(false);
    setContinuingErrorMessageId(null);
    setPendingReq(null);
    setGoal(null);
    setGoalPopup(null);
    setGoalCollapsed(false);
    setGoals([]);
    setBgTasks([]);
    const sock = connectChat(activeConvId, activeAgentId, onFrame);
    sockRef.current = sock;
    let disposed = false;
    let syncTimer: number | undefined;
    const syncRunningConversation = async () => {
      try {
        const [conversation, state] = await Promise.all([
          wsRequest<Conversation | null>(WS_EVENT_CONVERSATION_GET, { id: activeConvId }),
          wsRequest<{ running: boolean; request?: AgentRequest }>(WS_EVENT_CONVERSATION_RUN_STATUS, {
            id: activeConvId,
          }),
        ]);
        if (disposed) return;
        if (conversation) {
          setConvs((items) =>
            items.map((item) => (item.id === conversation.id ? mergeConversationHistory(item, conversation) : item)),
          );
          setGoal(conversation.goal ?? null);
          setGoals(conversation.goals ?? (conversation.goal ? [conversation.goal] : []));
        }
        // The live request is normally restored from the server-side request
        // registry. The persisted goal approval is a second source of truth,
        // so a page reload can still render the popup if the registry response
        // arrives a little later.
        const persistedApproval = conversation?.goal?.approval
          ? { ...conversation.goal.approval, reqId: conversation.goal.approval.id }
          : null;
        setBusy(Boolean(state.running) || Boolean(persistedApproval));
        setPendingReq(state.request ?? persistedApproval);
        if (!state.running && syncTimer !== undefined) {
          window.clearInterval(syncTimer);
          syncTimer = undefined;
        }
      } catch {
        // The shared transport reconnects automatically. Keep the existing UI
        // state and retry on the next poll rather than hiding active work.
      }
    };
    void syncRunningConversation().then(() => {
      if (!disposed) syncTimer = window.setInterval(() => void syncRunningConversation(), 1000);
    });
    return () => {
      disposed = true;
      if (syncTimer !== undefined) window.clearInterval(syncTimer);
      sock.close();
    };
    // переподключаемся только при смене диалога
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [activeConvId]);

  /* ------------------------------------------------------------- actions */

  const deleteMessage = async (messageId: string) => {
    if (!activeConvId) return;
    await wsRequest(110, { id: activeConvId, messageId });
    setConvs((cs) =>
      cs.map((c) => (c.id === activeConvId ? { ...c, messages: c.messages.filter((m) => m.id !== messageId) } : c)),
    );
  };

  const sendMessage = async (text: string, files: string[], skillIds: string[], asGoal = false) => {
    if (!activeWsId || busy) return;
    setBusy(true);
    let convId = activeConvId;
    // A workspace can intentionally start with no conversations. The first
    // prompt must create one instead of being silently discarded.
    if (!convId) {
      try {
        const created = await wsRequest<Omit<Conversation, "messages">>(WS_EVENT_CONVERSATION_CREATE, {
          workspaceId: activeWsId,
          agentId: activeAgentId,
        });
        convId = created.id;
        const conversation: Conversation = { ...created, messages: [] };
        setConvs((items) => [conversation, ...items]);
        setActiveConvId(convId);
        if (pendingModel) {
          await wsRequest(103, { id: convId, mode: "model", model: pendingModel });
          setConvs((items) =>
            items.map((item) => (item.id === convId ? { ...item, activeModel: pendingModel } : item)),
          );
        }
      } catch (error) {
        setBusy(false);
        notify("err", error instanceof Error ? error.message : String(error));
        return;
      }
    }
    const skillNames = skillIds.map((id) => db?.skills.find((s) => s.id === id)?.name ?? "").filter(Boolean);
    const msg: ChatMessage = {
      id: uid(),
      role: "user",
      time: nowTime(),
      parts: [{ type: "text", content: text }],
      attachments: files.length ? files : undefined,
      skills: skillNames.length ? skillNames : undefined,
    };
    setConvs((cs) =>
      cs.map((c) => (c.id === convId ? { ...c, messages: [...c.messages, msg], updatedAt: msg.time } : c)),
    );
    setContextTokenOverrides((values) => {
      const next = { ...values };
      delete next[convId];
      return next;
    });
    const frame = {
      type: "user.message",
      convId,
      agentId: activeAgentId,
      workspaceId: activeWsId,
      text,
      files,
      skillNames,
      asGoal,
    };
    if (sockRef.current) sockRef.current.send(frame);
    else sendChat(frame);
  };

  const continueConversation = (errorMessageId: string) => {
    if (!activeConvId || !activeWsId || busy) return;
    setConvs((cs) =>
      cs.map((conversation) =>
        conversation.id !== activeConvId
          ? conversation
          : {
              ...conversation,
              messages: conversation.messages.map((message) =>
                message.id === errorMessageId ? { ...message, retryableError: false } : message,
              ),
            },
      ),
    );
    setContinuingErrorMessageId(errorMessageId);
    setBusy(true);
    sockRef.current?.send({
      type: "user.continue",
      convId: activeConvId,
      agentId: activeAgentId,
      workspaceId: activeWsId,
      errorMessageId,
    });
  };

  const switchAndContinueConversation = (errorMessageId: string) => {
    if (!activeConvId || !activeWsId || busy || activeAgent.mainModels.length < 2) return;
    const current = activeConv?.activeModel || activeAgent.model;
    const index = activeAgent.mainModels.indexOf(current);
    const next = activeAgent.mainModels[(index + 1 + activeAgent.mainModels.length) % activeAgent.mainModels.length];
    setConvs((cs) =>
      cs.map((conversation) =>
        conversation.id !== activeConvId
          ? conversation
          : {
              ...conversation,
              messages: conversation.messages.map((message) =>
                message.id === errorMessageId ? { ...message, retryableError: false } : message,
              ),
              activeModel: next,
            },
      ),
    );
    setContinuingErrorMessageId(errorMessageId);
    setBusy(true);
    sockRef.current?.send({
      type: "user.continue",
      convId: activeConvId,
      agentId: activeAgentId,
      workspaceId: activeWsId,
      errorMessageId,
      model: next,
    });
  };

  const stopConversation = () => {
    if (!activeConvId || !busy) return;
    sockRef.current?.send({ type: "user.stop", convId: activeConvId });
  };

  const resumeGoal = () => {
    if (!activeConvId || busy) return;
    setBusy(true);
    sockRef.current?.send({
      type: "user.continue",
      convId: activeConvId,
      agentId: activeAgentId,
      workspaceId: activeWsId,
    });
  };
  const setGoalLifecycle = (action: "goal.pause" | "goal.stop") => {
    if (!activeConvId) return;
    sockRef.current?.send({ type: action, convId: activeConvId });
    // A lifecycle action closes the floating card immediately. The persisted
    // goal remains visible in the sidebar and can be opened again there.
    setGoalPopup(null);
    setGoalCollapsed(true);
  };

  const respondRequest = (reqId: string, value: boolean | string | string[]) => {
    sockRef.current?.send({ type: "user.response", convId: activeConvId, reqId, value });
    setPendingReq(null);
  };

  const switchConversationModel = async (model: string) => {
    if (!activeConvId) {
      setPendingModel(model);
      return;
    }
    if (busy || switchingModel) return;
    setSwitchingModel(true);
    try {
      const conversation = await wsRequest<Conversation>(103, {
        id: activeConvId,
        mode: model ? "model" : "main",
        model,
      });
      setConvs((cs) =>
        cs.map((item) =>
          item.id === activeConvId ? { ...item, activeModel: conversation.activeModel || undefined } : item,
        ),
      );
      setPendingModel("");
    } catch (error) {
      notify("err", error instanceof Error ? error.message : String(error));
    } finally {
      setSwitchingModel(false);
    }
  };

  const selectAgent = async (id: string) => {
    setActiveAgentId(id);
    setView("chat");
    setTab("chat");
    setSidebarOpen(false);
    const existing = convs.find((c) => c.agentId === id);
    if (existing) {
      setActiveConvId(existing.id);
    } else if (activeWsId) {
      const conv = {
        ...(await wsRequest<Omit<Conversation, "messages">>(WS_EVENT_CONVERSATION_CREATE, {
          workspaceId: activeWsId,
          agentId: id,
        })),
        messages: [],
      };
      setConvs((cs) => [conv, ...cs]);
      setActiveConvId(conv.id);
    }
  };

  const newConversation = async () => {
    if (!activeWsId) return;
    const conv = {
      ...(await wsRequest<Omit<Conversation, "messages">>(WS_EVENT_CONVERSATION_CREATE, {
        workspaceId: activeWsId,
        agentId: activeAgentId,
      })),
      messages: [],
    };
    setConvs((cs) => [conv, ...cs]);
    setActiveConvId(conv.id);
    setView("chat");
    setTab("chat");
    setSidebarOpen(false);
    notify("ok", t("toasts.convCreated"));
  };

  const openConversation = (id: string) => {
    const c = convs.find((x) => x.id === id);
    setActiveConvId(id);
    if (c) setActiveAgentId(c.agentId);
    setView("chat");
    setTab("chat");
    setSidebarOpen(false);
  };

  const deleteConversation = async (id: string) => {
    const next = convs.filter((c) => c.id !== id);
    await wsRequest(54, { id });
    setConvs(next);
    if (activeConvId === id) {
      setActiveConvId(next[0]?.id ?? null);
      if (next[0]) setActiveAgentId(next[0].agentId);
    }
    notify("info", t("toasts.convDeleted"));
  };

  const switchWorkspace = async (id: string) => {
    setActiveWsId(id);
    setView("chat");
    setSidebarOpen(false);
    await loadConvs(id);
  };

  const createWorkspace = async (workspace: Workspace) => {
    // Folder selection has already created the workspace on the backend. A
    // second workspace.create request used the basename as an ID and overwrote
    // another open directory with the same name.
    patch({
      workspaces: db?.workspaces.some((item) => item.id === workspace.id)
        ? db.workspaces
        : [...(db?.workspaces ?? []), workspace],
    });
    notify("ok", t("toasts.wsConnected", { name: workspace.name }));
    await switchWorkspace(workspace.id);
  };

  const deleteWorkspace = async (id: string) => {
    try {
      await wsRequest(62, { id });
      const remaining = (db?.workspaces ?? []).filter((workspace) => workspace.id !== id);
      patch({ workspaces: remaining });
      if (activeWsId === id) {
        setActiveWsId(remaining[0]?.id ?? null);
        setActiveConvId(null);
        if (remaining[0]) await loadConvs(remaining[0].id);
        else setConvs([]);
      }
      notify("info", t("toasts.wsDeleted"));
    } catch (error) {
      notify("err", error instanceof Error ? error.message : String(error));
    }
  };

  /* ------------------------------------------------------------ profiles */

  const switchProfile = async (id: string) => {
    if (id === activeProfileId || profileBusy) return;
    setProfileBusy(true);
    await wsRequest(15, { id });
    await hydrateProfile(await loadAll());
    setProfileBusy(false);
    const prof = profiles.find((p) => p.id === id);
    notify("ok", t("toasts.profileSwitched", { name: prof?.name ?? "" }));
  };

  const createProfile = async (f: { name: string; role: string; accent: AccentKey }) => {
    setProfileBusy(true);
    await wsRequest(13, f);
    await hydrateProfile(await loadAll());
    setProfileBusy(false);
    notify("ok", t("toasts.profileCreated", { name: f.name }));
  };

  const deleteProfile = async (id: string) => {
    setProfileBusy(true);
    try {
      await wsRequest(16, { id });
      await hydrateProfile(await loadAll());
      notify("info", t("toasts.profileDeleted"));
    } catch (error) {
      notify("err", error instanceof Error ? error.message : String(error));
    } finally {
      setProfileBusy(false);
    }
  };

  const cleanupOrphans = async () => {
    setCleaningUp(true);
    try {
      const result = await wsRequest<Record<string, number>>(WS_EVENT_SETTINGS_CLEANUP);
      notify("ok", t("settings.cleanupDone", { n: Object.values(result).reduce((sum, value) => sum + value, 0) }));
    } catch (error) {
      notify("err", error instanceof Error ? error.message : String(error));
    } finally {
      setCleaningUp(false);
      setCleanupOpen(false);
    }
  };

  const updateProfile = async (id: string, p: Partial<Profile>) => {
    const current = profiles.find((profile) => profile.id === id);
    const updated = await wsRequest<Profile>(14, { id, patch: { ...current, ...p, id } });
    setProfiles((ps) => ps.map((x) => (x.id === id ? updated : x)));
    notify("ok", t("toasts.profileUpdated"));
  };

  const clearHistory = async () => {
    if (!activeConvId) return;
    await wsRequest(55, { id: activeConvId });
    setConvs((cs) =>
      cs.map((c) => (c.id === activeConvId ? { ...c, messages: [], memory: undefined, goal: null } : c)),
    );
    setGoal(null);
    setGoals([]);
    setContextTokenOverrides((values) => ({ ...values, [activeConvId]: 0 }));
    setTokens(0);
    notify("info", t("toasts.historyCleared"));
  };

  const exportTranscript = () => {
    const conv = convs.find((c) => c.id === activeConvId);
    if (!conv) return;
    const agent = agents.find((a) => a.id === conv.agentId);
    const md = [
      `# ${conv.title}`,
      `> ${agent?.name ?? conv.agentId} · ${agent?.model ?? "—"} · ${new Date().toLocaleString()}`,
      "",
      ...conv.messages.flatMap((m) => [
        `## ${m.role === "user" ? "User" : (agent?.name ?? "Agent")} · ${m.time}`,
        ...m.parts.map((p) => (p.type === "code" ? `\`\`\`${p.lang ?? ""}\n${p.content}\n\`\`\`` : p.content)),
        "",
      ]),
    ].join("\n");
    const url = URL.createObjectURL(new Blob([md], { type: "text/markdown" }));
    const a = document.createElement("a");
    a.href = url;
    a.download = `${conv.title.replace(/[^a-zа-яё0-9]+/gi, "-").toLowerCase()}.md`;
    a.click();
    URL.revokeObjectURL(url);
    notify("ok", t("toasts.exported"));
  };

  const compressContext = async () => {
    const convId = activeConvId;
    if (!convId || compressing) return;
    setCompressing(true);
    try {
      const res = await wsRequest<{
        before: number;
        after: number;
        title: string;
        summary: string;
        topics: string[];
        tokens: number;
      }>(53, {
        convId,
        model: activeAgent.compactionModel,
      });
      setConvs((cs) =>
        cs.map((c) =>
          c.id === convId
            ? {
                ...c,
                memory: { title: res.title, summary: res.summary, topics: res.topics ?? [] },
                messages: [
                  ...c.messages,
                  {
                    id: uid(),
                    role: "agent" as const,
                    time: nowTime(),
                    compact: true,
                    contextSize: res.after,
                    tokens: res.tokens,
                    parts: [{ type: "text" as const, content: res.summary }],
                  },
                ],
              }
            : c,
        ),
      );
      setContextTokenOverrides((values) => ({ ...values, [convId]: res.after }));
      setTokens((total) => total + (Number(res.tokens) || 0));
      notify(
        "ok",
        t("panel.compactDone", {
          before: res.before.toLocaleString("ru-RU"),
          after: res.after.toLocaleString("ru-RU"),
        }),
      );
    } catch {
      notify("err", "conversation.compact: RPC error");
    }
    setCompressing(false);
  };

  /* -------------------------------------------------------------- derived */

  const agents: LiveAgent[] = useMemo(
    () =>
      (db?.agents ?? []).map((e) => ({
        id: e.id,
        name: e.name,
        description: e.description,
        icon: SKILL_ICONS[e.iconKey] ?? SKILL_ICONS.bot,
        accent: e.accent,
        model: e.mainModels?.[0] ?? "",
        mainModels: e.mainModels ?? [],
        compactionModel: e.compactionModel,
        compactionLevel: e.compactionLevel ?? "balanced",
        memoryModel: e.memoryModel ?? "",
        systemPrompt: e.systemPrompt,
        skillGroupIds: e.skillGroupIds ?? [],
        mcpIds: e.mcpIds,
        topics: (db?.presets ?? []).map((p) => p.title).slice(0, 3),
        status: agentStatus[e.id] ?? "idle",
      })),
    [db, agentStatus],
  );

  // если активного агента удалили — переключаемся на первого оставшегося
  useEffect(() => {
    if (db && agents.length > 0 && !agents.some((a) => a.id === activeAgentId)) {
      setActiveAgentId(agents[0].id);
    }
  }, [db, agents, activeAgentId]);

  const activeAgent = agents.find((a) => a.id === activeAgentId) ??
    agents[0] ?? {
      id: "empty-agent",
      name: "Agent",
      description: "",
      icon: SKILL_ICONS.bot,
      accent: "indigo" as AccentKey,
      model: "",
      mainModels: [],
      compactionModel: "",
      compactionLevel: "balanced",
      memoryModel: "",
      systemPrompt: "",
      skillGroupIds: [],
      mcpIds: [],
      topics: [],
      status: "idle" as AgentStatus,
    };
  const activeConv = convs.find((c) => c.id === activeConvId) ?? null;
  const activeProfile = profiles.find((profile) => profile.id === activeProfileId) ?? null;
  const activeModel = activeConv?.activeModel || pendingModel || activeAgent.model;
  const activeDisplayAgent =
    activeModel && activeModel !== activeAgent.model ? { ...activeAgent, model: activeModel } : activeAgent;
  const messages = activeConv?.messages ?? [];
  const activeWsMeta = db?.workspaces.find((w) => w.id === activeWsId) as
    (Workspace & { fileCount?: number }) | undefined;
  const accentRgb = ACCENTS[activeAgent.accent].rgb;

  const agentPresets: Preset[] = useMemo(
    () => (db?.presets ?? []).filter((p) => !p.agentId || p.agentId === activeAgent.id),
    [db, activeAgent.id],
  );
  const exportSettings = async (include: string[]) => {
    try {
      const result = await wsRequest<{ filename: string; content: string }>(100, { include });
      const blob = new Blob([result.content], { type: "application/json" });
      const url = URL.createObjectURL(blob);
      const link = document.createElement("a");
      link.href = url;
      link.download = result.filename;
      link.click();
      URL.revokeObjectURL(url);
      notify("ok", t("settings.exported"));
    } catch (error) {
      notify("err", error instanceof Error ? error.message : String(error));
    }
  };
  const importSettings = async (content: string, include: string[]) => {
    try {
      const result = await wsRequest<{ updated: number; skipped: number }>(101, { content, include });
      setTransferMode(null);
      setTransferContent(null);
      await loadAll().then((data) => setDb(data));
      notify("ok", t("settings.imported", result));
    } catch (error) {
      notify("err", error instanceof Error ? error.message : String(error));
    }
  };
  const transferKeys = Object.keys(transferSelection).filter((key) => transferSelection[key]);

  const calculatedContextTokens = useMemo(() => {
    const hasStoredUsage = messages.some((message) => message.tokens !== undefined);
    if (!hasStoredUsage) {
      // JSONL created before per-turn usage persistence only has the old
      // per-message approximation. Keep it readable until new turns supply
      // an authoritative provider snapshot.
      const compactIndex = messages.reduce((last, message, index) => (message.compact ? index : last), -1);
      return messages
        .slice(compactIndex < 0 ? 0 : compactIndex)
        .reduce((total, message) => total + (message.contextSize ?? 0), 0);
    }
    // Context is a snapshot of the latest provider prompt plus its response,
    // never a sum of historical prompts (which would count the same history
    // repeatedly during ReAct iterations).
    for (let index = messages.length - 1; index >= 0; index--) {
      const contextSize = messages[index].contextSize;
      if (contextSize !== undefined) return contextSize;
    }
    return Math.round(
      messages.reduce(
        (total, message) => total + message.parts.reduce((size, part) => size + part.content.length, 0),
        0,
      ) / 3.5,
    );
  }, [messages]);
  const contextTokens = contextTokenOverrides[activeConvId ?? ""] ?? calculatedContextTokens;
  // A provider may not expose its context limit. Keep that state unknown
  // instead of inventing a model-specific default; the panel renders it as ∞.
  const [contextWindow, setContextWindow] = useState<number | null>(null);
  useEffect(() => {
    setContextWindow(null);
    const at = activeDisplayAgent.model.lastIndexOf("@");
    const model = at > 0 ? activeDisplayAgent.model.slice(0, at) : activeDisplayAgent.model;
    const providerName = at > 0 ? activeDisplayAgent.model.slice(at + 1) : "";
    const provider =
      db?.providers.find((item) => item.name === providerName) ??
      db?.providers.find((item) => item.models.includes(model));
    if (!provider) {
      return;
    }
    void wsRequest<{ contextWindow: number }>(88, {
      id: provider.id,
      kind: provider.kind,
      baseUrl: provider.baseUrl,
      model,
    })
      .then((result) => {
        if (result.contextWindow > 0) setContextWindow(result.contextWindow);
      })
      .catch(() => undefined);
  }, [activeDisplayAgent.model, db]);

  /* --------------------------------------------------------------- render */

  if (!db) {
    return (
      <div className="grid h-full place-items-center bg-abyss-900">
        <div className="text-center">
          <motion.span
            animate={{ opacity: [1, 0.4, 1], scale: [1, 0.94, 1] }}
            transition={{ repeat: Infinity, duration: 1.6 }}
            className="mx-auto grid h-14 w-14 place-items-center rounded-2xl bg-gradient-to-br from-indigo-500/25 to-cyan-400/15 shadow-[inset_0_0_0_1px_rgba(129,140,248,0.35),0_0_40px_-8px_rgba(129,140,248,0.5)]"
          >
            <Hexagon size={24} className="text-indigo-300" />
          </motion.span>
          <p className="mt-4 font-display text-sm font-semibold text-slate-200">{t("brand.name")}</p>
          <p className="mt-1 font-mono text-[10px] tracking-[0.2em] text-slate-600">{t("brand.loadingSub")}</p>
        </div>
      </div>
    );
  }

  const TABS: { k: "chat" | "files"; label: string; icon: typeof MessageSquare }[] = [
    { k: "chat", label: t("chat.tabChat"), icon: MessageSquare },
    { k: "files", label: t("chat.tabFiles"), icon: FolderOpen },
  ];

  return (
    <div className="relative flex h-full overflow-hidden bg-abyss-900 text-slate-300">
      {/* ambient background */}
      <div className="pointer-events-none absolute inset-0 z-0">
        <div className="bg-grid absolute inset-0" />
        <div className="animate-float-slow absolute -left-32 -top-32 h-[480px] w-[480px] rounded-full bg-indigo-600/[0.13] blur-[130px]" />
        <div className="animate-float-slower absolute -bottom-40 -right-24 h-[420px] w-[420px] rounded-full bg-cyan-500/[0.09] blur-[120px]" />
        <div className="absolute left-1/2 top-1/3 h-[300px] w-[300px] rounded-full bg-violet-600/[0.07] blur-[110px]" />
      </div>

      <Sidebar
        view={view}
        version={db.version}
        workspaces={db.workspaces}
        activeWsId={activeWsId}
        conversations={convs}
        agents={agents}
        activeAgentId={activeAgent.id}
        activeConvId={activeConvId}
        counts={{
          skills: db.skills.length,
          providers: db.providers.length,
          mcp: db.mcpServers.length,
          presets: db.presets.length,
          kb: db.kbDocs.length,
          network: db.proxies.length,
          memory: db.longTermNotes.length + db.topicMemories.length,
        }}
        agentStatus={agentStatus}
        onWorkspace={(id) => void switchWorkspace(id)}
        onWorkspaceCreate={(f) => void createWorkspace(f)}
        onWorkspaceDelete={(id) => setWorkspaceToDelete(db.workspaces.find((workspace) => workspace.id === id) ?? null)}
        onAgent={(id) => void selectAgent(id)}
        onNewConversation={() => void newConversation()}
        onConversation={openConversation}
        onConversationDelete={setDeleteConversationID}
        onNavigate={(v) => {
          setView(v);
          setSidebarOpen(false);
          if (v !== "chat" && v !== "settings") setSettingsTab(v);
          writeHash(v, v === "chat" ? activeConvId : null, v === "settings" ? settingsTab : (v as SettingsTab));
        }}
        profiles={profiles}
        activeProfileId={activeProfileId}
        onProfileSwitch={(id) => void switchProfile(id)}
        onProfileCreate={(f) => void createProfile(f)}
        onProfileDelete={(id) => setProfileToDelete(profiles.find((profile) => profile.id === id) ?? null)}
        onProfileUpdate={(id, p) => void updateProfile(id, p)}
        open={sidebarOpen}
        onClose={() => setSidebarOpen(false)}
        connectionStatus={connectionStatus}
      />
      {workspaceToDelete && (
        <SModal
          title={t("sidebar.deleteWorkspaceTitle")}
          subtitle={workspaceToDelete.name}
          onClose={() => setWorkspaceToDelete(null)}
          footer={
            <>
              <SBtn onClick={() => setWorkspaceToDelete(null)}>{t("common.cancel")}</SBtn>
              <SBtn
                danger
                onClick={() => {
                  const id = workspaceToDelete.id;
                  setWorkspaceToDelete(null);
                  void deleteWorkspace(id);
                }}
              >
                {t("common.delete")}
              </SBtn>
            </>
          }
        >
          <p className="text-sm leading-relaxed text-slate-400">
            {t("sidebar.deleteWorkspaceConfirm", { name: workspaceToDelete.name })}
          </p>
        </SModal>
      )}
      {profileToDelete && (
        <SModal
          title={t("profile.deleteTitle")}
          subtitle={profileToDelete.name}
          onClose={() => setProfileToDelete(null)}
          footer={
            <>
              <SBtn onClick={() => setProfileToDelete(null)}>{t("common.cancel")}</SBtn>
              <SBtn
                danger
                disabled={profileBusy}
                onClick={() => {
                  const id = profileToDelete.id;
                  setProfileToDelete(null);
                  void deleteProfile(id);
                }}
              >
                {t("common.delete")}
              </SBtn>
            </>
          }
        >
          <p className="text-sm leading-relaxed text-slate-300">
            {t("profile.deleteConfirm", { name: profileToDelete.name })}
          </p>
          <p className="mt-3 rounded-lg border border-rose-400/20 bg-rose-500/[0.07] px-3 py-2 font-mono text-[10px] leading-relaxed text-rose-200/90">
            {t("profile.deleteData")}
          </p>
        </SModal>
      )}

      <div className="relative z-10 flex min-w-0 flex-1 flex-col">
        <Header
          view={view}
          agent={activeDisplayAgent}
          panelOpen={panelOpen}
          onTogglePanel={() => setPanelOpen((v) => !v)}
          onToggleSidebar={() => setSidebarOpen(true)}
          onBack={() => {
            setView("chat");
            setTab("chat");
          }}
          onClear={() => setClearHistoryOpen(true)}
          onExport={exportTranscript}
          temp={temp}
          topP={topP}
          onTempChange={(value) => {
            setTemp(value);
            if (activeProfileId) void updateProfile(activeProfileId, { temperature: value });
          }}
          onTopPChange={(value) => {
            setTopP(value);
            if (activeProfileId) void updateProfile(activeProfileId, { topP: value });
          }}
          onGenerationPreset={(temperature, topP) => {
            setTemp(temperature);
            setTopP(topP);
            if (activeProfileId) void updateProfile(activeProfileId, { temperature, topP });
          }}
          onSystemPromptSave={(value) => {
            void wsRequest(19, { id: activeAgentId, patch: { systemPrompt: value } }).then(() =>
              setDb((current) =>
                current
                  ? {
                      ...current,
                      agents: current.agents.map((item) =>
                        item.id === activeAgentId ? { ...item, systemPrompt: value } : item,
                      ),
                    }
                  : current,
              ),
            );
          }}
          settingsActions={
            view === "settings" && (
              <div className="flex shrink-0 gap-1">
                <button
                  onClick={() => {
                    setTransferSelection(Object.fromEntries(transferCategories.map((key) => [key, false])));
                    setTransferAvailable([...transferCategories]);
                    setTransferMode("export");
                  }}
                  className="grid h-7 w-7 place-items-center rounded-md border border-white/10 text-slate-400 hover:bg-white/[0.06]"
                  title={t("settings.export")}
                >
                  <Download size={12} />
                </button>
                <button
                  onClick={() => importSettingsRef.current?.click()}
                  className="grid h-7 w-7 place-items-center rounded-md border border-white/10 text-slate-400 hover:bg-white/[0.06]"
                  title={t("settings.import")}
                >
                  <Upload size={12} />
                </button>
                <button
                  onClick={() => setCleanupOpen(true)}
                  className="ml-2 grid h-7 w-7 place-items-center rounded-md border border-rose-400/30 bg-rose-500/[0.08] text-rose-300 hover:bg-rose-500/[0.18] hover:text-rose-100"
                  title={t("settings.cleanup")}
                >
                  <Eraser size={12} />
                </button>
              </div>
            )
          }
        />

        {view === "chat" ? (
          <>
            <div className="relative z-10 flex items-center border-b border-white/[0.06] bg-abyss-900/40 px-4 pt-1.5 sm:px-6">
              {TABS.map(({ k, label, icon: Icon }) => (
                <button
                  key={k}
                  onClick={() => setTab(k)}
                  className={`relative flex items-center gap-2 rounded-t-lg px-4 py-2.5 text-[12.5px] font-medium transition-colors ${
                    tab === k ? "text-slate-100" : "text-slate-500 hover:text-slate-300"
                  }`}
                >
                  <Icon size={14} className={tab === k ? "text-indigo-300" : ""} />
                  {label}
                  {k === "files" && activeWsMeta?.fileCount !== undefined && (
                    <span className="rounded border border-white/[0.08] bg-white/[0.03] px-1.5 py-px font-mono text-[9px] text-slate-500">
                      {activeWsMeta.fileCount}
                    </span>
                  )}
                  {tab === k && (
                    <motion.span
                      layoutId="tab-underline"
                      className="absolute inset-x-2 -bottom-px h-[2px] rounded-full bg-gradient-to-r from-indigo-400 to-cyan-400 shadow-[0_0_10px_rgba(129,140,248,0.8)]"
                    />
                  )}
                </button>
              ))}
            </div>

            <div className="relative flex min-h-0 flex-1 flex-col">
              {tab === "chat" ? (
                <>
                  <ChatArea
                    agent={activeDisplayAgent}
                    messages={messages}
                    busy={busy}
                    hasWorkspace={Boolean(activeWsId)}
                    onPreset={(txt) => setDraft({ text: txt, nonce: Date.now() })}
                    presets={agentPresets}
                    userName={activeProfile?.name ?? t("chat.user")}
                    onContinue={continueConversation}
                    onSwitchAndContinue={switchAndContinueConversation}
                    continuingErrorMessageId={continuingErrorMessageId}
                    onDeleteMessage={deleteMessage}
                  />
                  <InputBar
                    agent={activeDisplayAgent}
                    busy={busy}
                    disabled={!activeWsId}
                    onSend={(txt, f, s, asGoal) => void sendMessage(txt, f, s, asGoal)}
                    onStop={stopConversation}
                    draft={draft}
                    skills={db.skills}
                    presets={agentPresets}
                    mainModels={activeAgent.mainModels}
                    selectedModel={activeConv?.activeModel ?? pendingModel}
                    modelChanging={switchingModel}
                    onModelSelect={(model) => void switchConversationModel(model)}
                  />
                  <div className="pointer-events-none absolute bottom-4 right-4 z-30 flex flex-col items-end gap-2.5">
                    <AnimatePresence>
                      {bgTasks.length > 0 && (
                        <div className="pointer-events-auto">
                          <BgPlate tasks={bgTasks} accentRgb={accentRgb} />
                        </div>
                      )}
                    </AnimatePresence>
                    {(goalPopup || (!goalCollapsed && goal)) && (
                      <div className="pointer-events-auto">
                        <GoalPlate
                          goal={(goalPopup ?? goal)!}
                          accentRgb={accentRgb}
                          onResume={resumeGoal}
                          onPause={() => setGoalLifecycle("goal.pause")}
                          onStop={() => setGoalLifecycle("goal.stop")}
                          onClose={() => (goalPopup ? setGoalPopup(null) : setGoalCollapsed(true))}
                        />
                      </div>
                    )}
                  </div>
                </>
              ) : (
                <FilesExplorer workspaceId={activeWsId} accentRgb={accentRgb} notify={notify} />
              )}
            </div>
          </>
        ) : (
          <div className="scroll-slim relative z-10 min-h-0 flex-1 overflow-y-auto px-4 py-6 sm:px-8">
            {view === "settings" && (
              <>
                <input
                  ref={importSettingsRef}
                  type="file"
                  accept="application/json,.json"
                  className="hidden"
                  onChange={async (e) => {
                    const file = e.target.files?.[0];
                    e.target.value = "";
                    if (!file) return;
                    try {
                      const content = await file.text();
                      const data = JSON.parse(content) as Record<string, unknown>;
                      const available = transferCategories.filter((key) => key in data);
                      setTransferAvailable(available);
                      setTransferSelection(Object.fromEntries(transferCategories.map((key) => [key, false])));
                      setTransferContent(content);
                      setTransferMode("import");
                    } catch (error) {
                      notify("err", error instanceof Error ? error.message : String(error));
                    }
                  }}
                />
                <div className="mx-auto mb-6 flex max-w-6xl flex-wrap gap-2 border-b border-white/[0.08] pb-3">
                  {(
                    [
                      "agents",
                      "providers",
                      "mcp",
                      "presets",
                      "skills",
                      "kb",
                      "memory",
                      "network",
                      "systemInfo",
                    ] as SettingsTab[]
                  ).map((tab) => (
                    <button
                      key={tab}
                      onClick={() => {
                        setSettingsTab(tab);
                        writeHash("settings", null, tab);
                        void refreshSection(tab);
                      }}
                      className={`rounded-lg px-3 py-2 text-xs transition-colors ${settingsTab === tab ? "bg-indigo-400/15 text-indigo-200" : "text-slate-500 hover:bg-white/[0.05] hover:text-slate-200"}`}
                    >
                      {t(`nav.${tab}`)}
                    </button>
                  ))}
                </div>
              </>
            )}
            <Suspense
              fallback={
                <div className="grid min-h-48 place-items-center font-mono text-xs text-slate-500">
                  {t("common.loading")}
                </div>
              }
            >
              {(view === "skills" || (view === "settings" && settingsTab === "skills")) && (
                <SkillsScreen profileId={activeProfileId} patch={patch} notify={notify} />
              )}
              {(view === "providers" || (view === "settings" && settingsTab === "providers")) && (
                <ProvidersScreen providers={db.providers} proxies={db.proxies} patch={patch} notify={notify} />
              )}
              {(view === "mcp" || (view === "settings" && settingsTab === "mcp")) && (
                <McpScreen servers={db.mcpServers} patch={patch} notify={notify} />
              )}
              {(view === "agents" || (view === "settings" && settingsTab === "agents")) && (
                <AgentsScreen
                  agents={agents}
                  providers={db.providers}
                  groups={db.skillGroups}
                  mcpServers={db.mcpServers}
                  patch={patch}
                  notify={notify}
                />
              )}
              {(view === "presets" || (view === "settings" && settingsTab === "presets")) && (
                <PresetsScreen presets={db.presets} agents={agents} patch={patch} notify={notify} />
              )}
              {(view === "kb" || (view === "settings" && settingsTab === "kb")) && (
                <KbScreen docs={db.kbDocs} quotaBytes={db.kbQuotaBytes} patch={patch} notify={notify} />
              )}
              {(view === "network" || (view === "settings" && settingsTab === "network")) && (
                <NetworkScreen proxies={db.proxies} patch={patch} notify={notify} />
              )}
              {(view === "memory" || (view === "settings" && settingsTab === "memory")) && (
                <MemoryScreen notes={db.longTermNotes} topics={db.topicMemories} patch={patch} notify={notify} />
              )}
              {(view === "systemInfo" || (view === "settings" && settingsTab === "systemInfo")) && <SystemInfoScreen />}
            </Suspense>
          </div>
        )}
      </div>

      {panelOpen && view === "chat" && (
        <RightPanel
          agent={activeDisplayAgent}
          busy={busy}
          tokens={tokens}
          contextTokens={contextTokens}
          contextWindow={contextWindow}
          compressing={compressing}
          latencyMs={latencyMs}
          requestsPerMinute={requestsPerMinute}
          rpmLimit={rpmLimit}
          memory={activeConv?.memory}
          goals={goals}
          onGoalClick={(selectedGoal) => {
            setGoalPopup(selectedGoal);
            setGoalCollapsed(false);
          }}
          onCompress={() => void compressContext()}
        />
      )}

      {clearHistoryOpen && (
        <SModal
          title={t("header.clearHistoryTitle")}
          onClose={() => setClearHistoryOpen(false)}
          footer={
            <>
              <SBtn onClick={() => setClearHistoryOpen(false)}>{t("common.cancel")}</SBtn>
              <SBtn
                danger
                onClick={() => {
                  setClearHistoryOpen(false);
                  void clearHistory();
                }}
              >
                {t("header.clearHistory")}
              </SBtn>
            </>
          }
        >
          <p className="text-sm leading-relaxed text-slate-300">{t("header.clearHistoryConfirm")}</p>
        </SModal>
      )}
      <AnimatePresence>
        {pendingReq && <AgentRequestModal req={pendingReq} accentRgb={accentRgb} onRespond={respondRequest} />}
      </AnimatePresence>

      <AnimatePresence>
        {transferMode && (
          <SModal
            title={transferMode === "export" ? t("settings.export") : t("settings.import")}
            subtitle={t("settings.selectCategories")}
            onClose={() => setTransferMode(null)}
            footer={
              <>
                <SBtn onClick={() => setTransferMode(null)}>{t("common.cancel")}</SBtn>
                <SBtn
                  primary
                  disabled={!transferKeys.length}
                  onClick={() => {
                    if (transferMode === "export") {
                      setTransferMode(null);
                      void exportSettings(transferKeys);
                    } else if (transferContent) void importSettings(transferContent, transferKeys);
                  }}
                >
                  {transferMode === "export" ? t("settings.export") : t("settings.import")}
                </SBtn>
              </>
            }
          >
            <div className="mb-2 flex justify-end">
              <SBtn
                onClick={() => {
                  const enabled = transferAvailable.every((key) => transferSelection[key]);
                  setTransferSelection((current) =>
                    Object.fromEntries(transferAvailable.map((key) => [key, !enabled])),
                  );
                }}
              >
                {t("settings.selectAll")}
              </SBtn>
            </div>
            <div className="grid grid-cols-2 gap-2">
              {transferAvailable.map((key) => (
                <div
                  key={key}
                  className="flex items-center justify-between rounded-lg border border-white/[0.07] px-3 py-2"
                >
                  <span className="text-[11px] text-slate-300">
                    {key === "appSettings" ? t("settings.appSettings") : t(`nav.${key}`)}
                  </span>
                  <SToggle
                    on={Boolean(transferSelection[key])}
                    onChange={(value) => setTransferSelection((current) => ({ ...current, [key]: value }))}
                  />
                </div>
              ))}
            </div>
          </SModal>
        )}
      </AnimatePresence>

      {cleanupOpen && (
        <SModal
          title={t("settings.cleanup")}
          onClose={() => setCleanupOpen(false)}
          footer={
            <>
              <SBtn onClick={() => setCleanupOpen(false)}>{t("common.cancel")}</SBtn>
              <SBtn danger disabled={cleaningUp} onClick={() => void cleanupOrphans()}>
                {t("settings.cleanup")}
              </SBtn>
            </>
          }
        >
          <p className="text-sm leading-relaxed text-slate-300">{t("settings.cleanupConfirm")}</p>
          <p className="mt-3 rounded-lg border border-amber-400/20 bg-amber-400/[0.06] px-3 py-2 font-mono text-[10px] leading-relaxed text-amber-200/90">
            {t("settings.cleanupHint")}
          </p>
        </SModal>
      )}

      <AnimatePresence>
        {deleteConversationID && (
          <motion.div
            className="fixed inset-0 z-[80] grid place-items-center p-4"
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
          >
            <div
              className="absolute inset-0 bg-abyss-950/75 backdrop-blur-sm"
              onClick={() => setDeleteConversationID(null)}
            />
            <div className="relative w-full max-w-lg rounded-xl border border-rose-400/30 bg-abyss-850 p-5 shadow-2xl">
              <h2 className="font-display text-[15px] font-semibold text-slate-100">{t("chat.deleteDialogTitle")}</h2>
              <p className="mt-0.5 truncate font-mono text-[9.5px] tracking-[0.18em] text-slate-600">
                {convs.find((conversation) => conversation.id === deleteConversationID)?.title}
              </p>
              <p className="mt-4 text-[12px] leading-relaxed text-slate-300">{t("chat.deleteDialogText")}</p>
              <p className="mt-3 rounded-lg border border-rose-400/20 bg-rose-500/[0.07] px-3 py-2 font-mono text-[10px] leading-relaxed text-rose-200/90">
                {t("chat.deleteDialogFiles")}
              </p>
              <div className="mt-5 flex justify-end gap-2">
                <button
                  onClick={() => setDeleteConversationID(null)}
                  className="rounded-lg border border-white/[0.09] px-4 py-2 text-[12px] text-slate-300 hover:bg-white/[0.05]"
                >
                  {t("common.cancel")}
                </button>
                <button
                  onClick={() => {
                    const id = deleteConversationID;
                    setDeleteConversationID(null);
                    void deleteConversation(id);
                  }}
                  className="rounded-lg border border-rose-400/25 bg-rose-500/10 px-4 py-2 text-[12px] text-rose-300 hover:bg-rose-500/20"
                >
                  {t("chat.deleteDialogConfirm")}
                </button>
              </div>
            </div>
          </motion.div>
        )}
      </AnimatePresence>

      {/* toasts */}
      <div className="pointer-events-none fixed bottom-5 right-5 z-[70] flex flex-col items-end gap-2">
        <AnimatePresence>
          {toasts.map((toast) => (
            <motion.div
              key={toast.id}
              initial={{ opacity: 0, y: 16, scale: 0.96 }}
              animate={{ opacity: 1, y: 0, scale: 1 }}
              exit={{ opacity: 0, x: 40 }}
              transition={{ duration: 0.2 }}
              className={`flex items-center gap-2.5 rounded-xl border px-3.5 py-2.5 text-[12px] shadow-2xl backdrop-blur-xl ${
                toast.kind === "ok"
                  ? "border-emerald-400/25 bg-emerald-950/85 text-emerald-200"
                  : toast.kind === "err"
                    ? "border-rose-400/25 bg-rose-950/85 text-rose-200"
                    : "border-white/10 bg-abyss-800/90 text-slate-200"
              }`}
            >
              {toast.kind === "ok" ? (
                <CheckCircle2 size={14} />
              ) : toast.kind === "err" ? (
                <XCircle size={14} />
              ) : (
                <Info size={14} />
              )}
              <span>{toast.msg}</span>
            </motion.div>
          ))}
        </AnimatePresence>
      </div>
    </div>
  );
}

export default function App() {
  return (
    <I18nProvider>
      <Shell />
    </I18nProvider>
  );
}

export type { TVars };
