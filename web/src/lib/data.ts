import { BarChart3, Bot, Code2, Feather, Lock, Microscope } from "lucide-react";
import type { LucideIcon } from "lucide-react";

export const uid = () => crypto.randomUUID();

export const formatMessageTime = (value: string | Date) => {
  const date = value instanceof Date ? value : new Date(value);
  if (Number.isNaN(date.getTime())) return String(value);
  const pad = (part: number) => String(part).padStart(2, "0");
  return `${pad(date.getDate())}.${pad(date.getMonth() + 1)}.${date.getFullYear()} ${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`;
};

export const nowTime = () => formatMessageTime(new Date());

export const fmtSize = (b: number) =>
  b >= 1048576 ? `${(b / 1048576).toFixed(1)} MB` : b >= 1024 ? `${(b / 1024).toFixed(1)} KB` : `${b} B`;

export type View =
  | "chat"
  | "settings"
  | "skills"
  | "providers"
  | "mcp"
  | "agents"
  | "presets"
  | "kb"
  | "network"
  | "memory"
  | "systemInfo";

export type AccentKey = "indigo" | "cyan" | "violet" | "emerald" | "amber";

export type AgentStatus = "idle" | "thinking" | "executing" | "awaiting_approval";

export const ACCENTS: Record<AccentKey, { hex: string; rgb: string; grad: string }> = {
  indigo: { hex: "#818cf8", rgb: "129,140,248", grad: "from-indigo-500 to-violet-600" },
  cyan: { hex: "#22d3ee", rgb: "34,211,238", grad: "from-cyan-400 to-teal-500" },
  violet: { hex: "#a78bfa", rgb: "167,139,250", grad: "from-violet-500 to-fuchsia-600" },
  emerald: { hex: "#34d399", rgb: "52,211,153", grad: "from-emerald-400 to-teal-500" },
  amber: { hex: "#fbbf24", rgb: "251,191,36", grad: "from-amber-400 to-orange-500" },
};

export const STATUS_META: Record<
  AgentStatus,
  { label: string; dot: string; dotHex: string; text: string; pulse: boolean }
> = {
  idle: { label: "Idle", dot: "bg-slate-500", dotHex: "#64748b", text: "text-slate-500", pulse: false },
  thinking: { label: "Thinking", dot: "bg-amber-400", dotHex: "#fbbf24", text: "text-amber-300", pulse: true },
  executing: { label: "Executing", dot: "bg-cyan-400", dotHex: "#22d3ee", text: "text-cyan-300", pulse: true },
  awaiting_approval: {
    label: "Awaiting approval",
    dot: "bg-violet-400",
    dotHex: "#a78bfa",
    text: "text-violet-300",
    pulse: true,
  },
};

export type IconKey = "code" | "chart" | "feather" | "lock" | "search" | "bot";

export const SKILL_ICONS: Record<IconKey, LucideIcon> = {
  code: Code2,
  chart: BarChart3,
  feather: Feather,
  lock: Lock,
  search: Microscope,
  bot: Bot,
};

export const SKILL_ICON_KEYS = Object.keys(SKILL_ICONS) as IconKey[];

export type MessagePart = { type: "text" | "code" | "reasoning"; content: string; lang?: string };

export type ChatMessage = {
  id: string;
  role: "user" | "agent" | "tool";
  time: string;
  parts: MessagePart[];
  attachments?: string[];
  skills?: string[];
  compact?: boolean;
  contextSize?: number;
  tokens?: number;
  model?: string;
  provider?: string;
  toolName?: string;
  toolArguments?: Record<string, unknown>;
  toolResult?: string;
  toolError?: string;
  toolRunning?: boolean;
  retryableError?: boolean;
};

export type Conversation = {
  id: string;
  workspaceId: string;
  agentId: string;
  title: string;
  updatedAt: string;
  activeModel?: string;
  messages: ChatMessage[];
  memory?: { title: string; summary: string; topics: string[]; updatedAt?: string };
  goal?: import("./api").GoalState | null;
  goals?: import("./api").GoalState[];
};

export type LongTermNote = {
  id: string;
  title: string;
  content: string;
  tags: string[];
  updatedAt?: string;
  scope?: "profile";
};

export type TopicMemory = {
  id: string;
  title: string;
  summary: string;
  tags: string[];
  updatedAt?: string;
  scope?: "profile";
};

export type MemoryPage<T> = { items: T[]; nextCursor: string; hasMore: boolean };

export type WorkspaceFile = {
  id: string;
  name: string;
  size: number;
  ext: string;
  updatedAt: string;
  content: string;
  kind?: "text" | "binary";
  data?: string;
};

export type Workspace = {
  id: string;
  name: string;
  folderPath: string;
  createdAt: string;
  files: WorkspaceFile[];
  fileCount?: number;
};

export type Skill = {
  id: string;
  name: string;
  description: string;
  content?: string;
  files?: string[];
  icon: IconKey;
  accent: AccentKey;
  enabled: boolean;
  userInvocable?: boolean;
  disableModelInvocation?: boolean;
  source: "manual" | "link" | "directory" | "aggregate";
};

export type SkillGroup = {
  id: string;
  profileId: string;
  name: string;
  description: string;
  applyAuto: boolean;
  skillIds: string[];
};

export type SkillPage = { items: Skill[]; nextCursor: string; hasMore: boolean; total: number };

export type ProviderProxy = {
  type: "http" | "socks5";
  host: string;
  port: number;
  username?: string;
  password?: string;
  hasPassword?: boolean;
};

export type Proxy = {
  id: string;
  name: string;
  description: string;
  type: "http" | "https" | "socks5";
  host: string;
  port: number;
  username?: string;
  password?: string;
  hasPassword?: boolean;
  insecureSkipVerify?: boolean;
};

export type Provider = {
  id: string;
  name: string;
  kind:
    "openai" | "anthropic" | "ollama" | "openrouter" | "deepseek" | "mistral" | "groq" | "xai" | "google" | "custom";
  baseUrl: string;
  apiKey: string;
  hasApiKey?: boolean;
  models: string[];
  enabled: boolean;
  latency?: number;
  proxyId?: string;
  proxy?: ProviderProxy;
  rpm?: number;
};

export type McpTool = {
  name: string;
  alias: string;
  description?: string;
  inputSchema?: Record<string, unknown>;
  enabled: boolean;
};

export type McpServer = {
  id: string;
  name: string;
  transport: "builtin" | "stdio" | "sse" | "http";
  command?: string;
  url?: string;
  headers: { id: string; k: string; v: string }[];
  prefix: string;
  enabled: boolean;
  tools: McpTool[];
  builtinKey?: string;
  system?: boolean;
};

export type KbDoc = {
  id: string;
  title: string;
  source: string;
  kind: "doc" | "note" | "link";
  tags: string[];
  size: number;
  updatedAt: string;
  content: string;
};

export type Preset = { id: string; title: string; text: string; agentId: string | null };

export type CompactionLevel = "brief" | "balanced" | "detailed" | "comprehensive" | "epic";

export type AgentConfig = {
  name: string;
  description: string;
  systemPrompt: string;
  mainModels: string[];
  compactionModel: string;
  compactionLevel: CompactionLevel;
  memoryModel?: string;
  iconKey: IconKey;
  accent: AccentKey;
  skillGroupIds?: string[];
  mcpIds: string[];
};

export type AgentEntry = { id: string } & AgentConfig;

export type Profile = {
  id: string;
  name: string;
  role: string;
  accent: AccentKey;
  createdAt: string;
  temperature?: number;
  topP?: number;
};

export type LiveAgent = {
  id: string;
  name: string;
  description: string;
  icon: LucideIcon;
  accent: AccentKey;
  model: string;
  mainModels: string[];
  compactionModel: string;
  compactionLevel: CompactionLevel;
  memoryModel: string;
  systemPrompt: string;
  skillGroupIds: string[];
  mcpIds: string[];
  topics: string[];
  status: AgentStatus;
};
