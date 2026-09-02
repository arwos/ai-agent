import { useEffect, useRef, useState } from "react";
import type { ReactNode } from "react";
import { AnimatePresence, motion } from "framer-motion";
import {
  ArrowLeft,
  BookOpen,
  Boxes,
  Brain,
  Cpu,
  Download,
  FileText,
  Menu,
  Network,
  PanelRight,
  Plus,
  Settings,
  Sparkles,
  Trash2,
  X,
} from "lucide-react";
import type { LucideIcon } from "lucide-react";
import { ACCENTS, STATUS_META } from "../lib/data";
import type { LiveAgent, View } from "../lib/data";
import { useT } from "../lib/i18n";

export const SETTINGS_META: Record<Exclude<View, "chat">, { icon: LucideIcon; rgb: string }> = {
  settings: { icon: Settings, rgb: "129,140,248" },
  skills: { icon: Sparkles, rgb: "129,140,248" },
  providers: { icon: Boxes, rgb: "34,211,238" },
  mcp: { icon: Network, rgb: "52,211,153" },
  agents: { icon: Cpu, rgb: "251,191,36" },
  presets: { icon: Plus, rgb: "167,139,250" },
  kb: { icon: BookOpen, rgb: "244,114,182" },
  network: { icon: Network, rgb: "96,165,250" },
  memory: { icon: Brain, rgb: "244,114,182" },
  systemInfo: { icon: Cpu, rgb: "34,211,238" },
};
const GENERATION_PRESETS = [
  { key: "strict", temperature: 0.1, topP: 0.3 },
  { key: "reasoned", temperature: 0.3, topP: 0.6 },
  { key: "balanced", temperature: 0.7, topP: 0.9 },
  { key: "creative", temperature: 1, topP: 0.95 },
] as const;

type Props = {
  view: View;
  agent: LiveAgent;
  panelOpen: boolean;
  onTogglePanel: () => void;
  onToggleSidebar: () => void;
  onBack: () => void;
  onClear: () => void;
  onExport: () => void;
  temp: number;
  topP: number;
  onTempChange: (v: number) => void;
  onTopPChange: (v: number) => void;
  onGenerationPreset: (temperature: number, topP: number) => void;
  onSystemPromptSave: (value: string) => void;
  settingsActions?: ReactNode;
};

export function Header(props: Props) {
  const {
    view,
    agent,
    panelOpen,
    onTogglePanel,
    onToggleSidebar,
    onBack,
    onClear,
    onExport,
    temp,
    topP,
    onTempChange,
    onTopPChange,
    onGenerationPreset,
    onSystemPromptSave,
    settingsActions,
  } = props;
  const { t } = useT();
  const [menuOpen, setMenuOpen] = useState(false);
  const [promptOpen, setPromptOpen] = useState(false);
  const [promptDraft, setPromptDraft] = useState(agent.systemPrompt);
  const menuRef = useRef<HTMLDivElement>(null);
  const accent = ACCENTS[agent.accent] ?? ACCENTS.indigo;
  const meta = STATUS_META[agent.status] ?? STATUS_META.idle;

  useEffect(() => {
    const handler = (e: MouseEvent) => {
      if (menuRef.current && !menuRef.current.contains(e.target as Node)) setMenuOpen(false);
    };
    document.addEventListener("mousedown", handler);
    return () => document.removeEventListener("mousedown", handler);
  }, []);

  /* ------------------------------------------------ settings mode header */
  if (view !== "chat") {
    const s = SETTINGS_META[view];
    const SIcon = s.icon;
    return (
      <header className="relative z-20 flex h-[64px] shrink-0 items-center gap-3 border-b border-white/[0.06] bg-abyss-900/70 px-4 backdrop-blur-xl sm:px-6">
        <button
          onClick={onToggleSidebar}
          className="grid h-9 w-9 place-items-center rounded-lg text-slate-400 transition-colors hover:bg-white/5 hover:text-white lg:hidden"
          aria-label={t("common.openMenu")}
        >
          <Menu size={18} />
        </button>
        <button
          onClick={onBack}
          className="flex items-center gap-2 rounded-lg border border-white/[0.08] bg-white/[0.03] px-3 py-1.5 text-xs text-slate-300 transition-all hover:border-white/20 hover:text-white"
        >
          <ArrowLeft size={13} />
          <span className="hidden sm:inline">{t("common.back")}</span>
        </button>
        <span
          className="grid h-10 w-10 shrink-0 place-items-center rounded-xl"
          style={{ background: `rgba(${s.rgb},0.12)`, boxShadow: `inset 0 0 0 1px rgba(${s.rgb},0.25)` }}
        >
          <SIcon size={17} style={{ color: `rgb(${s.rgb})` }} />
        </span>
        <div className="min-w-0 flex-1">
          <h1 className="truncate font-display text-[15px] font-semibold leading-tight text-slate-100 sm:text-base">
            {t(`nav.${view}`)}
          </h1>
          <p className="mt-0.5 truncate font-mono text-[10.5px] text-slate-500">{t(`navDesc.${view}`)}</p>
        </div>
        {settingsActions}
      </header>
    );
  }

  /* --------------------------------------------------------- chat header */
  return (
    <header className="relative z-20 flex h-[64px] shrink-0 items-center gap-3 border-b border-white/[0.06] bg-abyss-900/70 px-4 backdrop-blur-xl sm:px-6">
      <button
        onClick={onToggleSidebar}
        className="grid h-9 w-9 place-items-center rounded-lg text-slate-400 transition-colors hover:bg-white/5 hover:text-white lg:hidden"
        aria-label={t("common.openMenu")}
      >
        <Menu size={18} />
      </button>

      <span
        className="grid h-10 w-10 shrink-0 place-items-center rounded-xl"
        style={{ background: `rgba(${accent.rgb},0.12)`, boxShadow: `inset 0 0 0 1px rgba(${accent.rgb},0.25)` }}
      >
        <agent.icon size={18} style={{ color: accent.hex }} />
      </span>

      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2.5">
          <h1 className="truncate font-display text-[15px] font-semibold leading-tight text-slate-100 sm:text-base">
            {agent.name}
          </h1>
          <span
            className={`hidden items-center gap-1.5 rounded-full border border-white/10 bg-white/[0.03] px-2 py-0.5 font-mono text-[9px] uppercase tracking-wider sm:flex ${meta.text}`}
          >
            <span
              className={`h-1.5 w-1.5 rounded-full ${meta.dot} ${meta.pulse ? "dot-live" : ""}`}
              style={{ color: meta.dotHex }}
            />
            {t(`status.${agent.status}`)}
          </span>
        </div>
        <div className="mt-0.5 flex items-center gap-2">
          <span className="flex items-center gap-1.5 font-mono text-[10.5px] text-slate-500">
            <Cpu size={11} style={{ color: accent.hex }} />
            {agent.model}
          </span>
          <span className="hidden text-[10.5px] text-slate-600 md:inline">· {agent.description}</span>
        </div>
      </div>

      <button
        onClick={onTogglePanel}
        className={`hidden h-9 w-9 place-items-center rounded-lg border transition-all xl:grid ${
          panelOpen
            ? "border-white/15 bg-white/[0.08] text-slate-100"
            : "border-transparent text-slate-400 hover:bg-white/5 hover:text-white"
        }`}
        aria-label={t("header.contextPanel")}
        title={t("header.contextPanel")}
      >
        <PanelRight size={16} />
      </button>

      <div className="relative" ref={menuRef}>
        <button
          onClick={() => setMenuOpen((v) => !v)}
          className={`grid h-9 w-9 place-items-center rounded-lg border transition-all ${
            menuOpen
              ? "border-white/15 bg-white/[0.08] text-slate-100"
              : "border-transparent text-slate-400 hover:bg-white/5 hover:text-white"
          }`}
          aria-label={t("header.agentSettings")}
        >
          <motion.span animate={{ rotate: menuOpen ? 60 : 0 }} transition={{ duration: 0.25 }}>
            <Settings size={16} />
          </motion.span>
        </button>

        <AnimatePresence>
          {menuOpen && (
            <motion.div
              initial={{ opacity: 0, y: -6, scale: 0.97 }}
              animate={{ opacity: 1, y: 0, scale: 1 }}
              exit={{ opacity: 0, y: -6, scale: 0.97 }}
              transition={{ duration: 0.16, ease: "easeOut" }}
              className="absolute right-0 top-full z-40 mt-2 w-[300px] rounded-xl p-px"
              style={{
                background:
                  "linear-gradient(160deg, rgba(129,140,248,0.4), rgba(255,255,255,0.08) 40%, rgba(34,211,238,0.3))",
                boxShadow: "0 24px 60px -18px rgba(0,0,0,0.75)",
              }}
            >
              <div className="rounded-[11px] bg-abyss-850/95 p-4 backdrop-blur-2xl">
                <p className="mb-3 font-mono text-[9px] tracking-[0.24em] text-slate-600">{t("header.genParams")}</p>

                <div className="mb-3 grid grid-cols-2 gap-1.5">
                  {GENERATION_PRESETS.map((preset) => {
                    const active = temp === preset.temperature && topP === preset.topP;
                    return (
                      <button
                        key={preset.key}
                        type="button"
                        onClick={() => onGenerationPreset(preset.temperature, preset.topP)}
                        className={`rounded-md border px-2 py-1.5 text-[10px] transition-colors ${active ? "border-indigo-400/40 bg-indigo-400/15 text-indigo-200" : "border-white/[0.08] text-slate-500 hover:bg-white/[0.05] hover:text-slate-300"}`}
                      >
                        {t(`header.preset.${preset.key}`)}
                      </button>
                    );
                  })}
                </div>

                <label className="mb-3 block">
                  <div className="mb-1.5 flex items-center justify-between">
                    <span className="text-[11.5px] text-slate-400">{t("header.temperature")}</span>
                    <span className="font-mono text-[11px] text-indigo-300">{temp.toFixed(2)}</span>
                  </div>
                  <input
                    type="range"
                    min={0}
                    max={1}
                    step={0.05}
                    value={temp}
                    onChange={(e) => onTempChange(parseFloat(e.target.value))}
                    className="w-full cursor-pointer"
                  />
                </label>

                <label className="mb-1 block">
                  <div className="mb-1.5 flex items-center justify-between">
                    <span className="text-[11.5px] text-slate-400">{t("header.topP")}</span>
                    <span className="font-mono text-[11px] text-indigo-300">{topP.toFixed(2)}</span>
                  </div>
                  <input
                    type="range"
                    min={0}
                    max={1}
                    step={0.05}
                    value={topP}
                    onChange={(e) => onTopPChange(parseFloat(e.target.value))}
                    className="w-full cursor-pointer"
                  />
                </label>

                <div className="my-3 h-px bg-white/[0.06]" />

                <button
                  onClick={() => {
                    onClear();
                    setMenuOpen(false);
                  }}
                  className="flex w-full items-center gap-2.5 rounded-lg px-3 py-2 text-left text-xs text-slate-300 transition-colors hover:bg-rose-500/10 hover:text-rose-300"
                >
                  <Trash2 size={14} />
                  {t("header.clearHistory")}
                </button>
                <button
                  onClick={() => {
                    onExport();
                    setMenuOpen(false);
                  }}
                  className="flex w-full items-center gap-2.5 rounded-lg px-3 py-2 text-left text-xs text-slate-300 transition-colors hover:bg-white/5 hover:text-white"
                >
                  <Download size={14} />
                  {t("header.exportMd")}
                </button>

                <div className="my-3 h-px bg-white/[0.06]" />

                <button
                  onClick={() => {
                    setPromptDraft(agent.systemPrompt);
                    setPromptOpen(true);
                    setMenuOpen(false);
                  }}
                  className="flex w-full items-center gap-2.5 rounded-lg px-3 py-2 text-left text-xs text-slate-300 transition-colors hover:bg-white/5 hover:text-white"
                >
                  <FileText size={14} />
                  {t("header.sysPromptBtn")}
                </button>
              </div>
            </motion.div>
          )}
        </AnimatePresence>
      </div>

      <AnimatePresence>
        {promptOpen && (
          <motion.div
            className="fixed inset-0 z-50 grid place-items-center p-4"
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
          >
            <div className="absolute inset-0 bg-abyss-950/75 backdrop-blur-sm" onClick={() => setPromptOpen(false)} />
            <motion.div
              initial={{ opacity: 0, y: 18, scale: 0.96 }}
              animate={{ opacity: 1, y: 0, scale: 1 }}
              exit={{ opacity: 0, y: 18, scale: 0.96 }}
              transition={{ duration: 0.22, ease: "easeOut" }}
              className="relative w-full max-w-lg rounded-xl p-px"
              style={{
                background: `linear-gradient(160deg, rgba(${accent.rgb},0.5), rgba(255,255,255,0.08) 45%, rgba(34,211,238,0.35))`,
                boxShadow: `0 30px 80px -20px rgba(0,0,0,0.8), 0 0 50px -18px rgba(${accent.rgb},0.4)`,
              }}
            >
              <div className="rounded-[11px] bg-abyss-850 p-5">
                <div className="mb-4 flex items-center gap-3">
                  <span
                    className="grid h-9 w-9 place-items-center rounded-lg"
                    style={{ background: `rgba(${accent.rgb},0.12)` }}
                  >
                    <FileText size={16} style={{ color: accent.hex }} />
                  </span>
                  <div className="flex-1">
                    <p className="font-display text-sm font-semibold text-slate-100">{t("header.sysPromptTitle")}</p>
                    <p className="font-mono text-[10px] text-slate-500">
                      {agent.name} · {agent.model}
                    </p>
                  </div>
                  <button
                    onClick={() => setPromptOpen(false)}
                    className="grid h-8 w-8 place-items-center rounded-lg text-slate-400 transition-colors hover:bg-white/5 hover:text-white"
                    aria-label={t("common.close")}
                  >
                    <X size={15} />
                  </button>
                </div>
                <textarea
                  value={promptDraft}
                  onChange={(e) => setPromptDraft(e.target.value)}
                  rows={10}
                  spellCheck={false}
                  autoCorrect="off"
                  autoCapitalize="off"
                  className="scroll-slim w-full resize-y rounded-lg border border-white/[0.06] bg-abyss-900/70 p-4 font-mono text-xs leading-relaxed text-slate-300 outline-none focus:border-indigo-400/40"
                />
                <div className="mt-4 flex justify-end">
                  <button
                    onClick={() => {
                      onSystemPromptSave(promptDraft);
                      setPromptOpen(false);
                    }}
                    className="rounded-lg border border-indigo-400/30 bg-indigo-400/10 px-4 py-2 text-xs font-medium text-indigo-200 transition-colors hover:bg-indigo-400/20"
                  >
                    {t("common.save")}
                  </button>
                </div>
              </div>
            </motion.div>
          </motion.div>
        )}
      </AnimatePresence>
    </header>
  );
}
