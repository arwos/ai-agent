import { useCallback, useEffect, useRef, useState } from "react";
import { AnimatePresence, motion } from "framer-motion";
import {
  Check,
  ChevronDown,
  ChevronRight,
  Circle,
  File,
  FileCode,
  FileJson,
  FileSpreadsheet,
  FileText,
  Folder,
  FolderPlus,
  HelpCircle,
  Image,
  Layers,
  ListChecks,
  Loader2,
  Paperclip,
  Pencil,
  Save,
  Send,
  ShieldCheck,
  Sparkles,
  Square,
  TerminalSquare,
  Trash2,
  X,
  Zap,
  Pause,
  Play,
  Ban,
  Minimize2,
} from "lucide-react";
import { fmtSize } from "../lib/data";
import type { LiveAgent, Preset, Skill, WorkspaceFile } from "../lib/data";
import { wsRequest } from "../lib/api";
import type { AgentRequest, BgTask, GoalState } from "../lib/api";
import { useT } from "../lib/i18n";
import { Prism as SyntaxHighlighter } from "react-syntax-highlighter";
import { vscDarkPlus } from "react-syntax-highlighter/dist/esm/styles/prism";

type Notify = (kind: "ok" | "err" | "info", msg: string) => void;

/* -------------------------------------------------------- file explorer -- */

function fileIcon(ext: string) {
  if (["ts", "tsx", "js", "jsx", "py", "sh"].includes(ext)) return FileCode;
  if (ext === "json") return FileJson;
  if (["md", "txt"].includes(ext)) return FileText;
  if (["csv", "sql"].includes(ext)) return FileSpreadsheet;
  if (["png", "jpg", "jpeg", "svg", "webp"].includes(ext)) return Image;
  return File;
}

type FileMeta = Omit<WorkspaceFile, "content"> & { isDir?: boolean };

export function FilesExplorer({
  workspaceId,
  accentRgb,
  notify,
}: {
  workspaceId: string | null;
  accentRgb: string;
  notify: Notify;
}) {
  const { t } = useT();
  const [tree, setTree] = useState<Record<string, FileMeta[]>>({});
  const [expandedDirectories, setExpandedDirectories] = useState<Set<string>>(() => new Set(["."]));
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [file, setFile] = useState<WorkspaceFile | null>(null);
  const [fileError, setFileError] = useState<string | null>(null);
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState("");
  const [newName, setNewName] = useState("");
  const [creating, setCreating] = useState(false);
  const [deleteFileId, setDeleteFileId] = useState<string | null>(null);
  const fileExt = (file?.ext ?? "").toLowerCase();
  const isImage = ["png", "jpg", "jpeg", "gif", "webp", "svg", "bmp", "ico"].includes(fileExt);
  const isText = file?.kind !== "binary" && !isImage;

  const loadDirectory = useCallback(
    async (dir: string) => {
      if (!workspaceId) return;
      const entries = await wsRequest<FileMeta[]>(67, { workspaceId, dir });
      setTree((current) => ({ ...current, [dir]: entries }));
    },
    [workspaceId],
  );

  const refresh = useCallback(async () => {
    if (!workspaceId) return;
    const entries = await wsRequest<FileMeta[]>(67, { workspaceId, dir: "." });
    setTree({ ".": entries });
    setExpandedDirectories(new Set(["."]));
  }, [workspaceId]);

  useEffect(() => {
    setSelectedId(null);
    setFile(null);
    setFileError(null);
    setEditing(false);
    void refresh();
  }, [refresh]);

  const open = async (id: string) => {
    if (!workspaceId) return;
    setSelectedId(id);
    setEditing(false);
    setFileError(null);
    try {
      const f = await wsRequest<WorkspaceFile>(68, { workspaceId, fileId: id });
      setFile(f);
      setDraft(f.content);
    } catch (error) {
      setFile(null);
      setFileError(t("files.previewError", { error: error instanceof Error ? error.message : String(error) }));
    }
  };

  const save = async () => {
    if (!workspaceId || !file) return;
    await wsRequest(69, { workspaceId, fileId: file.id, content: draft });
    setFile({ ...file, content: draft });
    setEditing(false);
    void refresh();
    notify("ok", t("files.saved"));
  };

  const create = async () => {
    if (!workspaceId || !newName.trim()) return;
    const f = await wsRequest<WorkspaceFile>(70, { workspaceId, name: newName.trim() });
    setNewName("");
    setCreating(false);
    void refresh();
    notify("ok", t("files.created", { name: f.name }));
    void open(f.id);
  };

  const remove = async (id: string) => {
    if (!workspaceId) return;
    await wsRequest(71, { workspaceId, fileId: id });
    if (selectedId === id) {
      setSelectedId(null);
      setFile(null);
    }
    void refresh();
    notify("info", t("files.deleted"));
  };

  const toggleDirectory = async (entry: FileMeta) => {
    if (!tree[entry.id]) await loadDirectory(entry.id);
    setExpandedDirectories((current) => {
      const next = new Set(current);
      if (next.has(entry.id)) next.delete(entry.id);
      else next.add(entry.id);
      return next;
    });
  };

  const allEntries = Object.values(tree).flat();
  const renderDirectory = (dir: string, depth = 0): React.ReactNode =>
    (tree[dir] ?? []).map((entry) => {
      const active = entry.id === selectedId;
      if (entry.isDir) {
        const expanded = expandedDirectories.has(entry.id);
        return (
          <div key={entry.id}>
            <button
              type="button"
              onClick={() => void toggleDirectory(entry)}
              className="flex w-full items-center gap-1.5 rounded-lg py-1.5 pr-2 text-left transition-all hover:bg-white/[0.03]"
              style={{ paddingLeft: `${8 + depth * 14}px` }}
            >
              {expanded ? (
                <ChevronDown size={12} className="shrink-0 text-slate-600" />
              ) : (
                <ChevronRight size={12} className="shrink-0 text-slate-600" />
              )}
              <Folder size={13} className="shrink-0 text-amber-300/80" />
              <span className="min-w-0 flex-1 truncate font-mono text-[11px] text-slate-300">{entry.name}</span>
            </button>
            {expanded && renderDirectory(entry.id, depth + 1)}
          </div>
        );
      }
      const Icon = fileIcon(entry.ext);
      return (
        <div key={entry.id} className="group relative">
          <button
            type="button"
            onClick={() => void open(entry.id)}
            className={`flex w-full items-center gap-2 rounded-lg py-1.5 pr-7 text-left transition-all ${active ? "bg-white/[0.07]" : "hover:bg-white/[0.03]"}`}
            style={{ paddingLeft: `${22 + depth * 14}px` }}
          >
            <Icon size={13} className="shrink-0" style={{ color: `rgba(${accentRgb},0.85)` }} />
            <span className="min-w-0 flex-1">
              <span className={`block truncate font-mono text-[11px] ${active ? "text-slate-100" : "text-slate-300"}`}>
                {entry.name}
              </span>
              <span className="block font-mono text-[8.5px] text-slate-600">{fmtSize(entry.size)}</span>
            </span>
          </button>
          <button
            onClick={() => setDeleteFileId(entry.id)}
            className="absolute right-1 top-1/2 grid h-5 w-5 -translate-y-1/2 place-items-center rounded text-slate-600 opacity-0 transition-all hover:bg-rose-500/15 hover:text-rose-300 group-hover:opacity-100"
            title={t("common.delete")}
          >
            <Trash2 size={10} />
          </button>
        </div>
      );
    });

  return (
    <div className="relative z-10 flex min-h-0 flex-1">
      {/* list */}
      <div className="flex w-[240px] shrink-0 flex-col border-r border-white/[0.06] bg-white/[0.015]">
        <div className="flex items-center justify-between border-b border-white/[0.06] px-3 py-2.5">
          <span className="font-mono text-[9px] tracking-[0.22em] text-slate-600">
            {t("files.title").toUpperCase()}
          </span>
          <button
            onClick={() => setCreating((v) => !v)}
            className="grid h-6 w-6 place-items-center rounded-md text-slate-500 transition-colors hover:bg-white/[0.07] hover:text-emerald-300"
            title={t("files.newFile")}
          >
            <FolderPlus size={13} />
          </button>
        </div>

        <AnimatePresence>
          {creating && (
            <motion.div
              initial={{ height: 0, opacity: 0 }}
              animate={{ height: "auto", opacity: 1 }}
              exit={{ height: 0, opacity: 0 }}
              className="overflow-hidden border-b border-white/[0.06]"
            >
              <div className="flex gap-1.5 p-2.5">
                <input
                  autoFocus
                  value={newName}
                  onChange={(e) => setNewName(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === "Enter") void create();
                    if (e.key === "Escape") setCreating(false);
                  }}
                  placeholder={t("files.namePh")}
                  className="min-w-0 flex-1 rounded-md border border-white/[0.09] bg-abyss-900/70 px-2 py-1.5 font-mono text-[10.5px] text-slate-200 outline-none placeholder:text-slate-700 focus:border-emerald-400/40"
                />
                <button
                  onClick={() => void create()}
                  className="grid h-7 w-7 shrink-0 place-items-center rounded-md bg-emerald-400/15 text-emerald-300 transition-colors hover:bg-emerald-400/25"
                  aria-label={t("common.add")}
                >
                  <Check size={13} />
                </button>
              </div>
            </motion.div>
          )}
        </AnimatePresence>

        <div className="scroll-slim min-h-0 flex-1 overflow-y-auto p-1.5">
          {renderDirectory(".")}
          {(tree["."]?.length ?? 0) === 0 && (
            <p className="px-3 py-4 text-[11px] text-slate-600">{t("files.noFiles")}</p>
          )}
        </div>
      </div>

      <AnimatePresence>
        {deleteFileId &&
          (() => {
            const target = allEntries.find((entry) => entry.id === deleteFileId);
            if (!target) return null;
            return (
              <motion.div
                className="fixed inset-0 z-[80] grid place-items-center p-4"
                initial={{ opacity: 0 }}
                animate={{ opacity: 1 }}
                exit={{ opacity: 0 }}
              >
                <div
                  className="absolute inset-0 bg-abyss-950/75 backdrop-blur-sm"
                  onClick={() => setDeleteFileId(null)}
                />
                <div className="relative w-full max-w-md rounded-xl border border-rose-400/30 bg-abyss-850 p-5 shadow-2xl">
                  <h2 className="font-display text-[15px] font-semibold text-slate-100">{t("files.deleteTitle")}</h2>
                  <p className="mt-2 truncate font-mono text-[11px] text-slate-300">{target.name}</p>
                  <p className="mt-3 text-[12px] leading-relaxed text-slate-400">{t("files.deleteConfirm")}</p>
                  <div className="mt-5 flex justify-end gap-2">
                    <button
                      onClick={() => setDeleteFileId(null)}
                      className="rounded-lg border border-white/[0.09] px-4 py-2 text-[12px] text-slate-300 hover:bg-white/[0.05]"
                    >
                      {t("common.cancel")}
                    </button>
                    <button
                      onClick={() => {
                        const id = deleteFileId;
                        setDeleteFileId(null);
                        void remove(id);
                      }}
                      className="rounded-lg border border-rose-400/25 bg-rose-500/10 px-4 py-2 text-[12px] text-rose-300 hover:bg-rose-500/20"
                    >
                      {t("common.delete")}
                    </button>
                  </div>
                </div>
              </motion.div>
            );
          })()}
      </AnimatePresence>

      {/* viewer */}
      <div className="flex min-w-0 flex-1 flex-col">
        {file ? (
          <>
            <div className="flex items-center gap-2.5 border-b border-white/[0.06] px-4 py-2.5">
              <span className="truncate font-mono text-[12px] text-slate-200">{file.name}</span>
              <span className="rounded border border-white/[0.08] bg-white/[0.03] px-1.5 py-px font-mono text-[9px] uppercase text-slate-500">
                {file.ext || "txt"}
              </span>
              <span className="font-mono text-[9px] text-slate-600">
                {fmtSize(file.size)} · {t("files.modified")} {file.updatedAt}
              </span>
              <div className="ml-auto flex gap-1.5">
                {editing && isText ? (
                  <>
                    <button
                      onClick={() => {
                        setEditing(false);
                        setDraft(file.content);
                      }}
                      className="flex items-center gap-1.5 rounded-lg border border-white/[0.09] px-2.5 py-1.5 text-[11px] text-slate-300 transition-colors hover:bg-white/[0.05]"
                    >
                      <X size={11} />
                      {t("common.cancel")}
                    </button>
                    <button
                      onClick={() => void save()}
                      className="flex items-center gap-1.5 rounded-lg bg-gradient-to-r from-indigo-500 to-cyan-500 px-2.5 py-1.5 text-[11px] font-medium text-white transition-all hover:brightness-115"
                    >
                      <Save size={11} />
                      {t("common.save")}
                    </button>
                  </>
                ) : isText ? (
                  <button
                    onClick={() => {
                      setDraft(file.content);
                      setEditing(true);
                    }}
                    className="flex items-center gap-1.5 rounded-lg border border-white/[0.09] px-2.5 py-1.5 text-[11px] text-slate-300 transition-colors hover:border-indigo-400/40 hover:text-indigo-200"
                  >
                    <Pencil size={11} />
                    {t("files.edit")}
                  </button>
                ) : null}
              </div>
            </div>
            <div className="min-h-0 flex-1 p-4">
              {editing && isText ? (
                <textarea
                  value={draft}
                  onChange={(e) => setDraft(e.target.value)}
                  spellCheck={false}
                  className="scroll-slim h-full w-full resize-none rounded-xl border border-indigo-400/25 bg-[#090d17] p-4 font-mono text-[12px] leading-6 text-slate-200 outline-none focus:border-indigo-400/50"
                />
              ) : isImage && file.data ? (
                <div className="grid h-full place-items-center rounded-xl border border-white/[0.06] bg-[#090d17] p-4">
                  <img src={file.data} alt={file.name} className="max-h-full max-w-full object-contain" />
                </div>
              ) : isText ? (
                <pre className="scroll-slim h-full overflow-auto whitespace-pre-wrap rounded-xl border border-white/[0.06] bg-[#090d17] p-4 font-mono text-[12px] leading-6 text-slate-300">
                  {file.content ? (
                    <SyntaxHighlighter
                      language={fileExt || "text"}
                      style={vscDarkPlus}
                      customStyle={{
                        margin: 0,
                        background: "transparent",
                        padding: 0,
                        whiteSpace: "pre-wrap",
                        overflow: "visible",
                      }}
                      codeTagProps={{ style: { font: "inherit" } }}
                    >
                      {file.content}
                    </SyntaxHighlighter>
                  ) : (
                    " "
                  )}
                </pre>
              ) : (
                <div className="grid h-full place-items-center rounded-xl border border-white/[0.06] bg-[#090d17] p-4 text-center text-[12px] text-slate-500">
                  {t("files.binaryUnavailable")}
                </div>
              )}
            </div>
          </>
        ) : fileError ? (
          <div className="grid flex-1 place-items-center p-6">
            <div className="max-w-lg rounded-xl border border-amber-400/25 bg-amber-400/[0.06] p-5 text-center">
              <p className="text-[12px] leading-relaxed text-amber-100/90">{fileError}</p>
            </div>
          </div>
        ) : (
          <div className="grid flex-1 place-items-center">
            <div className="text-center">
              <span className="mx-auto grid h-12 w-12 place-items-center rounded-xl border border-dashed border-white/[0.12] text-slate-600">
                <FileText size={20} />
              </span>
              <p className="mt-3 font-mono text-[11px] text-slate-600">{t("files.selectFile")}</p>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}

/* ------------------------------------------------------------- input bar -- */

type InputProps = {
  agent: LiveAgent;
  busy: boolean;
  disabled?: boolean;
  onSend: (text: string, files: string[], skillIds: string[], asGoal: boolean) => void;
  draft: { text: string; nonce: number } | null;
  skills: Skill[];
  presets: Preset[];
  mainModels: string[];
  selectedModel: string;
  modelChanging: boolean;
  onModelSelect: (model: string) => void;
  onStop: () => void;
};

export function InputBar({
  agent,
  busy,
  disabled = false,
  onSend,
  draft,
  skills,
  presets,
  mainModels,
  selectedModel,
  modelChanging,
  onModelSelect,
  onStop,
}: InputProps) {
  const { t } = useT();
  const [text, setText] = useState("");
  const [files, setFiles] = useState<string[]>([]);
  const [skillIds, setSkillIds] = useState<string[]>([]);
  const [asGoal, setAsGoal] = useState(false);
  const [presetsOpen, setPresetsOpen] = useState(false);
  const [skillsOpen, setSkillsOpen] = useState(false);
  const [modelsOpen, setModelsOpen] = useState(false);
  const [focused, setFocused] = useState(false);
  const sortedMainModels = [...(mainModels ?? [])].sort((a, b) =>
    a.localeCompare(b, undefined, { sensitivity: "base" }),
  );
  const taRef = useRef<HTMLTextAreaElement>(null);
  const fileRef = useRef<HTMLInputElement>(null);
  const accentRgb = (() => {
    const m = agent.accent;
    return m === "indigo"
      ? "129,140,248"
      : m === "cyan"
        ? "34,211,238"
        : m === "violet"
          ? "167,139,250"
          : m === "emerald"
            ? "52,211,153"
            : "251,191,36";
  })();

  useEffect(() => {
    if (draft) {
      setText(draft.text);
      setPresetsOpen(false);
      const el = taRef.current;
      if (el) {
        el.focus();
        el.style.height = "auto";
        el.style.height = `${Math.min(el.scrollHeight, 144)}px`;
      }
    }
  }, [draft]);

  const submit = () => {
    const tl = text.trim();
    if ((!tl && files.length === 0) || busy || disabled) return;
    onSend(tl, files, skillIds, asGoal);
    setText("");
    setFiles([]);
    // Skill selection belongs to one message only. Do not carry it over to
    // the next prompt after the current message has been submitted.
    setSkillIds([]);
    setAsGoal(false);
    const el = taRef.current;
    if (el) el.style.height = "auto";
  };

  const onInput = (value: string, el: HTMLTextAreaElement) => {
    setText(value);
    el.style.height = "auto";
    el.style.height = `${Math.min(el.scrollHeight, 144)}px`;
  };

  const canSend = (text.trim().length > 0 || files.length > 0) && !busy;
  const visiblePresets = presets.filter((p) => !p.agentId || p.agentId === agent.id);

  return (
    <div className="relative z-20 shrink-0 border-t border-white/[0.06] bg-abyss-900/75 px-4 pb-4 pt-3 backdrop-blur-xl sm:px-6">
      <div className="mx-auto max-w-3xl">
        <div className="mb-1.5 flex items-center gap-1 px-1">
          {/* Compact composer controls stay above the message field. */}
          <div className="relative shrink-0">
            <button
              onClick={() => {
                setPresetsOpen((v) => !v);
                setSkillsOpen(false);
                setModelsOpen(false);
              }}
              className={`flex h-7 items-center gap-1 rounded-md px-2 text-[10px] transition-colors ${presetsOpen ? "bg-white/[0.07] text-slate-100" : "text-slate-500 hover:bg-white/5 hover:text-slate-100"}`}
            >
              <Sparkles size={12} style={{ color: `rgb(${accentRgb})` }} />
              <span>{t("input.presets")}</span>
            </button>
            <AnimatePresence>
              {presetsOpen && (
                <>
                  <div className="fixed inset-0 z-20" onClick={() => setPresetsOpen(false)} />
                  <motion.div
                    initial={{ opacity: 0, y: 6, scale: 0.97 }}
                    animate={{ opacity: 1, y: 0, scale: 1 }}
                    exit={{ opacity: 0, y: 6, scale: 0.97 }}
                    transition={{ duration: 0.16, ease: "easeOut" }}
                    className="absolute bottom-full left-0 z-30 mb-2 w-80 rounded-xl p-px"
                    style={{
                      background: `linear-gradient(160deg, rgba(${accentRgb},0.45), rgba(255,255,255,0.08) 45%, rgba(34,211,238,0.3))`,
                      boxShadow: "0 22px 55px -16px rgba(0,0,0,0.75)",
                    }}
                  >
                    <div className="rounded-[11px] bg-abyss-850/98 p-2 backdrop-blur-2xl">
                      <p className="px-3 pb-1.5 pt-1.5 font-mono text-[9px] tracking-[0.24em] text-slate-600">
                        {t("input.presetsTitle")}
                      </p>
                      <div className="scroll-slim max-h-56 overflow-y-auto">
                        {visiblePresets.map((p) => (
                          <button
                            key={p.id}
                            onClick={() => {
                              setText(p.text);
                              setPresetsOpen(false);
                              taRef.current?.focus();
                            }}
                            className="w-full rounded-lg px-3 py-2 text-left transition-colors hover:bg-white/[0.06]"
                          >
                            <span className="block text-[12px] font-medium text-slate-200">{p.title}</span>
                            <span className="block truncate text-[10.5px] text-slate-500">{p.text}</span>
                          </button>
                        ))}
                        {visiblePresets.length === 0 && <p className="px-3 py-2 text-[11px] text-slate-600">—</p>}
                      </div>
                    </div>
                  </motion.div>
                </>
              )}
            </AnimatePresence>
          </div>
          <div className="relative shrink-0">
            <button
              onClick={() => {
                setSkillsOpen((v) => !v);
                setPresetsOpen(false);
                setModelsOpen(false);
              }}
              className={`flex h-7 items-center gap-1 rounded-md px-2 text-[10px] transition-colors ${skillsOpen || skillIds.length > 0 ? "bg-violet-400/[0.1] text-violet-200" : "text-slate-500 hover:bg-white/5 hover:text-slate-100"}`}
            >
              <Zap size={12} className={skillIds.length ? "text-violet-300" : ""} />
              <span>{t("input.skills")}</span>
              {skillIds.length > 0 && (
                <span className="rounded bg-violet-400/25 px-1 py-px font-mono text-[9px] text-violet-200">
                  {skillIds.length}
                </span>
              )}
            </button>
            <AnimatePresence>
              {skillsOpen && (
                <>
                  <div className="fixed inset-0 z-20" onClick={() => setSkillsOpen(false)} />
                  <motion.div
                    initial={{ opacity: 0, y: 6, scale: 0.97 }}
                    animate={{ opacity: 1, y: 0, scale: 1 }}
                    exit={{ opacity: 0, y: 6, scale: 0.97 }}
                    transition={{ duration: 0.16, ease: "easeOut" }}
                    className="absolute bottom-full left-0 z-30 mb-2 w-80 rounded-xl p-px"
                    style={{
                      background:
                        "linear-gradient(160deg, rgba(167,139,250,0.45), rgba(255,255,255,0.08) 45%, rgba(129,140,248,0.35))",
                      boxShadow: "0 22px 55px -16px rgba(0,0,0,0.75)",
                    }}
                  >
                    <div className="rounded-[11px] bg-abyss-850/98 p-2 backdrop-blur-2xl">
                      <div className="flex items-center justify-between px-3 pb-1.5 pt-1.5">
                        <p className="font-mono text-[9px] tracking-[0.24em] text-slate-600">
                          {t("input.skillsTitle")}
                        </p>
                        <p className="font-mono text-[9px] text-violet-300/80">
                          {t("input.skillsApproved", { n: skillIds.length })}
                        </p>
                      </div>
                      <div className="scroll-slim max-h-56 overflow-y-auto">
                        {skills
                          .filter((s) => s.userInvocable !== false)
                          .map((s) => {
                            const on = skillIds.includes(s.id);
                            return (
                              <button
                                key={s.id}
                                onClick={() => setSkillIds((p) => (on ? p.filter((x) => x !== s.id) : [...p, s.id]))}
                                className={`flex w-full items-center gap-2.5 rounded-lg px-3 py-2 text-left transition-colors ${on ? "bg-violet-400/[0.1]" : "hover:bg-white/[0.05]"}`}
                              >
                                <span
                                  className={`grid h-4 w-4 shrink-0 place-items-center rounded border ${on ? "border-violet-300 bg-violet-400/90 text-abyss-950" : "border-slate-600"}`}
                                >
                                  {on && <Check size={11} strokeWidth={3} />}
                                </span>
                                <span className="min-w-0 flex-1">
                                  <span
                                    className={`block truncate text-[12px] ${on ? "text-slate-100" : "text-slate-300"}`}
                                  >
                                    {s.name}
                                  </span>
                                  <span className="block truncate text-[10px] text-slate-600">{s.description}</span>
                                </span>
                                {!s.enabled && <span className="font-mono text-[8.5px] text-amber-400/80">off</span>}
                              </button>
                            );
                          })}
                        {skills.length === 0 && <p className="px-3 py-2 text-[11px] text-slate-600">—</p>}
                      </div>
                    </div>
                  </motion.div>
                </>
              )}
            </AnimatePresence>
          </div>
          <button
            type="button"
            onClick={() => setAsGoal((value) => !value)}
            className={`flex h-7 items-center gap-1 rounded-md px-2 text-[10px] transition-colors ${asGoal ? "bg-cyan-400/[0.12] text-cyan-200" : "text-slate-500 hover:bg-white/5 hover:text-slate-100"}`}
            title={t("input.goalHint")}
            aria-pressed={asGoal}
          >
            <Layers size={12} className={asGoal ? "text-cyan-300" : ""} />
            <span>{t("input.goal")}</span>
          </button>
        </div>
        <div
          className="rounded-xl p-px transition-[box-shadow] duration-300"
          style={{
            background: `linear-gradient(120deg, rgba(${accentRgb},0.45), rgba(255,255,255,0.09) 42%, rgba(34,211,238,0.38))`,
            boxShadow: focused
              ? `0 0 0 1px rgba(${accentRgb},0.35), 0 0 46px -10px rgba(${accentRgb},0.5)`
              : "0 10px 36px -16px rgba(0,0,0,0.6)",
          }}
        >
          <div className="rounded-[11px] bg-abyss-850/95">
            {/* attachments + skills chips */}
            <AnimatePresence>
              {(files.length > 0 || skillIds.length > 0) && (
                <motion.div
                  initial={{ opacity: 0, height: 0 }}
                  animate={{ opacity: 1, height: "auto" }}
                  exit={{ opacity: 0, height: 0 }}
                  className="overflow-hidden"
                >
                  <div className="flex flex-wrap gap-2 px-3 pt-3">
                    {files.map((f, i) => (
                      <span
                        key={`${f}-${i}`}
                        className="flex items-center gap-1.5 rounded-md border border-white/10 bg-white/[0.05] py-1 pl-2 pr-1 font-mono text-[10.5px] text-slate-300"
                      >
                        <FileText size={11} style={{ color: `rgb(${accentRgb})` }} />
                        <span className="max-w-[160px] truncate">{f}</span>
                        <button
                          onClick={() => setFiles((p) => p.filter((_, j) => j !== i))}
                          className="grid h-4 w-4 place-items-center rounded text-slate-500 transition-colors hover:bg-white/10 hover:text-rose-300"
                          aria-label={t("input.removeFile", { name: f })}
                        >
                          <X size={10} />
                        </button>
                      </span>
                    ))}
                    {skillIds.map((id) => {
                      const s = skills.find((x) => x.id === id);
                      if (!s) return null;
                      return (
                        <span
                          key={id}
                          className="flex items-center gap-1.5 rounded-md border border-violet-400/25 bg-violet-400/[0.08] py-1 pl-2 pr-1 font-mono text-[10.5px] text-violet-300"
                        >
                          <Zap size={10} />
                          <span className="max-w-[140px] truncate">{s.name}</span>
                          <button
                            onClick={() => setSkillIds((p) => p.filter((x) => x !== id))}
                            className="grid h-4 w-4 place-items-center rounded text-violet-400/70 transition-colors hover:bg-white/10 hover:text-rose-300"
                            aria-label={s.name}
                          >
                            <X size={10} />
                          </button>
                        </span>
                      );
                    })}
                  </div>
                </motion.div>
              )}
            </AnimatePresence>

            <div className="flex items-end gap-1.5 p-2">
              <input
                ref={fileRef}
                type="file"
                multiple
                className="hidden"
                onChange={(e) => {
                  const list = e.target.files;
                  if (list && list.length) setFiles((p) => [...p, ...Array.from(list).map((f) => f.name)].slice(0, 4));
                  e.target.value = "";
                }}
              />
              <button
                onClick={() => fileRef.current?.click()}
                className="grid h-9 w-9 shrink-0 place-items-center rounded-lg text-slate-500 transition-colors hover:bg-white/5 hover:text-slate-100"
                aria-label={t("input.attach")}
                title={t("input.attach")}
              >
                <Paperclip size={16} />
              </button>

              <textarea
                ref={taRef}
                rows={1}
                value={text}
                onChange={(e) => onInput(e.target.value, e.currentTarget)}
                onKeyDown={(e) => {
                  if (e.key === "Enter" && !e.shiftKey) {
                    e.preventDefault();
                    submit();
                  }
                }}
                onFocus={() => setFocused(true)}
                onBlur={() => setFocused(false)}
                placeholder={t("input.placeholder", { name: agent.name })}
                className="scroll-slim max-h-36 min-w-0 flex-1 resize-none bg-transparent px-1 py-2.5 text-sm leading-relaxed text-slate-200 outline-none placeholder:text-slate-600"
              />

              <motion.button
                whileTap={canSend ? { scale: 0.9 } : undefined}
                onClick={busy ? onStop : submit}
                disabled={busy ? disabled : !canSend || disabled}
                aria-label={busy ? t("input.stop") : t("input.send")}
                title={busy ? t("input.stop") : t("input.send")}
                className={`relative grid h-10 w-10 shrink-0 place-items-center rounded-lg bg-gradient-to-br text-white transition-all duration-300 ${
                  busy
                    ? "from-rose-500 to-orange-500"
                    : agent.accent === "cyan"
                      ? "from-cyan-500 to-teal-500"
                      : agent.accent === "violet"
                        ? "from-violet-500 to-fuchsia-500"
                        : agent.accent === "emerald"
                          ? "from-emerald-500 to-teal-500"
                          : agent.accent === "amber"
                            ? "from-amber-500 to-orange-500"
                            : "from-indigo-500 to-violet-500"
                } ${busy || canSend ? "hover:brightness-125" : "cursor-not-allowed opacity-30 saturate-50"}`}
                style={
                  busy || canSend
                    ? { boxShadow: `0 0 24px -4px rgba(${busy ? "251,113,133" : accentRgb},0.65)` }
                    : undefined
                }
              >
                {busy ? (
                  <Square size={13} fill="currentColor" />
                ) : (
                  <Send size={15} className="-translate-x-px translate-y-px" />
                )}
              </motion.button>
            </div>
          </div>
        </div>

        <div className="mt-2 flex items-center justify-between px-1">
          <span className="font-mono text-[10px] text-slate-600">{t("input.hint")}</span>
          <div className="relative hidden shrink-0 sm:block">
            <button
              type="button"
              disabled={mainModels.length === 0 || busy || modelChanging}
              onClick={() => {
                setModelsOpen((open) => !open);
                setPresetsOpen(false);
                setSkillsOpen(false);
              }}
              className={`inline-flex items-center gap-1 font-mono text-[10px] transition-colors ${modelsOpen ? "text-indigo-200" : "text-slate-600 hover:text-slate-300"} disabled:cursor-default disabled:hover:text-slate-600`}
              title={mainModels.length > 0 ? t("input.modelSelect") : undefined}
            >
              <span>
                {agent.name} · {agent.model}
              </span>
            </button>
            <AnimatePresence>
              {modelsOpen && (
                <>
                  <div className="fixed inset-0 z-20" onClick={() => setModelsOpen(false)} />
                  <motion.div
                    initial={{ opacity: 0, y: 6, scale: 0.97 }}
                    animate={{ opacity: 1, y: 0, scale: 1 }}
                    exit={{ opacity: 0, y: 6, scale: 0.97 }}
                    transition={{ duration: 0.16, ease: "easeOut" }}
                    className="absolute bottom-full right-0 z-30 mb-2 w-80 rounded-xl p-px"
                    style={{
                      background: `linear-gradient(160deg, rgba(${accentRgb},0.45), rgba(255,255,255,0.08) 45%, rgba(34,211,238,0.3))`,
                      boxShadow: "0 22px 55px -16px rgba(0,0,0,0.75)",
                    }}
                  >
                    <div className="rounded-[11px] bg-abyss-850/98 p-2 backdrop-blur-2xl">
                      <p className="px-3 pb-1.5 pt-1.5 font-mono text-[9px] tracking-[0.24em] text-slate-600">
                        {t("input.modelSelect")}
                      </p>
                      {sortedMainModels.map((model) => {
                        const selected = selectedModel === model || (!selectedModel && model === sortedMainModels[0]);
                        const option = { value: model, label: model };
                        return (
                          <button
                            key={option.value || "main"}
                            type="button"
                            onClick={() => {
                              setModelsOpen(false);
                              onModelSelect(option.value);
                            }}
                            className={`flex w-full items-center gap-2.5 rounded-lg px-3 py-2 text-left transition-colors ${selected ? "bg-indigo-400/[0.1]" : "hover:bg-white/[0.05]"}`}
                          >
                            <span
                              className={`grid h-4 w-4 shrink-0 place-items-center rounded-full border ${selected ? "border-indigo-300" : "border-slate-600"}`}
                            >
                              <span
                                className={`h-2 w-2 rounded-full ${selected ? "bg-indigo-300" : "bg-transparent"}`}
                              />
                            </span>
                            <span
                              className={`min-w-0 truncate font-mono text-[11px] ${selected ? "text-slate-100" : "text-slate-300"}`}
                            >
                              {option.label}
                            </span>
                          </button>
                        );
                      })}
                    </div>
                  </motion.div>
                </>
              )}
            </AnimatePresence>
          </div>
        </div>
      </div>
    </div>
  );
}

/* ---------------------------------------------------- agent request modal -- */

export function AgentRequestModal({
  req,
  accentRgb,
  onRespond,
}: {
  req: AgentRequest;
  accentRgb: string;
  onRespond: (reqId: string, value: boolean | string | string[]) => void;
}) {
  const { t } = useT();
  const [answer, setAnswer] = useState("");
  const [choice, setChoice] = useState<string | null>(null);
  const [multi, setMulti] = useState<string[]>([]);

  const eyebrow =
    req.kind === "approval"
      ? t("requests.approveTitle")
      : req.kind === "question"
        ? t("requests.questionTitle")
        : req.kind === "choice"
          ? t("requests.choiceTitle")
          : t("requests.multiTitle");

  const KindIcon =
    req.kind === "approval"
      ? ShieldCheck
      : req.kind === "question"
        ? HelpCircle
        : req.kind === "choice"
          ? ListChecks
          : Layers;
  const iconColor = req.kind === "approval" ? "52,211,153" : req.kind === "question" ? "34,211,238" : "167,139,250";

  return (
    <motion.div
      className="fixed inset-0 z-[60] grid place-items-center p-4"
      initial={{ opacity: 0 }}
      animate={{ opacity: 1 }}
      exit={{ opacity: 0 }}
    >
      <div className="absolute inset-0 bg-abyss-950/75 backdrop-blur-sm" />
      <motion.div
        initial={{ opacity: 0, y: 22, scale: 0.96 }}
        animate={{ opacity: 1, y: 0, scale: 1 }}
        exit={{ opacity: 0, y: 22, scale: 0.96 }}
        transition={{ duration: 0.22, ease: "easeOut" }}
        className="relative w-full max-w-md rounded-xl p-px"
        style={{
          background: `linear-gradient(160deg, rgba(${iconColor},0.55), rgba(255,255,255,0.08) 45%, rgba(${accentRgb},0.35))`,
          boxShadow: `0 30px 80px -20px rgba(0,0,0,0.85), 0 0 60px -18px rgba(${iconColor},0.35)`,
        }}
      >
        <div className="rounded-[11px] bg-abyss-850 p-5">
          <div className="mb-3 flex items-center gap-3">
            <span
              className="grid h-9 w-9 place-items-center rounded-lg"
              style={{ background: `rgba(${iconColor},0.12)`, boxShadow: `inset 0 0 0 1px rgba(${iconColor},0.3)` }}
            >
              <KindIcon size={16} style={{ color: `rgb(${iconColor})` }} />
            </span>
            <div className="min-w-0 flex-1">
              <p className="font-mono text-[8.5px] tracking-[0.24em] text-slate-600">{eyebrow.toUpperCase()}</p>
              <p className="truncate font-display text-[14px] font-semibold text-slate-100">
                {req.title === "Confirm goal completion" ? t("goal.confirmCompletion") : req.title}
              </p>
            </div>
          </div>

          {req.detail && <p className="mb-3 text-[12px] leading-relaxed text-slate-400">{req.detail}</p>}

          {req.kind === "approval" && req.command && (
            <div className="mb-4 flex items-start gap-2 rounded-lg border border-white/[0.07] bg-[#090d17] px-3 py-2.5">
              <TerminalSquare size={13} className="mt-0.5 shrink-0 text-emerald-300" />
              <code className="break-all font-mono text-[11.5px] leading-relaxed text-slate-300">$ {req.command}</code>
            </div>
          )}

          {req.kind === "question" && (
            <>
              {req.question && <p className="mb-2.5 text-[12.5px] leading-relaxed text-slate-300">{req.question}</p>}
              <textarea
                autoFocus
                value={answer}
                onChange={(e) => setAnswer(e.target.value)}
                rows={3}
                placeholder={req.placeholder ?? t("requests.answerPh")}
                className="mb-4 w-full resize-none rounded-lg border border-white/[0.09] bg-abyss-900/70 px-3 py-2.5 text-[12.5px] text-slate-200 outline-none placeholder:text-slate-700 focus:border-cyan-400/40"
              />
            </>
          )}

          {(req.kind === "choice" || req.kind === "multichoice") && (
            <>
              {req.question && <p className="mb-2.5 text-[12.5px] leading-relaxed text-slate-300">{req.question}</p>}
              <div className="mb-4 space-y-1.5">
                {(req.options ?? []).map((o) => {
                  const on = req.kind === "choice" ? choice === o.id : multi.includes(o.id);
                  return (
                    <button
                      key={o.id}
                      onClick={() => {
                        if (req.kind === "choice") setChoice(o.id);
                        else setMulti((m) => (m.includes(o.id) ? m.filter((x) => x !== o.id) : [...m, o.id]));
                      }}
                      className={`flex w-full items-center gap-2.5 rounded-lg border px-3 py-2.5 text-left transition-all ${
                        on ? "border-violet-400/40 bg-violet-400/[0.1]" : "border-white/[0.07] hover:bg-white/[0.04]"
                      }`}
                    >
                      <span
                        className={`grid h-4 w-4 shrink-0 place-items-center ${req.kind === "multichoice" ? "rounded" : "rounded-full"} border ${on ? "border-violet-300 bg-violet-400/90 text-abyss-950" : "border-slate-600"}`}
                      >
                        {on && <Check size={11} strokeWidth={3} />}
                      </span>
                      <span className="min-w-0 flex-1">
                        <span className={`block text-[12.5px] ${on ? "text-slate-100" : "text-slate-300"}`}>
                          {o.label}
                        </span>
                        {o.hint && <span className="block text-[10.5px] text-slate-500">{o.hint}</span>}
                      </span>
                    </button>
                  );
                })}
              </div>
            </>
          )}

          <div className="flex justify-end gap-2">
            {req.kind === "approval" ? (
              <>
                <button
                  onClick={() => onRespond(req.reqId, false)}
                  className="rounded-lg border border-rose-400/25 bg-rose-500/10 px-4 py-2 text-[12px] font-medium text-rose-300 transition-colors hover:bg-rose-500/20"
                >
                  {t("requests.decline")}
                </button>
                <button
                  onClick={() => onRespond(req.reqId, true)}
                  className="rounded-lg bg-gradient-to-r from-emerald-500 to-teal-500 px-4 py-2 text-[12px] font-semibold text-white shadow-[0_0_24px_-8px_rgba(52,211,153,0.7)] transition-all hover:brightness-115"
                >
                  {t("requests.approve")}
                </button>
              </>
            ) : req.kind === "question" ? (
              <button
                onClick={() => onRespond(req.reqId, answer)}
                className="rounded-lg bg-gradient-to-r from-cyan-500 to-teal-500 px-4 py-2 text-[12px] font-semibold text-white shadow-[0_0_24px_-8px_rgba(34,211,238,0.7)] transition-all hover:brightness-115"
              >
                {t("requests.answer")}
              </button>
            ) : (
              <button
                onClick={() => onRespond(req.reqId, req.kind === "choice" ? (choice ?? "") : multi)}
                disabled={req.kind === "choice" ? !choice : multi.length === 0}
                className="rounded-lg bg-gradient-to-r from-violet-500 to-fuchsia-500 px-4 py-2 text-[12px] font-semibold text-white shadow-[0_0_24px_-8px_rgba(167,139,250,0.7)] transition-all hover:brightness-115 disabled:cursor-not-allowed disabled:opacity-40"
              >
                {t("requests.apply")}
              </button>
            )}
          </div>
        </div>
      </motion.div>
    </motion.div>
  );
}

/* -------------------------------------------------------------- plates -- */

export function GoalPlate({
  goal,
  accentRgb,
  onResume,
  onPause,
  onStop,
  onClose,
}: {
  goal: GoalState;
  accentRgb: string;
  onResume?: () => void;
  onPause?: () => void;
  onStop?: () => void;
  onClose?: () => void;
}) {
  const { t } = useT();
  const readableTask = (label: string) =>
    (
      ({
        "fs.list_dir": "List files and directories in the workspace",
        "fs.read_file": "Read a text file from the workspace",
        "fs.write_file": "Write content to a workspace file",
        "fs.edit_file": "Edit selected parts of a workspace file",
        "fs.grep": "Search file contents in the workspace",
        "fs.glob": "Find workspace files by name pattern",
        "fs.mkdir": "Create a directory in the workspace",
        "fs.delete_file": "Delete a file from the workspace",
        "fs.move_file": "Move a file within the workspace",
      }) as Record<string, string>
    )[label] ?? label;
  const done = goal.tasks.filter((x) => x.status === "done").length;
  const pct = Math.round((done / Math.max(goal.tasks.length, 1)) * 100);

  return (
    <motion.div
      initial={{ opacity: 0, y: 16, scale: 0.97 }}
      animate={{ opacity: 1, y: 0, scale: 1 }}
      exit={{ opacity: 0, y: 16, scale: 0.97 }}
      className="w-[300px] rounded-xl p-px"
      style={{
        background: `linear-gradient(160deg, rgba(${accentRgb},0.5), rgba(255,255,255,0.08) 50%, rgba(34,211,238,0.3))`,
        boxShadow: "0 24px 60px -18px rgba(0,0,0,0.8)",
      }}
    >
      <div className="relative rounded-[11px] bg-abyss-850/95 p-3.5 backdrop-blur-2xl">
        <div className="mb-1.5 flex items-center justify-between">
          <span className="flex items-center gap-1.5 font-mono text-[9px] tracking-[0.24em] text-slate-500">
            <Layers size={11} style={{ color: `rgb(${accentRgb})` }} />
            {t("goal.label")}
            <span className="ml-1 flex items-center gap-1" onClick={(event) => event.stopPropagation()}>
              {(goal.status === "paused" ||
                goal.status === "stopped" ||
                goal.status === "failed" ||
                goal.status === "incomplete") &&
                onResume && (
                  <button
                    title={t("goal.resume")}
                    onClick={onResume}
                    className="p-1 text-emerald-300 transition-colors hover:text-emerald-200"
                  >
                    <Play size={11} />
                  </button>
                )}
              {(goal.status === "running" || goal.status === "awaiting_approval") && onPause && (
                <button
                  title={t("goal.pause")}
                  onClick={onPause}
                  className="p-1 text-amber-300 transition-colors hover:text-amber-200"
                >
                  <Pause size={11} />
                </button>
              )}
              {goal.status !== "done" && goal.status !== "stopped" && onStop && (
                <button
                  title={t("goal.stop")}
                  onClick={onStop}
                  className="p-1 text-rose-300 transition-colors hover:text-rose-200"
                >
                  <Ban size={11} />
                </button>
              )}
            </span>
          </span>
          <span className="font-mono text-[10px]" style={{ color: `rgb(${accentRgb})` }}>
            {done}/{goal.tasks.length} · {pct}%
          </span>
        </div>
        {onClose && (
          <button
            title={t("common.close")}
            onClick={onClose}
            className="absolute right-0 top-0 p-1 text-slate-500 transition-colors hover:text-slate-200"
          >
            <Minimize2 size={11} />
          </button>
        )}
        <p className="mb-2.5 text-[12.5px] font-medium leading-snug text-slate-100">{goal.goal}</p>
        <div className="mb-2.5 h-1 overflow-hidden rounded-full bg-white/[0.07]">
          <motion.div
            className="h-full rounded-full"
            animate={{ width: `${pct}%` }}
            transition={{ duration: 0.5 }}
            style={{ background: `linear-gradient(90deg, rgb(${accentRgb}), #22d3ee)` }}
          />
        </div>
        <div className="space-y-1">
          {goal.tasks.map((task) => (
            <div key={task.id} className="flex items-center gap-2">
              {task.status === "done" ? (
                <Check size={12} className="shrink-0 text-emerald-400" />
              ) : task.status === "failed" ? (
                <X size={12} className="shrink-0 text-rose-400" />
              ) : task.status === "skipped" ? (
                <Circle size={12} className="shrink-0 text-amber-400/70" />
              ) : task.status === "running" ? (
                <Loader2 size={12} className="shrink-0 animate-spin" style={{ color: `rgb(${accentRgb})` }} />
              ) : (
                <Circle size={12} className="shrink-0 text-slate-700" />
              )}
              <span
                className={`truncate text-[11px] ${task.status === "done" ? "text-slate-500 line-through decoration-slate-700" : task.status === "failed" ? "text-rose-300" : task.status === "skipped" ? "text-amber-300/70" : task.status === "running" ? "text-slate-200" : "text-slate-500"}`}
              >
                {readableTask(task.label)}
              </span>
            </div>
          ))}
        </div>
      </div>
    </motion.div>
  );
}

export function BgPlate({ tasks, accentRgb }: { tasks: BgTask[]; accentRgb: string }) {
  const { t } = useT();
  return (
    <motion.div
      initial={{ opacity: 0, y: 16, scale: 0.97 }}
      animate={{ opacity: 1, y: 0, scale: 1 }}
      exit={{ opacity: 0, y: 16, scale: 0.97 }}
      className="w-[280px] rounded-xl border border-white/[0.09] bg-abyss-850/95 p-3.5 shadow-[0_24px_60px_-18px_rgba(0,0,0,0.8)] backdrop-blur-2xl"
    >
      <p className="mb-2 flex items-center gap-1.5 font-mono text-[9px] tracking-[0.24em] text-slate-500">
        <Loader2 size={11} className="animate-spin" style={{ color: `rgb(${accentRgb})` }} />
        {t("bg.title")}
      </p>
      <div className="space-y-2">
        {tasks.map((task) => (
          <div key={task.id}>
            <div className="mb-1 flex items-center justify-between">
              <span className="truncate text-[11px] text-slate-300">{task.label}</span>
              <span className="ml-2 shrink-0 font-mono text-[10px] text-slate-500">{Math.round(task.progress)}%</span>
            </div>
            <div className="h-1 overflow-hidden rounded-full bg-white/[0.07]">
              <motion.div
                className="h-full rounded-full"
                animate={{ width: `${task.progress}%` }}
                style={{ background: `rgba(${accentRgb},0.85)` }}
                transition={{ duration: 0.4 }}
              />
            </div>
          </div>
        ))}
      </div>
    </motion.div>
  );
}
