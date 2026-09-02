import { useEffect, useRef, useState } from "react";
import type { ReactNode } from "react";
import unidecode from "unidecode";
import { AnimatePresence, motion } from "framer-motion";
import { createPortal } from "react-dom";
import {
  Boxes,
  Check,
  CheckCheck,
  ChevronDown,
  ChevronLeft,
  ChevronRight,
  Download,
  Eye,
  EyeOff,
  FolderOpen,
  Link2,
  Loader2,
  Network,
  KeyRound,
  Pencil,
  Plus,
  Puzzle,
  RefreshCw,
  ScanLine,
  Search,
  Sparkles,
  Trash2,
  X,
  Zap,
} from "lucide-react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { ACCENTS, SKILL_ICON_KEYS, SKILL_ICONS } from "../../lib/data";
import type {
  AccentKey,
  AgentConfig,
  IconKey,
  LiveAgent,
  McpServer,
  Preset,
  Provider,
  Proxy,
  Skill,
  SkillGroup,
  SkillPage,
} from "../../lib/data";
import { pickSkillFolder, wsRequest } from "../../lib/api";
import type { Db, DiscoveredSkill } from "../../lib/api";
import { useT } from "../../lib/i18n";

export type Notify = (kind: "ok" | "err" | "info", msg: string) => void;
type Patch = (p: Partial<Db>) => void;

/* ------------------------------------------------------------- primitives */

export function SModal({
  title,
  subtitle,
  onClose,
  children,
  footer,
  w = "max-w-lg",
  layer = 50,
}: {
  title: string;
  subtitle?: string;
  onClose: () => void;
  children: ReactNode;
  footer?: ReactNode;
  w?: string;
  layer?: number;
}) {
  const { t } = useT();
  return createPortal(
    <div className="fixed inset-0 grid place-items-center p-4" style={{ zIndex: layer }}>
      <div className="absolute inset-0 bg-abyss-950/75 backdrop-blur-sm" onClick={onClose} />
      <div
        className={`relative w-full ${w} animate-[modal-in_0.2s_ease-out] rounded-xl p-px`}
        style={{
          background:
            "linear-gradient(160deg, rgba(129,140,248,0.5), rgba(255,255,255,0.08) 45%, rgba(34,211,238,0.35))",
          boxShadow: "0 30px 80px -20px rgba(0,0,0,0.85)",
        }}
      >
        <div className="scroll-slim max-h-[84vh] overflow-y-auto rounded-[11px] bg-abyss-850 p-5">
          <div className="mb-4 flex items-start gap-3">
            <div className="flex-1">
              <p className="font-display text-[15px] font-semibold text-slate-100">{title}</p>
              {subtitle && <p className="mt-0.5 font-mono text-[9.5px] tracking-[0.18em] text-slate-600">{subtitle}</p>}
            </div>
            <button
              onClick={onClose}
              className="grid h-7 w-7 place-items-center rounded-lg text-slate-500 transition-colors hover:bg-white/5 hover:text-white"
              aria-label={t("common.close")}
            >
              <X size={14} />
            </button>
          </div>
          {children}
          {footer && <div className="mt-5 flex justify-end gap-2">{footer}</div>}
        </div>
      </div>
    </div>,
    document.body,
  );
}

export function SField({ label, children }: { label: string; children: ReactNode }) {
  return (
    <label className="mb-3 block">
      <span className="mb-1.5 block text-[11px] font-medium text-slate-400">{label}</span>
      {children}
    </label>
  );
}

export const inputCls =
  "w-full rounded-lg border border-white/[0.09] bg-abyss-900/70 px-3 py-2 text-[12.5px] text-slate-200 outline-none transition-colors placeholder:text-slate-700 focus:border-indigo-400/45";

export function SSelect({
  value,
  onChange,
  options,
  dropUp = false,
  searchable = false,
  searchPlaceholder = "",
  maxVisible,
}: {
  value: string;
  onChange: (value: string) => void;
  options: { value: string; label: string }[];
  dropUp?: boolean;
  searchable?: boolean;
  searchPlaceholder?: string;
  maxVisible?: number;
}) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const rootRef = useRef<HTMLDivElement>(null);
  const selected = options.find((option) => option.value === value);
  const filteredOptions = query.trim()
    ? options.filter((option) => option.label.toLowerCase().includes(query.trim().toLowerCase()))
    : options;
  useEffect(() => {
    if (!open) return;
    const closeOnOutside = (event: PointerEvent) => {
      if (!rootRef.current?.contains(event.target as Node)) setOpen(false);
    };
    document.addEventListener("pointerdown", closeOnOutside);
    return () => document.removeEventListener("pointerdown", closeOnOutside);
  }, [open]);
  return (
    <div ref={rootRef} className="relative">
      <button
        type="button"
        onClick={() => {
          setQuery("");
          setOpen((v) => !v);
        }}
        className={`${inputCls} flex items-center justify-between gap-2 text-left`}
      >
        <span className="min-w-0 truncate">{selected?.label ?? value}</span>
        <ChevronDown size={14} className={`shrink-0 text-slate-500 transition-transform ${open ? "rotate-180" : ""}`} />
      </button>
      {open && (
        <div
          className={`absolute left-0 right-0 z-50 rounded-lg border border-indigo-400/25 bg-abyss-850 p-1 shadow-2xl backdrop-blur-xl ${dropUp ? "bottom-full mb-1" : "top-full mt-1"}`}
        >
          {searchable && (
            <div className="mb-1 flex items-center gap-1.5 rounded-md border border-white/[0.08] px-2 text-slate-500">
              <Search size={12} />
              <input
                autoFocus
                value={query}
                onChange={(event) => setQuery(event.target.value)}
                placeholder={searchPlaceholder}
                className="min-w-0 flex-1 bg-transparent py-2 text-[11.5px] text-slate-200 outline-none placeholder:text-slate-600"
                spellCheck={false}
                autoComplete="off"
              />
            </div>
          )}
          <div className="overflow-y-auto" style={maxVisible ? { maxHeight: `${maxVisible * 36}px` } : undefined}>
            {filteredOptions.map((option) => (
              <button
                type="button"
                key={option.value}
                onClick={() => {
                  onChange(option.value);
                  setOpen(false);
                }}
                className={`flex w-full items-center rounded-md px-2.5 py-2 text-left text-[11.5px] transition-colors ${option.value === value ? "bg-indigo-400/15 text-indigo-100" : "text-slate-300 hover:bg-white/[0.06]"}`}
              >
                <span className="min-w-0 truncate">{option.label}</span>
              </button>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}

export function SToggle({
  on,
  onChange,
  rgb = "129,140,248",
  small = false,
}: {
  on: boolean;
  onChange: (v: boolean) => void;
  rgb?: string;
  small?: boolean;
}) {
  return (
    <button
      type="button"
      onClick={(e) => {
        e.stopPropagation();
        onChange(!on);
      }}
      className={`relative shrink-0 overflow-hidden rounded-full border transition-all ${small ? "h-4 w-8" : "h-5 w-9"} ${on ? "border-transparent" : "border-white/10 bg-white/[0.04]"}`}
      style={on ? { background: `rgba(${rgb},0.35)`, boxShadow: `inset 0 0 0 1px rgba(${rgb},0.5)` } : undefined}
      aria-pressed={on}
    >
      <motion.span
        layout
        className={`absolute rounded-full ${small ? "top-[1px] h-3.5 w-3.5" : "top-0.5 h-3.5 w-3.5"} ${on ? "right-[1px]" : "left-[1px] bg-slate-500"}`}
        style={on ? { background: `rgb(${rgb})`, boxShadow: `0 0 8px rgba(${rgb},0.7)` } : undefined}
        transition={{ type: "spring", stiffness: 520, damping: 34 }}
      />
    </button>
  );
}

export function SBtn({
  children,
  onClick,
  primary,
  disabled,
  danger,
}: {
  children: ReactNode;
  onClick?: () => void;
  primary?: boolean;
  disabled?: boolean;
  danger?: boolean;
}) {
  const base =
    "flex items-center justify-center gap-2 rounded-lg px-4 py-2 text-[12px] font-medium transition-all disabled:cursor-not-allowed disabled:opacity-40";
  if (danger)
    return (
      <button
        onClick={onClick}
        disabled={disabled}
        className={`${base} border border-rose-400/25 bg-rose-500/10 text-rose-300 hover:bg-rose-500/20`}
      >
        {children}
      </button>
    );
  if (primary)
    return (
      <button
        onClick={onClick}
        disabled={disabled}
        className={`${base} bg-gradient-to-r from-indigo-500 to-cyan-500 text-white shadow-[0_0_24px_-8px_rgba(129,140,248,0.7)] hover:brightness-115`}
      >
        {children}
      </button>
    );
  return (
    <button
      onClick={onClick}
      disabled={disabled}
      className={`${base} border border-white/10 bg-white/[0.04] text-slate-200 hover:bg-white/[0.08]`}
    >
      {children}
    </button>
  );
}

export function ScreenHeader({
  title,
  count,
  actionLabel,
  onAction,
  secondaryLabel,
  onSecondary,
  secondaryBusy,
}: {
  title: string;
  count: number;
  actionLabel: string;
  onAction: () => void;
  secondaryLabel?: string;
  onSecondary?: () => void;
  secondaryBusy?: boolean;
}) {
  const { t } = useT();
  return (
    <div className="mb-5 flex items-center justify-between gap-3">
      <div>
        <h2 className="font-display text-lg font-semibold text-slate-100">{title}</h2>
        <p className="font-mono text-[10px] tracking-[0.2em] text-slate-600">{count}</p>
      </div>
      <div className="flex items-center gap-2">
        {secondaryLabel && onSecondary && (
          <button
            onClick={onSecondary}
            disabled={secondaryBusy}
            className="flex items-center gap-1.5 rounded-lg border border-white/10 bg-white/[0.04] px-2.5 py-2 text-[11px] text-slate-300 transition-colors hover:bg-white/[0.08] disabled:cursor-wait disabled:opacity-50"
          >
            <RefreshCw size={12} className={secondaryBusy ? "animate-spin" : ""} />
            {secondaryLabel}
          </button>
        )}
        <button
          onClick={onAction}
          className="flex items-center gap-2 rounded-lg bg-gradient-to-r from-indigo-500 to-cyan-500 px-3.5 py-2 text-[12px] font-semibold text-white shadow-[0_0_24px_-8px_rgba(129,140,248,0.7)] transition-all hover:brightness-115"
        >
          <Plus size={14} />
          {actionLabel}
        </button>
      </div>
      <span className="sr-only">{t("common.add")}</span>
    </div>
  );
}

const Row = ({ children }: { children: ReactNode }) => (
  <motion.div
    layout
    initial={{ opacity: 0, y: 8 }}
    animate={{ opacity: 1, y: 0 }}
    className="glass group flex items-center gap-3 rounded-xl px-3.5 py-3 transition-colors hover:border-white/[0.14]"
  >
    {children}
  </motion.div>
);

/* ------------------------------------------------------------------ skills */

// Keep a trailing hyphen while the user is typing (for example, "code-").
// The saved value is trimmed below, and the backend applies the same rule.
const canonicalSkillName = (value: string) =>
  unidecode(value)
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+/, "");
const savedCanonicalSkillName = (value: string) => canonicalSkillName(value).replace(/-+$/, "");

function SkillForm({
  initial,
  onSave,
  saveLabel,
}: {
  initial?: Skill;
  onSave: (v: { name: string; description: string; icon: IconKey; accent: AccentKey }) => void;
  saveLabel: string;
}) {
  const { t } = useT();
  const [name, setName] = useState(initial?.name ?? "");
  const [description, setDescription] = useState(initial?.description ?? "");
  const [icon, setIcon] = useState<IconKey>(initial?.icon ?? "code");
  const [accent, setAccent] = useState<AccentKey>(initial?.accent ?? "indigo");
  const valid = name.trim().length > 1;

  return (
    <div>
      <SField label={t("common.name")}>
        <input
          value={name}
          onChange={(e) => setName(canonicalSkillName(e.target.value))}
          placeholder="skill-name"
          spellCheck={false}
          autoCorrect="off"
          autoCapitalize="off"
          className={`${inputCls} disabled:cursor-not-allowed disabled:opacity-60`}
          disabled={Boolean(initial)}
        />
      </SField>
      <SField label={t("common.description")}>
        <textarea
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          rows={2}
          placeholder={t("skills.descPh")}
          className={`${inputCls} resize-none`}
        />
      </SField>
      <SField label={t("skills.icon")}>
        <div className="flex gap-1.5">
          {SKILL_ICON_KEYS.map((k) => {
            const I = SKILL_ICONS[k];
            const on = icon === k;
            return (
              <button
                key={k}
                onClick={() => setIcon(k)}
                className={`grid h-9 w-9 place-items-center rounded-lg border transition-all ${on ? "border-indigo-400/50 bg-indigo-400/[0.14] text-indigo-200" : "border-white/[0.08] text-slate-500 hover:bg-white/[0.05]"}`}
              >
                <I size={15} />
              </button>
            );
          })}
        </div>
      </SField>
      <SField label={t("skills.accent")}>
        <div className="flex gap-2">
          {(Object.keys(ACCENTS) as AccentKey[]).map((k) => (
            <button
              key={k}
              onClick={() => setAccent(k)}
              className={`h-7 w-7 rounded-full border-2 transition-transform ${accent === k ? "scale-110 border-white/70" : "border-transparent hover:scale-105"}`}
              style={{ background: ACCENTS[k].hex, boxShadow: accent === k ? `0 0 14px ${ACCENTS[k].hex}` : undefined }}
              aria-label={k}
            />
          ))}
        </div>
      </SField>
      <SBtn
        primary
        disabled={!valid}
        onClick={() => onSave({ name: savedCanonicalSkillName(name), description: description.trim(), icon, accent })}
      >
        <Check size={14} />
        {saveLabel}
      </SBtn>
    </div>
  );
}

function DiscoveredList({
  items,
  onToggle,
  onImport,
  importing,
}: {
  items: DiscoveredSkill[];
  onToggle: (id: string) => void;
  onImport: () => void;
  importing: boolean;
}) {
  const { t } = useT();
  const [filter, setFilter] = useState("");
  const checked = items.filter((i) => i.checked);
  const visibleItems = items.filter((item) => item.name.toLowerCase().includes(filter.trim().toLowerCase()));
  return (
    <div>
      <p className="mb-2 font-mono text-[9px] tracking-[0.22em] text-slate-600">
        {t("skills.found", { total: items.length, checked: checked.length })}
      </p>
      <div className="mb-2 flex items-center gap-1.5">
        <input
          value={filter}
          onChange={(event) => setFilter(event.target.value)}
          placeholder={t("skills.filterNamePh")}
          className={`${inputCls} min-w-0 flex-1`}
        />
        <button
          onClick={() =>
            visibleItems.forEach((item) => {
              if (!item.checked) onToggle(item.tempId);
            })
          }
          disabled={visibleItems.length === 0 || visibleItems.every((item) => item.checked)}
          className="grid h-8 w-8 shrink-0 place-items-center rounded-md border border-white/10 bg-white/[0.04] text-slate-300 hover:bg-white/[0.08] disabled:cursor-not-allowed disabled:opacity-40"
          title={t("skills.selectAll")}
          aria-label={t("skills.selectAll")}
        >
          <CheckCheck size={14} />
        </button>
        <button
          onClick={() =>
            visibleItems.forEach((item) => {
              if (item.checked) onToggle(item.tempId);
            })
          }
          disabled={!visibleItems.some((item) => item.checked)}
          className="grid h-8 w-8 shrink-0 place-items-center rounded-md border border-white/10 bg-white/[0.04] text-slate-300 hover:bg-white/[0.08] disabled:cursor-not-allowed disabled:opacity-40"
          title={t("skills.clearSelection")}
          aria-label={t("skills.clearSelection")}
        >
          <X size={14} />
        </button>
      </div>
      <div className="mb-4 max-h-64 space-y-1.5 overflow-y-auto scroll-slim">
        {visibleItems.map((it) => {
          const I = SKILL_ICONS[it.icon];
          return (
            <button
              key={it.tempId}
              onClick={() => onToggle(it.tempId)}
              className={`flex w-full items-center gap-2.5 rounded-lg border px-3 py-2 text-left transition-all ${it.checked ? "border-indigo-400/35 bg-indigo-400/[0.08]" : "border-white/[0.07] bg-white/[0.02] hover:bg-white/[0.05]"}`}
            >
              <span
                className={`grid h-4 w-4 shrink-0 place-items-center rounded border ${it.checked ? "border-indigo-300 bg-indigo-400/90 text-abyss-950" : "border-slate-600"}`}
              >
                {it.checked && <Check size={11} strokeWidth={3} />}
              </span>
              <I size={14} style={{ color: ACCENTS[it.accent].hex }} className="shrink-0" />
              <span className="min-w-0 flex-1">
                <span className="block truncate text-[12px] font-medium text-slate-200">{it.name}</span>
                <span className="block truncate text-[10px] text-slate-500">{it.description}</span>
              </span>
            </button>
          );
        })}
      </div>
      <SBtn primary disabled={checked.length === 0 || importing} onClick={onImport}>
        {importing ? <Loader2 size={14} className="animate-spin" /> : <Download size={14} />}
        {t("skills.importSelected", { n: checked.length })}
      </SBtn>
    </div>
  );
}

export function SkillsScreen({ profileId, patch, notify }: { profileId: string; patch: Patch; notify: Notify }) {
  const { t } = useT();
  const [importOpen, setImportOpen] = useState(false);
  const [tab, setTab] = useState<"manual" | "link" | "dir" | "group">("manual");
  const [ref, setRef] = useState("");
  const [scanning, setScanning] = useState(false);
  const [importing, setImporting] = useState(false);
  const [found, setFound] = useState<DiscoveredSkill[] | null>(null);
  const [editSkill, setEditSkill] = useState<Skill | null>(null);
  const [viewSkill, setViewSkill] = useState<{ skill: Skill; content: string } | null>(null);
  const [skillToDelete, setSkillToDelete] = useState<Skill | null>(null);
  const [reindexing, setReindexing] = useState(false);
  const [pageSkills, setPageSkills] = useState<Skill[]>([]);
  const [allSkills, setAllSkills] = useState<Skill[]>([]);
  const [paging, setPaging] = useState({ page: 0, cursors: [""], nextCursor: "", hasMore: false, total: 0 });
  const [query, setQuery] = useState("");
  const [groups, setGroups] = useState<SkillGroup[]>([]);
  const [groupDraft, setGroupDraft] = useState<SkillGroup | null>(null);
  const [groupFilter, setGroupFilter] = useState("");
  const [groupAdd, setGroupAdd] = useState<string | null>(null);
  const [groupToDelete, setGroupToDelete] = useState<SkillGroup | null>(null);

  const loadPage = async (cursor: string, page: number, search = query) => {
    const result = await wsRequest<SkillPage>(72, { limit: 20, cursor, query: search.trim() });
    const items = Array.isArray(result.items) ? result.items : [];
    setPageSkills(items);
    setPaging((current) => {
      const cursors = [...current.cursors];
      cursors[page] = cursor;
      return {
        page,
        cursors,
        nextCursor: result.nextCursor ?? "",
        hasMore: Boolean(result.hasMore),
        total: result.total ?? 0,
      };
    });
  };
  const loadGroups = async () => {
    try {
      const value = await wsRequest<SkillGroup[]>(111);
      setGroups(Array.isArray(value) ? value : []);
    } catch (error) {
      notify("err", error instanceof Error ? error.message : String(error));
    }
  };
  const loadAllSkills = async () => {
    try {
      const value = await wsRequest<Skill[]>(72);
      setAllSkills(Array.isArray(value) ? value : []);
    } catch (error) {
      notify("err", error instanceof Error ? error.message : String(error));
    }
  };
  useEffect(() => {
    void loadGroups();
    void loadAllSkills();
  }, [profileId]);
  const saveGroup = async () => {
    if (!groupDraft?.name.trim()) return;
    try {
      const saved = await wsRequest<SkillGroup>(112, {
        ...groupDraft,
        name: groupDraft.name.trim(),
        description: groupDraft.description.trim(),
      });
      setGroups((items) => {
        const exists = items.some((x) => x.id === saved.id);
        return exists ? items.map((x) => (x.id === saved.id ? saved : x)) : [...items, saved];
      });
      setGroupDraft(null);
    } catch (error) {
      notify("err", error instanceof Error ? error.message : String(error));
    }
  };
  const deleteGroup = async (id: string) => {
    try {
      await wsRequest(113, { id });
      setGroups((items) => items.filter((x) => x.id !== id));
    } catch (error) {
      notify("err", error instanceof Error ? error.message : String(error));
    }
  };
  const assignSkill = async (skillId: string, groupId: string) => {
    try {
      await wsRequest(114, { skillId, groupId });
      await loadGroups();
      setGroupAdd(null);
    } catch (error) {
      notify("err", error instanceof Error ? error.message : String(error));
    }
  };
  const removeSkillFromGroup = async (skillId: string) => {
    try {
      await wsRequest(114, { skillId, groupId: "" });
      await loadGroups();
    } catch (error) {
      notify("err", error instanceof Error ? error.message : String(error));
    }
  };
  const refresh = async () => {
    const all = await wsRequest<Skill[]>(72);
    const skills = Array.isArray(all) ? all : [];
    patch({ skills });
    setAllSkills(skills);
    await loadPage("", 0);
  };
  useEffect(() => {
    const timer = window.setTimeout(
      () => {
        void loadPage("", 0).catch((error) => notify("err", error instanceof Error ? error.message : String(error)));
      },
      query ? 250 : 0,
    );
    return () => window.clearTimeout(timer);
  }, [profileId, query]);
  const changePage = async (direction: -1 | 1) => {
    const page = paging.page + direction;
    if (page < 0 || (direction > 0 && !paging.hasMore)) return;
    await loadPage(direction > 0 ? paging.nextCursor : paging.cursors[page], page);
  };

  const scan = async () => {
    if (!ref.trim()) return;
    setScanning(true);
    setFound(null);
    try {
      const items = await wsRequest<DiscoveredSkill[]>(31, { source: "link", ref: ref.trim() });
      setFound(Array.isArray(items) ? items : []);
    } catch (error) {
      notify("err", error instanceof Error ? error.message : String(error));
    } finally {
      setScanning(false);
    }
  };

  const scanDir = async () => {
    const folder = await pickSkillFolder();
    if (!folder) return;
    setScanning(true);
    setFound(null);
    setRef(folder);
    try {
      const items = await wsRequest<DiscoveredSkill[]>(31, { source: "directory", ref: folder });
      setFound(Array.isArray(items) ? items : []);
    } catch (error) {
      notify("err", error instanceof Error ? error.message : String(error));
    } finally {
      setScanning(false);
    }
  };

  const doImport = async (source: Skill["source"]) => {
    if (!found) return;
    setImporting(true);
    try {
      const items = found
        .filter((f) => f.checked)
        .map((item) => ({ ...item, name: savedCanonicalSkillName(item.name) || item.name }));
      await wsRequest(32, { items, source });
      setImportOpen(false);
      setFound(null);
      setRef("");
      await refresh();
      notify("ok", t("skills.imported", { n: items.length }));
    } catch (error) {
      notify("err", error instanceof Error ? error.message : String(error));
    } finally {
      setImporting(false);
    }
  };

  const sourceLabel: Record<Skill["source"], string> = {
    manual: t("skills.manualSrc"),
    link: t("skills.linkSrc"),
    directory: t("skills.dirSrc"),
    aggregate: t("skills.aggregateSrc"),
  };
  return (
    <div className="mx-auto max-w-3xl">
      <ScreenHeader
        title={t("skills.title")}
        count={paging.total}
        actionLabel={t("skills.add")}
        onAction={() => {
          setImportOpen(true);
          setTab("manual");
          setFound(null);
          setRef("");
          setGroupDraft(null);
        }}
        secondaryLabel={t("skills.reindex")}
        secondaryBusy={reindexing}
        onSecondary={() => {
          setReindexing(true);
          void wsRequest<{ count: number }>(104)
            .then(async (result) => {
              await refresh();
              notify("ok", t("skills.reindexDone", { n: result.count }));
            })
            .catch((error) => notify("err", error instanceof Error ? error.message : String(error)))
            .finally(() => setReindexing(false));
        }}
      />

      <div className="mb-4 rounded-xl border border-white/[0.08] bg-white/[0.02] p-3">
        <div className="mb-2 flex items-center justify-between">
          <span className="font-display text-sm text-slate-200">{t("skills.groupsTitle")}</span>
        </div>
        <div className="space-y-2">
          {groups.map((group) => (
            <div key={group.id} className="rounded-lg border border-white/[0.07] p-2">
              <div className="flex items-center gap-2">
                <div className="min-w-0 flex-1">
                  <span className="block truncate text-xs text-slate-200">{group.name}</span>
                  <span className="block truncate text-[10px] text-slate-500">
                    {group.description || t("skills.noDescription")} · {group.skillIds.length} {t("skills.skillCount")}
                  </span>
                </div>
                <SToggle
                  on={group.applyAuto}
                  onChange={(value) => void wsRequest(112, { ...group, applyAuto: value }).then(() => loadGroups())}
                  rgb="34,211,238"
                />
                <button
                  className="grid h-7 w-7 place-items-center rounded-lg text-slate-500 transition-colors hover:bg-white/[0.07] hover:text-indigo-300"
                  onClick={() => {
                    setGroupDraft(group);
                    setGroupFilter("");
                    setTab("group");
                    setImportOpen(true);
                  }}
                  title={t("common.edit")}
                >
                  <Pencil size={13} />
                </button>
                <button
                  className="grid h-7 w-7 place-items-center rounded-lg text-slate-500 transition-colors hover:bg-rose-500/15 hover:text-rose-300"
                  onClick={() => setGroupToDelete(group)}
                  title={t("common.delete")}
                >
                  <Trash2 size={13} />
                </button>
              </div>
              <div className="mt-2 flex flex-wrap gap-1">
                {group.skillIds.map((id) => (
                  <span
                    key={id}
                    className="inline-flex items-center gap-1 rounded bg-indigo-400/10 py-1 pl-1.5 pr-0.5 text-[10px] text-indigo-200"
                  >
                    {allSkills.find((s) => s.id === id)?.name ?? id}
                    <button
                      className="grid h-4 w-4 place-items-center rounded text-indigo-200/60 hover:bg-rose-500/15 hover:text-rose-300"
                      onClick={() => void removeSkillFromGroup(id)}
                      title={t("skills.groupRemoveSkill")}
                    >
                      <X size={10} />
                    </button>
                  </span>
                ))}
                <button
                  className="rounded border border-dashed border-white/10 px-1.5 py-1 text-[10px] text-slate-500"
                  onClick={() => {
                    setGroupAdd(group.id);
                    setGroupFilter("");
                  }}
                >
                  + {t("skills.groupAddSkills")}
                </button>
              </div>
            </div>
          ))}
        </div>
        {groupAdd && (
          <SModal
            title={t("skills.groupAddTitle")}
            subtitle={t("skills.groupAddSub")}
            onClose={() => setGroupAdd(null)}
          >
            <input
              autoFocus
              value={groupFilter}
              onChange={(e) => setGroupFilter(e.target.value)}
              placeholder={t("skills.groupFilterPh")}
              className={`${inputCls} mb-2`}
            />
            <div className="max-h-40 space-y-1 overflow-y-auto">
              {allSkills
                .filter(
                  (s) =>
                    !groups.find((g) => g.id === groupAdd)?.skillIds.includes(s.id) &&
                    `${s.name} ${s.description}`.toLowerCase().includes(groupFilter.toLowerCase()),
                )
                .map((skill) => (
                  <button
                    key={skill.id}
                    className="block w-full rounded px-2 py-1 text-left text-xs text-slate-300 hover:bg-white/[0.06]"
                    onClick={() => void assignSkill(skill.id, groupAdd)}
                  >
                    {skill.name}
                  </button>
                ))}
            </div>
          </SModal>
        )}
        {groupToDelete && (
          <SModal
            title={t("skills.groupDeleteTitle")}
            subtitle={groupToDelete.name}
            onClose={() => setGroupToDelete(null)}
          >
            <p className="text-[12px] leading-relaxed text-slate-400">{t("skills.groupDeleteConfirm")}</p>
            <div className="mt-4 flex justify-end gap-2">
              <SBtn onClick={() => setGroupToDelete(null)}>{t("common.cancel")}</SBtn>
              <SBtn danger onClick={() => void deleteGroup(groupToDelete.id).then(() => setGroupToDelete(null))}>
                <Trash2 size={14} />
                {t("common.delete")}
              </SBtn>
            </div>
          </SModal>
        )}
      </div>

      <div className="relative mb-3">
        <Search className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-slate-600" size={14} />
        <input
          value={query}
          onChange={(event) => setQuery(event.target.value)}
          placeholder={t("skills.searchPh")}
          className={`${inputCls} pl-9 pr-9`}
        />
        {query && (
          <button
            onClick={() => setQuery("")}
            className="absolute right-2 top-1/2 grid h-6 w-6 -translate-y-1/2 place-items-center rounded-md text-slate-500 hover:bg-white/[0.06] hover:text-slate-200"
            title={t("skills.clearSearch")}
          >
            <X size={13} />
          </button>
        )}
      </div>

      <div className="space-y-2">
        {pageSkills.map((s) => {
          const I = SKILL_ICONS[s.icon] ?? SKILL_ICONS.bot;
          const a = ACCENTS[s.accent] ?? ACCENTS.indigo;
          return (
            <Row key={s.id}>
              <span
                className="grid h-9 w-9 shrink-0 place-items-center rounded-lg"
                style={{ background: `rgba(${a.rgb},0.12)`, boxShadow: `inset 0 0 0 1px rgba(${a.rgb},0.25)` }}
              >
                <I size={15} style={{ color: a.hex }} />
              </span>
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2">
                  <span
                    className={`truncate text-[13px] font-medium ${s.enabled ? "text-slate-100" : "text-slate-500"}`}
                  >
                    {s.name}
                  </span>
                  <span className="rounded border border-white/[0.08] bg-white/[0.03] px-1.5 py-px font-mono text-[8.5px] uppercase tracking-wider text-slate-500">
                    {sourceLabel[s.source] ?? sourceLabel.directory}
                  </span>
                </div>
                <p className="truncate text-[11px] text-slate-500">{s.description}</p>
              </div>
              <button
                onClick={() =>
                  void wsRequest<string>(73, { name: s.id })
                    .then((content) => setEditSkill({ ...s, content }))
                    .catch((error) => notify("err", error instanceof Error ? error.message : String(error)))
                }
                className="grid h-7 w-7 place-items-center rounded-lg text-slate-500 transition-colors hover:bg-white/[0.07] hover:text-indigo-300"
                title={t("common.edit")}
              >
                <Pencil size={13} />
              </button>
              <button
                onClick={() =>
                  void wsRequest(107, { id: s.id }).catch((error) =>
                    notify("err", error instanceof Error ? error.message : String(error)),
                  )
                }
                className="grid h-7 w-7 place-items-center rounded-lg text-slate-500 transition-colors hover:bg-white/[0.07] hover:text-cyan-300"
                title={t("skills.openFolder")}
              >
                <FolderOpen size={13} />
              </button>
              <button
                onClick={() =>
                  void wsRequest<string>(73, { name: s.id })
                    .then((content) => setViewSkill({ skill: s, content }))
                    .catch((error) => notify("err", error instanceof Error ? error.message : String(error)))
                }
                className="grid h-7 w-7 place-items-center rounded-lg text-slate-500 transition-colors hover:bg-white/[0.07] hover:text-cyan-300"
                title={t("skills.view")}
              >
                <Eye size={13} />
              </button>
              <button
                onClick={() => setSkillToDelete(s)}
                className="grid h-7 w-7 place-items-center rounded-lg text-slate-500 transition-colors hover:bg-rose-500/15 hover:text-rose-300"
                title={t("common.delete")}
              >
                <Trash2 size={13} />
              </button>
            </Row>
          );
        })}
        {pageSkills.length === 0 && <p className="py-8 text-center text-[12px] text-slate-600">{t("skills.empty")}</p>}
      </div>
      {paging.total > 0 && (
        <div className="mt-4 flex items-center justify-end gap-2 font-mono text-[10px] text-slate-500">
          <span>{paging.page + 1}</span>
          <button
            disabled={paging.page === 0}
            onClick={() => void changePage(-1)}
            className="grid h-7 w-7 place-items-center rounded-md border border-white/10 hover:bg-white/[0.06] disabled:opacity-30"
            title={t("common.previous")}
          >
            <ChevronLeft size={14} />
          </button>
          <button
            disabled={!paging.hasMore}
            onClick={() => void changePage(1)}
            className="grid h-7 w-7 place-items-center rounded-md border border-white/10 hover:bg-white/[0.06] disabled:opacity-30"
            title={t("common.next")}
          >
            <ChevronRight size={14} />
          </button>
        </div>
      )}

      {importOpen && (
        <SModal
          title={t("skills.addTitle")}
          subtitle={t("skills.addSub")}
          onClose={() => {
            setImportOpen(false);
            setGroupDraft(null);
          }}
        >
          <div className="mb-4 grid grid-cols-4 gap-1">
            {[
              { k: "manual" as const, label: t("skills.tabManual"), icon: Pencil },
              { k: "link" as const, label: t("skills.tabLink"), icon: Link2 },
              { k: "dir" as const, label: t("skills.tabDir"), icon: FolderOpen },
              { k: "group" as const, label: t("skills.tabGroup"), icon: Boxes },
            ].map((tb) => (
              <button
                key={tb.k}
                onClick={() => {
                  setTab(tb.k);
                  setFound(null);
                  if (tb.k === "group" && !groupDraft)
                    setGroupDraft({ id: "", profileId, name: "", description: "", applyAuto: false, skillIds: [] });
                }}
                className={`flex min-w-0 items-center justify-center gap-1 rounded-lg border px-1 py-1.5 text-[10px] font-medium transition-all ${tab === tb.k ? "border-indigo-400/45 bg-indigo-400/[0.1] text-indigo-200" : "border-white/[0.08] text-slate-400 hover:bg-white/[0.04]"}`}
              >
                <tb.icon size={12} />
                {tb.label}
              </button>
            ))}
          </div>

          {tab === "manual" && (
            <SkillForm
              saveLabel={t("skills.create")}
              onSave={async (v) => {
                try {
                  await wsRequest(28, { ...v, enabled: true, source: "manual" });
                  setImportOpen(false);
                  await refresh();
                  notify("ok", t("skills.created", { name: v.name }));
                } catch (error) {
                  notify("err", error instanceof Error ? error.message : String(error));
                }
              }}
            />
          )}

          {tab === "group" && groupDraft && (
            <div>
              <SField label={t("common.name")}>
                <input
                  value={groupDraft.name}
                  onChange={(e) => setGroupDraft({ ...groupDraft, name: e.target.value })}
                  placeholder={t("skills.groupNamePh")}
                  className={inputCls}
                />
              </SField>
              <SField label={t("common.description")}>
                <textarea
                  value={groupDraft.description}
                  onChange={(e) => setGroupDraft({ ...groupDraft, description: e.target.value })}
                  rows={2}
                  placeholder={t("skills.groupDescPh")}
                  className={`${inputCls} resize-none`}
                />
              </SField>
              <div className="mb-3 flex items-center justify-between rounded-lg border border-white/[0.07] bg-white/[0.02] px-3 py-2">
                <span className="text-[11.5px] text-slate-300">{t("skills.applyAuto")}</span>
                <SToggle
                  on={groupDraft.applyAuto}
                  onChange={(value) => setGroupDraft({ ...groupDraft, applyAuto: value })}
                  rgb="34,211,238"
                />
              </div>
              <SField label={t("skills.groupSkills")}>
                <input
                  value={groupFilter}
                  onChange={(e) => setGroupFilter(e.target.value)}
                  placeholder={t("skills.groupFilterPh")}
                  className={`${inputCls} mb-2`}
                />
                <div className="max-h-52 space-y-1 overflow-y-auto scroll-slim rounded-lg border border-white/[0.07] bg-abyss-900/50 p-2">
                  {allSkills
                    .filter((s) => `${s.name} ${s.description}`.toLowerCase().includes(groupFilter.toLowerCase()))
                    .map((skill) => {
                      const selected = groupDraft.skillIds.includes(skill.id);
                      const I = SKILL_ICONS[skill.icon] ?? SKILL_ICONS.bot;
                      const accent = ACCENTS[skill.accent] ?? ACCENTS.indigo;
                      return (
                        <button
                          key={skill.id}
                          className={`flex w-full min-w-0 items-center gap-2.5 rounded-md px-2 py-1.5 text-left transition-colors ${selected ? "bg-violet-400/[0.1]" : "hover:bg-white/[0.04]"}`}
                          onClick={() =>
                            setGroupDraft({
                              ...groupDraft,
                              skillIds: selected
                                ? groupDraft.skillIds.filter((id) => id !== skill.id)
                                : [...groupDraft.skillIds, skill.id],
                            })
                          }
                        >
                          <span
                            className={`grid h-4 w-4 shrink-0 place-items-center rounded border ${selected ? "border-violet-300 bg-violet-400/90 text-abyss-950" : "border-slate-600"}`}
                          >
                            {selected && <Check size={11} strokeWidth={3} />}
                          </span>
                          <I size={13} className="shrink-0" style={{ color: accent.hex }} />
                          <span className="min-w-0 flex-1">
                            <span
                              className={`block truncate text-[12px] ${selected ? "text-slate-200" : "text-slate-400"}`}
                            >
                              {skill.name}
                            </span>
                            <span className="block truncate text-[10px] text-slate-600">{skill.description}</span>
                          </span>
                        </button>
                      );
                    })}
                </div>
              </SField>
              <SBtn
                primary
                disabled={!groupDraft.name.trim()}
                onClick={async () => {
                  await saveGroup();
                  setImportOpen(false);
                  setGroupDraft(null);
                }}
              >
                {t("skills.groupCreate")}
              </SBtn>
            </div>
          )}

          {tab === "link" && (
            <div>
              <p className="mb-2 text-[11.5px] leading-relaxed text-slate-500">{t("skills.linkHint")}</p>
              <div className="mb-3 flex gap-2">
                <input
                  value={ref}
                  onChange={(e) => setRef(e.target.value)}
                  onKeyDown={(e) => e.key === "Enter" && void scan()}
                  placeholder={t("skills.refPh")}
                  className={inputCls}
                />
                <SBtn onClick={() => void scan()} disabled={!ref.trim() || scanning}>
                  {scanning ? <Loader2 size={13} className="animate-spin" /> : <ScanLine size={13} />}
                  {scanning ? t("skills.scanning") : t("skills.scan")}
                </SBtn>
              </div>
              {found && (
                <DiscoveredList
                  items={found}
                  importing={importing}
                  onToggle={(id) =>
                    setFound((f) => f?.map((x) => (x.tempId === id ? { ...x, checked: !x.checked } : x)) ?? null)
                  }
                  onImport={() => void doImport("link")}
                />
              )}
            </div>
          )}

          {tab === "dir" && (
            <div>
              <p className="mb-3 text-[11.5px] leading-relaxed text-slate-500">{t("skills.dirHint")}</p>
              <SBtn onClick={() => void scanDir()} disabled={scanning}>
                {scanning ? <Loader2 size={13} className="animate-spin" /> : <FolderOpen size={13} />}
                {scanning ? t("skills.scanning") : t("skills.chooseDir")}
              </SBtn>
              {ref && <p className="mt-2 truncate font-mono text-[10px] text-slate-600">{ref}</p>}
              {found && (
                <div className="mt-3">
                  <DiscoveredList
                    items={found}
                    importing={importing}
                    onToggle={(id) =>
                      setFound((f) => f?.map((x) => (x.tempId === id ? { ...x, checked: !x.checked } : x)) ?? null)
                    }
                    onImport={() => void doImport("directory")}
                  />
                </div>
              )}
            </div>
          )}
        </SModal>
      )}

      {skillToDelete && (
        <SModal title={t("skills.deleteTitle")} subtitle={skillToDelete.name} onClose={() => setSkillToDelete(null)}>
          <p className="text-[12px] leading-relaxed text-slate-400">{t("skills.deleteConfirm")}</p>
          <div className="mt-4 flex justify-end gap-2">
            <SBtn onClick={() => setSkillToDelete(null)}>{t("common.cancel")}</SBtn>
            <SBtn
              danger
              onClick={() =>
                void wsRequest(30, { id: skillToDelete.id })
                  .then(async () => {
                    await refresh();
                    setSkillToDelete(null);
                    notify("info", t("skills.deleted", { name: skillToDelete.name }));
                  })
                  .catch((error) => notify("err", error instanceof Error ? error.message : String(error)))
              }
            >
              <Trash2 size={14} />
              {t("common.delete")}
            </SBtn>
          </div>
        </SModal>
      )}

      {viewSkill && (
        <SModal
          title={viewSkill.skill.name}
          subtitle={t("skills.view")}
          onClose={() => setViewSkill(null)}
          w="max-w-3xl"
          layer={100}
        >
          <div className="markdown-content max-w-none text-[13px] leading-6 text-slate-300 [&_a]:text-cyan-300 [&_blockquote]:border-l-2 [&_blockquote]:border-indigo-400/40 [&_blockquote]:pl-3 [&_code]:font-mono [&_h1]:mb-3 [&_h1]:font-display [&_h1]:text-xl [&_h1]:font-semibold [&_h1]:text-slate-100 [&_h2]:mb-2 [&_h2]:mt-5 [&_h2]:font-display [&_h2]:text-lg [&_h2]:font-semibold [&_h2]:text-slate-100 [&_h3]:mb-2 [&_h3]:mt-4 [&_h3]:font-display [&_h3]:font-semibold [&_h3]:text-slate-100 [&_li]:ml-5 [&_li]:list-disc [&_ol]:my-2 [&_ol]:list-decimal [&_p]:my-2 [&_pre]:my-3 [&_pre]:overflow-x-auto [&_pre]:rounded-lg [&_pre]:border [&_pre]:border-white/[0.08] [&_pre]:bg-black/20 [&_pre]:p-3 [&_table]:my-3 [&_table]:w-full [&_td]:border [&_td]:border-white/10 [&_td]:p-2 [&_th]:border [&_th]:border-white/10 [&_th]:bg-white/[0.04] [&_th]:p-2 [&_ul]:my-2 [&_ul]:list-disc">
            <ReactMarkdown remarkPlugins={[remarkGfm]}>{viewSkill.content}</ReactMarkdown>
          </div>
        </SModal>
      )}

      {editSkill && (
        <SModal
          title={t("skills.editTitle")}
          subtitle={editSkill.name.toUpperCase()}
          onClose={() => setEditSkill(null)}
        >
          <SkillForm
            initial={editSkill}
            saveLabel={t("skills.saveChanges")}
            onSave={async (v) => {
              try {
                await wsRequest(29, { id: editSkill.id, patch: v });
                setEditSkill(null);
                await refresh();
                notify("ok", t("skills.updated"));
              } catch (error) {
                notify("err", error instanceof Error ? error.message : String(error));
              }
            }}
          />
        </SModal>
      )}
    </div>
  );
}

/* --------------------------------------------------------------- providers */

function ProviderModal({
  initial,
  proxies,
  onClose,
  onSaved,
  notify,
}: {
  initial: Provider | null;
  proxies: Proxy[];
  onClose: () => void;
  onSaved: () => Promise<void>;
  notify: Notify;
}) {
  const { t } = useT();
  const [name, setName] = useState(initial?.name ?? "");
  const [kind, setKind] = useState<Provider["kind"]>(initial?.kind ?? "openai");
  const [baseUrl, setBaseUrl] = useState(initial?.baseUrl ?? "");
  const [apiKey, setApiKey] = useState(initial?.apiKey ?? "");
  const [enabled, setEnabled] = useState(initial?.enabled ?? true);
  const [showKey, setShowKey] = useState(false);
  const [clearAPIKey, setClearAPIKey] = useState(false);
  const [proxyId, setProxyId] = useState(initial?.proxyId ?? "");
  const [rpm, setRPM] = useState(initial?.rpm ?? 0);
  const [models, setModels] = useState<{ name: string; checked: boolean }[] | null>(
    initial
      ? [...(initial.models ?? [])]
          .sort((a, b) => a.localeCompare(b, undefined, { sensitivity: "base" }))
          .map((m) => ({ name: m, checked: true }))
      : null,
  );
  const [fetching, setFetching] = useState(false);
  const [saving, setSaving] = useState(false);
  const [validationError, setValidationError] = useState("");
  const initializedKind = useRef(false);
  const valid = Boolean(name.trim() && baseUrl.trim());

  const providerDefaults: Record<Provider["kind"], { name: string; baseUrl: string }> = {
    openai: { name: "OpenAI", baseUrl: "https://api.openai.com/v1" },
    anthropic: { name: "Anthropic", baseUrl: "https://api.anthropic.com/v1" },
    ollama: { name: "Ollama", baseUrl: "http://127.0.0.1:11434/v1" },
    openrouter: { name: "OpenRouter", baseUrl: "https://openrouter.ai/api/v1" },
    deepseek: { name: "DeepSeek", baseUrl: "https://api.deepseek.com/v1" },
    mistral: { name: "Mistral", baseUrl: "https://api.mistral.ai/v1" },
    groq: { name: "Groq", baseUrl: "https://api.groq.com/openai/v1" },
    xai: { name: "xAI", baseUrl: "https://api.x.ai/v1" },
    google: { name: "Google Gemini", baseUrl: "https://generativelanguage.googleapis.com/v1beta/openai" },
    custom: { name: "Custom", baseUrl: "" },
  };

  useEffect(() => {
    if (!initializedKind.current) {
      initializedKind.current = true;
      return;
    }
    const defaults = providerDefaults[kind];
    setName(defaults.name);
    setBaseUrl(defaults.baseUrl);
    setApiKey(kind === "ollama" ? "sk-ollama" : "");
    setClearAPIKey(false);
    setModels(null);
    setValidationError("");
  }, [kind]);

  const validate = () => {
    if (!name.trim()) return t("providers.validationName");
    if (!baseUrl.trim()) return t("providers.validationBaseUrl");
    try {
      const parsed = new URL(baseUrl.trim());
      if (!["http:", "https:"].includes(parsed.protocol) || !parsed.hostname) return t("providers.validationBaseUrl");
    } catch {
      return t("providers.validationBaseUrl");
    }
    return "";
  };

  const fetchModels = async () => {
    setFetching(true);
    try {
      if (!baseUrl.trim()) {
        notify("err", "Base URL is required");
        return;
      }
      const res = await wsRequest<{ models: string[] }>(89, { id: initial?.id, kind, baseUrl: baseUrl.trim(), apiKey });
      const sortedModels = [...(res.models ?? [])].sort((a, b) =>
        a.localeCompare(b, undefined, { sensitivity: "base" }),
      );
      setModels(sortedModels.map((m) => ({ name: m, checked: initial?.models.includes(m) ?? false })));
      notify("info", t("providers.fetched", { n: res.models.length }));
    } catch (error) {
      notify("err", error instanceof Error ? error.message : "Failed to fetch models");
    } finally {
      setFetching(false);
    }
  };

  const save = async () => {
    const error = validate();
    setValidationError(error);
    if (error) return;
    setSaving(true);
    try {
      const payload = {
        name: name.trim(),
        kind,
        baseUrl: baseUrl.trim(),
        apiKey: clearAPIKey ? "" : apiKey,
        enabled,
        models: models?.filter((m) => m.checked).map((m) => m.name) ?? initial?.models ?? [],
        proxyId,
        rpm,
      };
      if (initial) await wsRequest(86, { id: initial.id, patch: payload, clearApiKey: clearAPIKey });
      else await wsRequest(85, payload);
      await onSaved();
      notify("ok", initial ? t("providers.updated") : t("providers.added", { name }));
      onClose();
    } catch (error) {
      setValidationError(error instanceof Error ? error.message : t("providers.saveError"));
    } finally {
      setSaving(false);
    }
  };

  return (
    <SModal
      title={initial ? t("providers.editTitle") : t("providers.newTitle")}
      subtitle={t("providers.sub")}
      onClose={onClose}
      footer={
        <div className="flex w-full items-center justify-between gap-3">
          <div className="flex items-center gap-2">
            <span className="text-[12px] text-slate-300">{t("providers.active")}</span>
            <SToggle on={enabled} onChange={setEnabled} />
          </div>
          <div className="flex items-center gap-2">
            <SBtn onClick={onClose}>{t("common.cancel")}</SBtn>
            <SBtn primary disabled={!valid || saving} onClick={() => void save()}>
              {saving ? <Loader2 size={13} className="animate-spin" /> : <Check size={13} />}
              {t("common.save")}
            </SBtn>
          </div>
        </div>
      }
    >
      <div className="grid grid-cols-[1fr_140px] gap-3">
        <SField label={t("common.name")}>
          <input value={name} onChange={(e) => setName(e.target.value)} placeholder="OpenAI" className={inputCls} />
        </SField>
        <SField label={t("providers.kind")}>
          <select value={kind} onChange={(e) => setKind(e.target.value as Provider["kind"])} className={inputCls}>
            <option value="openai">OpenAI</option>
            <option value="openrouter">OpenRouter</option>
            <option value="deepseek">DeepSeek</option>
            <option value="mistral">Mistral</option>
            <option value="groq">Groq</option>
            <option value="xai">xAI</option>
            <option value="google">Google Gemini</option>
            <option value="anthropic">Anthropic</option>
            <option value="ollama">Ollama</option>
            <option value="custom">Custom</option>
          </select>
        </SField>
      </div>
      <SField label={t("providers.baseUrl")}>
        <input
          type="text"
          value={baseUrl}
          onChange={(e) => setBaseUrl(e.target.value)}
          placeholder="https://api.openai.com/v1"
          spellCheck={false}
          autoCapitalize="none"
          autoCorrect="off"
          className={`${inputCls} font-sans text-[12px] tracking-normal`}
        />
      </SField>
      <div className="grid grid-cols-[minmax(0,1fr)_140px] gap-3">
        <SField label={t("providers.proxy")}>
          <select value={proxyId} onChange={(e) => setProxyId(e.target.value)} className={inputCls}>
            <option value="">{t("providers.noProxy")}</option>
            {proxies.map((p) => (
              <option key={p.id} value={p.id}>
                {p.name} · {p.type.toUpperCase()} · {p.host}:{p.port}
              </option>
            ))}
          </select>
        </SField>
        <SField label={t("providers.rpm")}>
          <input
            type="number"
            min="0"
            step="1"
            value={rpm}
            onChange={(e) => setRPM(Math.max(0, Number(e.target.value) || 0))}
            placeholder="0"
            title={t("providers.rpmHint")}
            className={inputCls}
          />
        </SField>
      </div>
      {validationError && (
        <p className="mb-3 rounded-md border border-rose-400/25 bg-rose-400/10 px-3 py-2 text-[11px] text-rose-300">
          {validationError}
        </p>
      )}
      <SField label={t("providers.apiKey")}>
        <div className="flex gap-2">
          <div className="relative min-w-0 flex-1">
            <input
              value={apiKey}
              onChange={(e) => {
                setApiKey(e.target.value);
                setClearAPIKey(false);
              }}
              type={showKey ? "text" : "password"}
              placeholder={initial?.hasApiKey && !clearAPIKey ? t("providers.keySaved") : t("providers.keyPh")}
              className={`${inputCls} pr-9 font-mono text-[11.5px]`}
            />
            <button
              onClick={() => setShowKey((v) => !v)}
              className="absolute right-2 top-1/2 -translate-y-1/2 text-slate-500 hover:text-slate-200"
              aria-label={t("providers.apiKey")}
            >
              {showKey ? <EyeOff size={14} /> : <Eye size={14} />}
            </button>
          </div>
          {initial && (
            <button
              type="button"
              onClick={() => {
                setApiKey("");
                setClearAPIKey(true);
              }}
              title={t("providers.resetKey")}
              aria-label={t("providers.resetKey")}
              className="grid h-9 w-9 shrink-0 place-items-center rounded-md border border-rose-400/25 text-rose-300 hover:bg-rose-400/10"
            >
              <KeyRound size={14} />
            </button>
          )}
        </div>
      </SField>
      <div className="rounded-lg border border-white/[0.07] bg-abyss-900/50 p-3">
        <div className="mb-2 flex items-center justify-between">
          <span className="text-[11.5px] font-medium text-slate-300">{t("providers.models")}</span>
          <SBtn onClick={() => void fetchModels()} disabled={fetching}>
            {fetching ? <Loader2 size={12} className="animate-spin" /> : <RefreshCw size={12} />}
            {models ? t("providers.refresh") : t("providers.fetch")}
          </SBtn>
        </div>
        {models === null && <p className="py-2 text-[11px] text-slate-600">{t("providers.fetchHint")}</p>}
        {models && (
          <div className="max-h-44 space-y-1 overflow-y-auto scroll-slim">
            {models.map((m) => (
              <button
                key={m.name}
                onClick={() =>
                  setModels((list) => list?.map((x) => (x.name === m.name ? { ...x, checked: !x.checked } : x)) ?? null)
                }
                className={`flex w-full items-center gap-2.5 rounded-md border px-2.5 py-1.5 transition-all ${m.checked ? "border-cyan-400/35 bg-cyan-400/[0.07]" : "border-white/[0.06] hover:bg-white/[0.04]"}`}
              >
                <span
                  className={`grid h-4 w-4 place-items-center rounded border ${m.checked ? "border-cyan-300 bg-cyan-400/90 text-abyss-950" : "border-slate-600"}`}
                >
                  {m.checked && <Check size={11} strokeWidth={3} />}
                </span>
                <span className="font-mono text-[11.5px] text-slate-300">{m.name}</span>
              </button>
            ))}
          </div>
        )}
      </div>
    </SModal>
  );
}

export function ProvidersScreen({
  providers,
  proxies,
  patch,
  notify,
}: {
  providers: Provider[];
  proxies: Proxy[];
  patch: Patch;
  notify: Notify;
}) {
  const { t } = useT();
  const [modal, setModal] = useState<{ open: boolean; editing: Provider | null }>({ open: false, editing: null });
  const [testing, setTesting] = useState<string | null>(null);
  const refresh = async () => patch({ providers: await wsRequest<Provider[]>(84) });

  const test = async (p: Provider) => {
    setTesting(p.id);
    try {
      const res = await wsRequest<{ ok: boolean; latency: number }>(90, { id: p.id });
      notify("ok", t("providers.testOk", { name: p.name, ms: res.latency }));
    } catch (error) {
      notify(
        "err",
        `${t("providers.testFail", { name: p.name })}: ${error instanceof Error ? error.message : String(error)}`,
      );
    } finally {
      setTesting(null);
    }
  };

  return (
    <div className="mx-auto max-w-3xl">
      <ScreenHeader
        title={t("providers.title")}
        count={providers.length}
        actionLabel={t("providers.add")}
        onAction={() => setModal({ open: true, editing: null })}
      />
      <div className="space-y-2">
        {providers.map((p) => (
          <Row key={p.id}>
            <span
              className={`grid h-9 w-9 shrink-0 place-items-center rounded-lg ${p.enabled ? "bg-cyan-400/[0.1] text-cyan-300" : "bg-white/[0.04] text-slate-600"}`}
              style={p.enabled ? { boxShadow: "inset 0 0 0 1px rgba(34,211,238,0.25)" } : undefined}
            >
              <Boxes size={15} />
            </span>
            <div className="min-w-0 flex-1">
              <div className="flex flex-wrap items-center gap-2">
                <span className={`text-[13px] font-medium ${p.enabled ? "text-slate-100" : "text-slate-500"}`}>
                  {p.name}
                </span>
                <span className="rounded border border-white/[0.08] bg-white/[0.03] px-1.5 py-px font-mono text-[8.5px] uppercase tracking-wider text-slate-500">
                  {p.kind}
                </span>
                {p.latency !== undefined && p.enabled && (
                  <span className="font-mono text-[9px] text-emerald-400/80">
                    ~{p.latency} {t("panel.ms")}
                  </span>
                )}
              </div>
              <p className="truncate font-mono text-[10px] text-slate-600">
                {p.baseUrl} · {p.apiKey ? `••••${p.apiKey.slice(-4)}` : p.hasApiKey ? "••••" : t("providers.noKey")}
              </p>
              <div className="mt-1 flex flex-wrap gap-1">
                {p.models.map((m) => (
                  <span
                    key={m}
                    className="rounded border border-cyan-400/20 bg-cyan-400/[0.06] px-1.5 py-px font-mono text-[9px] text-cyan-300/90"
                  >
                    {m}
                  </span>
                ))}
                {p.models.length === 0 && (
                  <span className="font-mono text-[9px] text-slate-700">{t("providers.noModels")}</span>
                )}
              </div>
            </div>
            <button
              onClick={() => void test(p)}
              disabled={testing === p.id}
              className="grid h-7 w-7 place-items-center rounded-lg text-slate-500 transition-colors hover:bg-white/[0.07] hover:text-emerald-300 disabled:opacity-50"
              title={t("providers.test")}
            >
              {testing === p.id ? <Loader2 size={13} className="animate-spin" /> : <Zap size={13} />}
            </button>
            <button
              onClick={() => setModal({ open: true, editing: p })}
              className="grid h-7 w-7 place-items-center rounded-lg text-slate-500 transition-colors hover:bg-white/[0.07] hover:text-indigo-300"
              title={t("common.edit")}
            >
              <Pencil size={13} />
            </button>
            <button
              onClick={async () => {
                await wsRequest(93, { id: p.id });
                await refresh();
                notify("info", t("providers.deleted", { name: p.name }));
              }}
              className="grid h-7 w-7 place-items-center rounded-lg text-slate-500 transition-colors hover:bg-rose-500/15 hover:text-rose-300"
              title={t("common.delete")}
            >
              <Trash2 size={13} />
            </button>
            <SToggle
              rgb="34,211,238"
              on={p.enabled}
              onChange={async (v) => {
                await wsRequest(86, { id: p.id, patch: { enabled: v } });
                await refresh();
              }}
            />
          </Row>
        ))}
      </div>
      <AnimatePresence>
        {modal.open && (
          <ProviderModal
            initial={modal.editing}
            proxies={proxies}
            notify={notify}
            onClose={() => setModal({ open: false, editing: null })}
            onSaved={refresh}
          />
        )}
      </AnimatePresence>
    </div>
  );
}

/* ----------------------------------------------------------------- presets */

export function PresetsScreen({
  presets,
  agents,
  patch,
  notify,
}: {
  presets: Preset[];
  agents: LiveAgent[];
  patch: Patch;
  notify: Notify;
}) {
  const { t } = useT();
  const [modal, setModal] = useState<{ open: boolean; editing: Preset | null }>({ open: false, editing: null });
  const [title, setTitle] = useState("");
  const [text, setText] = useState("");
  const [agentId, setAgentId] = useState<string>("");
  const refresh = async () => patch({ presets: await wsRequest<Preset[]>(21) });

  const openAdd = () => {
    setTitle("");
    setText("");
    setAgentId("");
    setModal({ open: true, editing: null });
  };
  const openEdit = (p: Preset) => {
    setTitle(p.title);
    setText(p.text);
    setAgentId(p.agentId ?? "");
    setModal({ open: true, editing: p });
  };

  const save = async () => {
    const payload = { title: title.trim(), text: text.trim(), agentId: agentId || null };
    if (modal.editing) await wsRequest(22, { id: modal.editing.id, patch: payload });
    else await wsRequest(22, payload);
    setModal({ open: false, editing: null });
    await refresh();
    notify("ok", modal.editing ? t("presets.updated") : t("presets.added"));
  };

  return (
    <div className="mx-auto max-w-3xl">
      <ScreenHeader
        title={t("presets.title")}
        count={presets.length}
        actionLabel={t("presets.add")}
        onAction={openAdd}
      />
      <div className="space-y-2">
        {presets.map((p) => {
          const bound = agents.find((a) => a.id === p.agentId);
          return (
            <Row key={p.id}>
              <span
                className="grid h-9 w-9 shrink-0 place-items-center rounded-lg bg-amber-400/[0.1] text-amber-300"
                style={{ boxShadow: "inset 0 0 0 1px rgba(251,191,36,0.22)" }}
              >
                <Sparkles size={15} />
              </span>
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2">
                  <span className="truncate text-[13px] font-medium text-slate-100">{p.title}</span>
                  <span
                    className={`rounded border px-1.5 py-px font-mono text-[8.5px] uppercase tracking-wider ${bound ? "border-indigo-400/25 bg-indigo-400/[0.07] text-indigo-300" : "border-white/[0.08] bg-white/[0.03] text-slate-500"}`}
                  >
                    {bound ? bound.name : t("presets.all")}
                  </span>
                </div>
                <p className="truncate text-[11px] text-slate-500">{p.text}</p>
              </div>
              <button
                onClick={() => openEdit(p)}
                className="grid h-7 w-7 place-items-center rounded-lg text-slate-500 transition-colors hover:bg-white/[0.07] hover:text-indigo-300"
                title={t("common.edit")}
              >
                <Pencil size={13} />
              </button>
              <button
                onClick={async () => {
                  await wsRequest(24, { id: p.id });
                  await refresh();
                  notify("info", t("presets.deleted"));
                }}
                className="grid h-7 w-7 place-items-center rounded-lg text-slate-500 transition-colors hover:bg-rose-500/15 hover:text-rose-300"
                title={t("common.delete")}
              >
                <Trash2 size={13} />
              </button>
            </Row>
          );
        })}
        {presets.length === 0 && <p className="py-8 text-center text-[12px] text-slate-600">{t("presets.empty")}</p>}
      </div>

      <AnimatePresence>
        {modal.open && (
          <SModal
            title={modal.editing ? t("presets.editTitle") : t("presets.newTitle")}
            subtitle={t("presets.sub")}
            onClose={() => setModal({ open: false, editing: null })}
            footer={
              <>
                <SBtn onClick={() => setModal({ open: false, editing: null })}>{t("common.cancel")}</SBtn>
                <SBtn primary disabled={!title.trim() || !text.trim()} onClick={() => void save()}>
                  <Check size={13} />
                  {t("common.save")}
                </SBtn>
              </>
            }
          >
            <SField label={t("common.name")}>
              <input
                value={title}
                onChange={(e) => setTitle(e.target.value)}
                placeholder={t("presets.titlePh")}
                className={inputCls}
              />
            </SField>
            <SField label={t("presets.textLabel")}>
              <textarea
                value={text}
                onChange={(e) => setText(e.target.value)}
                rows={4}
                placeholder={t("presets.textPh")}
                className={`${inputCls} resize-none`}
              />
            </SField>
            <SField label={t("presets.bind")}>
              <select value={agentId} onChange={(e) => setAgentId(e.target.value)} className={inputCls}>
                <option value="">{t("presets.all")}</option>
                {agents.map((a) => (
                  <option key={a.id} value={a.id}>
                    {a.name}
                  </option>
                ))}
              </select>
            </SField>
          </SModal>
        )}
      </AnimatePresence>
    </div>
  );
}
