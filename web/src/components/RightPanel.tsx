import { useEffect, useState } from "react";
import { motion } from "framer-motion";
import { createPortal } from "react-dom";
import { Database, Gauge, Globe, Layers, Loader2, Minimize2, Plug, Terminal, X, Zap } from "lucide-react";
import type { LucideIcon } from "lucide-react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { ACCENTS, STATUS_META } from "../lib/data";
import type { LiveAgent } from "../lib/data";
import type { GoalState } from "../lib/api";
import { useT } from "../lib/i18n";

type Tool = { id: string; key: string; icon: LucideIcon; on: boolean };

const INITIAL_TOOLS: Tool[] = [
  { id: "web", key: "panel.toolWeb", icon: Globe, on: true },
  { id: "interp", key: "panel.toolInterp", icon: Terminal, on: true },
  { id: "kb", key: "panel.toolKb", icon: Database, on: false },
  { id: "api", key: "panel.toolApi", icon: Plug, on: false },
];

type Props = {
  agent: LiveAgent;
  busy: boolean;
  tokens: number;
  contextTokens: number;
  contextWindow: number | null;
  compressing: boolean;
  latencyMs: number | null;
  requestsPerMinute: number | null;
  rpmLimit: number;
  memory?: { title: string; summary: string; topics: string[] };
  goals: GoalState[];
  onCompress: () => void;
};

function SectionLabel({ children }: { children: string }) {
  return <p className="mb-2.5 font-mono text-[9.5px] tracking-[0.24em] text-slate-600">{children}</p>;
}

function formatLatency(milliseconds: number, t: (key: string) => string): string {
  if (milliseconds < 1000) return `${Math.round(milliseconds)} ${t("panel.milliseconds")}`;
  if (milliseconds < 60_000) return `${(milliseconds / 1000).toFixed(2)} ${t("panel.seconds")}`;
  if (milliseconds < 3_600_000) return `${(milliseconds / 60_000).toFixed(2)} ${t("panel.minutes")}`;
  return `${(milliseconds / 3_600_000).toFixed(2)} ${t("panel.hours")}`;
}

function formatContextTokens(tokens: number): string {
  if (tokens < 1000) return tokens.toLocaleString("ru-RU");
  return `${Math.round(tokens / 1000)}K`;
}

export function RightPanel({
  agent,
  busy,
  tokens,
  contextTokens,
  contextWindow,
  compressing,
  latencyMs,
  requestsPerMinute,
  rpmLimit,
  memory,
  goals,
  onCompress,
  onGoalClick,
}: Props & { onGoalClick?: (goal: GoalState) => void }) {
  const { t } = useT();
  const accent = ACCENTS[agent.accent] ?? ACCENTS.indigo;
  const meta = STATUS_META[agent.status] ?? STATUS_META.idle;
  const [tools, setTools] = useState<Tool[]>(INITIAL_TOOLS);
  const [latency, setLatency] = useState<number[]>(() => Array.from({ length: 28 }, () => 0));
  const [memoryOpen, setMemoryOpen] = useState(false);

  useEffect(() => {
    if (latencyMs !== null && latencyMs >= 0) {
      setLatency((prev) => [...prev.slice(1), latencyMs]);
    }
  }, [latencyMs]);

  const current = Math.round(latency[latency.length - 1]);
  const maxLatency = Math.max(...latency, 1);
  const points = latency.map((v, i) => `${(i / (latency.length - 1)) * 260},${46 - (v / maxLatency) * 40}`).join(" ");
  const MetricLabel = ({ icon: Icon, label, hint }: { icon: typeof Gauge; label: string; hint: string }) => (
    <span className="group relative flex items-center gap-1.5 text-[11px] text-slate-400">
      <Icon size={12} style={{ color: accent.hex }} />
      <span>{label}</span>
      <span className="pointer-events-none absolute bottom-full left-0 z-40 mb-2 w-56 rounded-lg border border-white/[0.12] bg-abyss-800 px-2.5 py-2 text-[10px] leading-relaxed text-slate-300 opacity-0 shadow-xl transition-opacity group-hover:opacity-100">
        {hint}
      </span>
    </span>
  );
  const ctxPct = contextWindow ? Math.min((contextTokens / contextWindow) * 100, 100) : 0;
  const ctxWarning = ctxPct >= 50;
  const ctxCritical = ctxPct >= 80;

  return (
    <aside className="scroll-slim relative z-10 hidden w-[300px] shrink-0 overflow-y-auto border-l border-white/[0.06] bg-white/[0.015] backdrop-blur-xl xl:block">
      {/* header */}
      <div className="border-b border-white/[0.06] p-4">
        <div className="flex items-center gap-3">
          <span
            className="grid h-10 w-10 place-items-center rounded-xl"
            style={{
              background: `rgba(${accent.rgb},0.12)`,
              boxShadow: busy
                ? `inset 0 0 0 1px rgba(${accent.rgb},0.35), 0 0 22px -4px rgba(${accent.rgb},0.6)`
                : `inset 0 0 0 1px rgba(${accent.rgb},0.22)`,
            }}
          >
            <agent.icon size={17} style={{ color: accent.hex }} />
          </span>
          <div className="min-w-0 flex-1">
            <p className="truncate font-display text-[13px] font-semibold text-slate-100">{agent.name}</p>
            <p className={`flex items-center gap-1.5 font-mono text-[9px] uppercase tracking-wider ${meta.text}`}>
              <span
                className={`h-1.5 w-1.5 rounded-full ${meta.dot} ${meta.pulse ? "dot-live" : ""}`}
                style={{ color: meta.dotHex }}
              />
              {t(`status.${agent.status}`)}
            </p>
          </div>
        </div>
      </div>

      <div className="space-y-5 p-4">
        {/* metrics */}
        <div>
          <SectionLabel>{t("panel.metrics")}</SectionLabel>
          <div className="glass rounded-xl p-3.5">
            <div className="mb-2 flex items-center justify-between">
              <MetricLabel icon={Gauge} label={t("panel.latency")} hint={t("panel.latencyHint")} />
              <motion.span
                key={current}
                initial={{ opacity: 0.4 }}
                animate={{ opacity: 1 }}
                className="font-mono text-[12px] text-slate-200"
              >
                {latencyMs === null ? "—" : formatLatency(current, t)}
              </motion.span>
            </div>
            <svg viewBox="0 0 260 48" className="h-12 w-full" preserveAspectRatio="none">
              <defs>
                <linearGradient id="lat-fill" x1="0" y1="0" x2="0" y2="1">
                  <stop offset="0%" stopColor={`rgba(${accent.rgb},0.35)`} />
                  <stop offset="100%" stopColor={`rgba(${accent.rgb},0)`} />
                </linearGradient>
              </defs>
              <polygon points={`0,48 ${points} 260,48`} fill="url(#lat-fill)" />
              <polyline
                points={points}
                fill="none"
                stroke={`rgba(${accent.rgb},0.9)`}
                strokeWidth="1.6"
                strokeLinejoin="round"
              />
            </svg>
          </div>

          <div className="glass mt-2 flex items-center justify-between rounded-xl px-3.5 py-3">
            <MetricLabel icon={Zap} label={t("panel.tokens")} hint={t("panel.tokensHint")} />
            <span className="font-mono text-[12px] text-slate-200">{tokens.toLocaleString("ru-RU")}</span>
          </div>

          <div className="glass mt-2 flex items-center justify-between rounded-xl px-3.5 py-3">
            <MetricLabel icon={Gauge} label={t("panel.rpm")} hint={t("panel.rpmHint")} />
            <span className="font-mono text-[12px] text-slate-200">
              {requestsPerMinute === null ? "—" : `${requestsPerMinute} / ${rpmLimit > 0 ? rpmLimit : "∞"}`}
            </span>
          </div>

          {/* context usage */}
          <div className="glass mt-2 rounded-xl p-3.5">
            <div className="mb-1.5 flex items-center justify-between">
              <span className="group relative flex items-center gap-1.5 text-[11px] text-slate-400">
                <Database size={12} style={{ color: ctxCritical ? "#fb7185" : ctxWarning ? "#fbbf24" : accent.hex }} />
                <span>{t("panel.context")}</span>
                <span className="pointer-events-none absolute bottom-full left-0 z-40 mb-2 w-56 rounded-lg border border-white/[0.12] bg-abyss-800 px-2.5 py-2 text-[10px] leading-relaxed text-slate-300 opacity-0 shadow-xl transition-opacity group-hover:opacity-100">
                  {t("panel.contextHint")}
                </span>
              </span>
              <motion.span
                key={contextTokens}
                initial={{ opacity: 0.4 }}
                animate={{ opacity: 1 }}
                className="font-mono text-[11px] text-slate-300"
              >
                {formatContextTokens(contextTokens)} / {contextWindow ? formatContextTokens(contextWindow) : "∞"}
              </motion.span>
            </div>
            <div className="h-1.5 overflow-hidden rounded-full bg-white/[0.07]">
              <motion.div
                className={`h-full rounded-full ${ctxCritical ? "bg-gradient-to-r from-rose-500 to-red-400" : ctxWarning ? "bg-gradient-to-r from-amber-400 to-orange-400" : "bg-gradient-to-r from-indigo-400 to-cyan-400"}`}
                animate={{ width: `${ctxPct}%` }}
                transition={{ duration: 0.5, ease: "easeOut" }}
                style={
                  ctxCritical
                    ? { boxShadow: "0 0 10px rgba(248,113,113,0.7)" }
                    : ctxWarning
                      ? { boxShadow: "0 0 10px rgba(251,191,36,0.6)" }
                      : undefined
                }
              />
            </div>
            <div className="mt-2.5 flex items-center justify-between gap-2">
              <span className="truncate font-mono text-[8.5px] text-slate-700" title={agent.compactionModel}>
                {t("panel.ctxModel", { m: agent.compactionModel })}
              </span>
              <button
                onClick={onCompress}
                disabled={compressing || contextTokens === 0}
                className={`flex shrink-0 items-center gap-1.5 rounded-md border px-2 py-1 font-mono text-[9.5px] transition-all disabled:cursor-not-allowed disabled:opacity-40 ${
                  ctxCritical
                    ? "context-compress-critical border-rose-400/45 bg-rose-500/15 text-rose-300 hover:bg-rose-500/25"
                    : ctxWarning
                      ? "border-amber-400/35 bg-amber-400/10 text-amber-300 hover:bg-amber-400/20"
                      : "border-white/[0.09] bg-white/[0.03] text-slate-400 hover:border-white/20 hover:text-slate-100"
                }`}
              >
                {compressing ? <Loader2 size={10} className="animate-spin" /> : <Minimize2 size={10} />}
                {compressing ? t("panel.compacting") : t("panel.compress")}
              </button>
            </div>
          </div>
        </div>

        {/*
          TODO: Future features — re-enable after their backend and UI flows
          are implemented:
          - Web search
          - Code interpreter
          - Knowledge base
          - API connectors
        */}
        {/* tools
        <div>
          <SectionLabel>{t("panel.tools")}</SectionLabel>
          <div className="space-y-1.5">
            {tools.map((tool) => {
              const Icon = tool.icon;
              return (
                <button
                  key={tool.id}
                  onClick={() => setTools((ts) => ts.map((x) => (x.id === tool.id ? { ...x, on: !x.on } : x)))}
                  className="glass flex w-full items-center gap-2.5 rounded-lg px-3 py-2.5 transition-colors hover:border-white/[0.14]"
                >
                  <Icon size={13} className={tool.on ? "text-cyan-300" : "text-slate-600"} />
                  <span className={`flex-1 text-left text-[11.5px] ${tool.on ? "text-slate-200" : "text-slate-500"}`}>{t(tool.key)}</span>
                  <span
                    className={`relative h-4 w-7 rounded-full border transition-all ${tool.on ? "border-transparent bg-cyan-400/30" : "border-white/10 bg-white/[0.04]"}`}
                  >
                    <span
                      className={`absolute top-0.5 h-2.5 w-2.5 rounded-full transition-all ${tool.on ? "left-3.5 bg-cyan-300 shadow-[0_0_8px_rgba(34,211,238,0.7)]" : "left-0.5 bg-slate-600"}`}
                    />
                  </span>
                </button>
              );
            })}
          </div>
        </div> */}

        {/* memory */}
        <div>
          <SectionLabel>{t("panel.memory")}</SectionLabel>
          <div className="glass rounded-xl p-3.5">
            <button
              onClick={() => setMemoryOpen(true)}
              disabled={!memory?.summary}
              className="w-full truncate text-left font-display text-[12px] font-medium text-indigo-200 transition-colors hover:text-cyan-200 disabled:cursor-default disabled:text-slate-600"
              title={memory?.title}
            >
              {memory?.title || t("panel.memoryEmpty")}
            </button>
          </div>
        </div>

        <div>
          <SectionLabel>{t("goal.history")}</SectionLabel>
          <div className="space-y-2">
            {goals.length === 0 && (
              <div className="glass rounded-xl px-3.5 py-3 text-[11px] text-slate-600">{t("goal.empty")}</div>
            )}
            {goals
              .slice()
              .reverse()
              .map((item) => {
                const tasks = Array.isArray(item.tasks) ? item.tasks : [];
                const done = tasks.filter((task) => task.status === "done").length;
                const percent = tasks.length ? Math.round((done / tasks.length) * 100) : 0;
                return (
                  <button
                    key={item.id}
                    onClick={() => onGoalClick?.(item)}
                    className="glass group flex w-full items-center gap-2 rounded-xl px-3.5 py-3 text-left transition-colors hover:bg-white/[0.05]"
                  >
                    <Layers size={12} className="shrink-0 text-cyan-300" />
                    <span className="min-w-0 flex-1 truncate text-[11px] text-slate-300">{item.goal}</span>
                    <span className="shrink-0 font-mono text-[10px] text-cyan-300">{percent}%</span>
                  </button>
                );
              })}
          </div>
        </div>
      </div>
      {memoryOpen &&
        memory?.summary &&
        createPortal(
          <div
            className="fixed inset-0 z-[90] grid place-items-center bg-abyss-950/75 p-5 backdrop-blur-sm"
            onClick={() => setMemoryOpen(false)}
          >
            <div
              className="glass scroll-slim max-h-[80vh] w-full max-w-2xl overflow-y-auto rounded-2xl p-5 shadow-2xl"
              onClick={(event) => event.stopPropagation()}
            >
              <div className="mb-4 flex items-center gap-3 border-b border-white/[0.08] pb-3">
                <h2 className="min-w-0 flex-1 truncate font-display text-lg font-semibold text-slate-100">
                  {memory.title}
                </h2>
                <button
                  onClick={() => setMemoryOpen(false)}
                  className="grid h-7 w-7 place-items-center rounded-lg text-slate-400 transition-colors hover:bg-white/[0.08] hover:text-slate-100"
                  title={t("common.close")}
                >
                  <X size={15} />
                </button>
              </div>
              <div className="markdown-content text-[13px] leading-relaxed text-slate-300">
                <ReactMarkdown remarkPlugins={[remarkGfm]}>{memory.summary}</ReactMarkdown>
              </div>
            </div>
          </div>,
          document.body,
        )}
    </aside>
  );
}
