import type { ReactNode } from "react";
import { useEffect, useState } from "react";
import { AnimatePresence, motion } from "framer-motion";
import {
  Boxes,
  Check,
  ChevronDown,
  FolderOpen,
  Globe,
  Hexagon,
  MessageSquare,
  Pencil,
  Plus,
  Settings,
  Trash2,
  UserRound,
  X,
} from "lucide-react";
import type { AccentKey, AgentStatus, Conversation, LiveAgent, Profile, View, Workspace } from "../lib/data";
import { ACCENTS, formatMessageTime, STATUS_META } from "../lib/data";
import { pickFolder, wsRequest } from "../lib/api";
import type { Lang } from "../lib/i18n";
import { useT } from "../lib/i18n";

type Props = {
  view: View;
  version?: string;
  workspaces: (Workspace & { fileCount?: number })[];
  activeWsId: string | null;
  conversations: Conversation[];
  agents: LiveAgent[];
  activeAgentId: string;
  activeConvId: string | null;
  counts: {
    skills: number;
    providers: number;
    mcp: number;
    presets: number;
    kb: number;
    network: number;
    memory: number;
  };
  agentStatus: Record<string, AgentStatus>;
  onWorkspace: (id: string) => void;
  onWorkspaceCreate: (workspace: Workspace) => void;
  onWorkspaceDelete: (id: string) => void;
  onAgent: (id: string) => void;
  onNewConversation: () => void;
  onConversation: (id: string) => void;
  onConversationDelete: (id: string) => void;
  onNavigate: (v: View) => void;
  profiles: Profile[];
  activeProfileId: string;
  onProfileSwitch: (id: string) => void;
  onProfileCreate: (f: { name: string; role: string; accent: AccentKey }) => void;
  onProfileDelete: (id: string) => void;
  onProfileUpdate: (id: string, patch: Partial<Profile>) => void;
  open: boolean;
  onClose: () => void;
  connectionStatus?: "connected" | "disconnected";
};

const initials = (name: string) =>
  name
    .trim()
    .split(/\s+/)
    .map((w) => w[0] ?? "")
    .slice(0, 2)
    .join("")
    .toUpperCase() || "?";

function Label({ children, right }: { children: ReactNode; right?: ReactNode }) {
  return (
    <div className="mb-2 flex items-center justify-between px-1">
      <span className="font-mono text-[9px] tracking-[0.26em] text-slate-600">{children}</span>
      {right}
    </div>
  );
}

type Release = { version: string; name: string; body: string; publishedAt: string; available: boolean };
function VersionButton({ version }: { version?: string }) {
  const { t } = useT();
  const [release, setRelease] = useState<Release | null>(null);
  const [open, setOpen] = useState(false);
  const [updating, setUpdating] = useState(false);
  useEffect(() => {
    void wsRequest<Release>(115)
      .then(setRelease)
      .catch(() => undefined);
  }, []);
  const check = async () => {
    try {
      setRelease(await wsRequest<Release>(115));
    } finally {
      setOpen(true);
    }
  };
  return (
    <>
      <button
        onClick={() => void check()}
        className={`font-mono text-[8.5px] transition-colors ${release?.available ? "animate-pulse text-rose-300 hover:text-rose-100" : "text-slate-700 hover:text-slate-400"}`}
        title={t("update.title")}
      >
        {version ?? ""}
      </button>
      {open && (
        <div className="fixed inset-0 z-[90] grid place-items-center p-4">
          <button
            className="absolute inset-0 bg-abyss-950/80 backdrop-blur-sm"
            onClick={() => !updating && setOpen(false)}
            aria-label={t("common.close")}
          />
          <div className="relative w-full max-w-lg overflow-hidden rounded-xl border border-indigo-400/30 bg-abyss-900 p-5 shadow-2xl">
            <button onClick={() => setOpen(false)} className="absolute right-3 top-3 text-slate-500 hover:text-white">
              <X size={15} />
            </button>
            <p className="font-display text-lg text-slate-100">{t("update.title")}</p>
            {release?.available ? (
              <>
                <p className="mt-2 text-sm text-rose-200">{t("update.available", { version: release.version })}</p>
                <p className="mt-3 text-xs text-slate-400">{release.name}</p>
                <pre className="mt-3 max-h-52 overflow-auto whitespace-pre-wrap rounded-lg border border-white/10 bg-black/20 p-3 text-[11px] text-slate-400">
                  {release.body || t("update.noNotes")}
                </pre>
                <button
                  disabled={updating}
                  onClick={() => {
                    setUpdating(true);
                    void wsRequest(116).catch(() => setUpdating(false));
                  }}
                  className="mt-5 rounded-lg bg-gradient-to-r from-indigo-500 to-cyan-500 px-4 py-2 text-xs font-medium text-white disabled:opacity-60"
                >
                  {updating ? t("update.preparing") : t("update.apply")}
                </button>
              </>
            ) : (
              <p className="mt-3 text-sm text-slate-400">{t("update.latest")}</p>
            )}
          </div>
        </div>
      )}
    </>
  );
}

/* ------------------------------------------------------------ language -- */

function LangSwitcher() {
  const { lang, setLang, t } = useT();
  const [open, setOpen] = useState(false);
  const options: Lang[] = ["ru", "en", "es", "it", "ko", "fr", "zh", "hi", "bn", "pt"];

  return (
    <div className="relative">
      <button
        onClick={() => setOpen((v) => !v)}
        className={`flex h-8 items-center gap-1.5 rounded-lg border px-2 transition-all ${
          open
            ? "border-indigo-400/40 bg-indigo-400/[0.1] text-indigo-200"
            : "border-white/[0.08] text-slate-400 hover:bg-white/[0.05] hover:text-slate-200"
        }`}
        title={t("sidebar.langTitle")}
        aria-label={t("sidebar.langTitle")}
      >
        <Globe size={13} />
        <span className="font-mono text-[10px] font-semibold uppercase">{lang}</span>
        <motion.span animate={{ rotate: open ? 180 : 0 }} transition={{ duration: 0.2 }}>
          <ChevronDown size={11} />
        </motion.span>
      </button>

      <AnimatePresence>
        {open && (
          <>
            <div className="fixed inset-0 z-40" onClick={() => setOpen(false)} />
            <motion.div
              initial={{ opacity: 0, y: 8, scale: 0.96 }}
              animate={{ opacity: 1, y: 0, scale: 1 }}
              exit={{ opacity: 0, y: 8, scale: 0.96 }}
              transition={{ duration: 0.16, ease: "easeOut" }}
              className="absolute bottom-full right-0 z-50 mb-2 w-44 rounded-xl p-px"
              style={{
                background:
                  "linear-gradient(160deg, rgba(129,140,248,0.45), rgba(255,255,255,0.08) 45%, rgba(34,211,238,0.3))",
                boxShadow: "0 20px 50px -14px rgba(0,0,0,0.75)",
              }}
            >
              <div className="rounded-[11px] bg-abyss-850/98 p-1.5 backdrop-blur-2xl">
                <p className="px-2.5 pb-1 pt-1 font-mono text-[8.5px] tracking-[0.24em] text-slate-600">
                  {t("sidebar.langTitle").toUpperCase()}
                </p>
                {options.map((l) => (
                  <button
                    key={l}
                    onClick={() => {
                      setLang(l);
                      setOpen(false);
                    }}
                    className={`flex w-full items-center justify-between rounded-lg px-2.5 py-2 text-left text-[12px] transition-colors ${
                      lang === l
                        ? "bg-indigo-400/[0.12] text-indigo-100"
                        : "text-slate-300 hover:bg-white/[0.05] hover:text-white"
                    }`}
                  >
                    <span className="flex items-center gap-2">
                      <span
                        className={`grid h-4 w-4 place-items-center rounded border ${lang === l ? "border-indigo-300 bg-indigo-400/90 text-abyss-950" : "border-slate-600"}`}
                      >
                        {lang === l && <Check size={11} strokeWidth={3} />}
                      </span>
                      {t(`lang.${l}`)}
                    </span>
                    <span className="font-mono text-[9px] uppercase text-slate-500">{l}</span>
                  </button>
                ))}
              </div>
            </motion.div>
          </>
        )}
      </AnimatePresence>
    </div>
  );
}

/* -------------------------------------------------------------- profile -- */

function ProfileForm({
  initial,
  onSave,
  onCancel,
  saveLabel,
}: {
  initial?: Profile;
  onSave: (f: { name: string; role: string; accent: AccentKey }) => void;
  onCancel: () => void;
  saveLabel: string;
}) {
  const { t } = useT();
  const [name, setName] = useState(initial?.name ?? "");
  const [role, setRole] = useState(initial?.role ?? "");
  const [accent, setAccent] = useState<AccentKey>(initial?.accent ?? "indigo");
  const valid = name.trim().length > 0;

  return (
    <div className="rounded-xl border border-white/[0.08] bg-abyss-850/95 p-4 shadow-2xl backdrop-blur-2xl">
      <p className="mb-3 font-mono text-[9px] tracking-[0.24em] text-slate-600">
        {initial ? t("profile.editTitle").toUpperCase() : t("profile.newTitle").toUpperCase()}
      </p>
      <label className="mb-3 block">
        <span className="mb-1.5 block text-[11px] font-medium text-slate-400">{t("profile.name")}</span>
        <input
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder={t("profile.namePh")}
          className="w-full rounded-lg border border-white/[0.09] bg-abyss-900/70 px-3 py-2 text-[12.5px] text-slate-200 outline-none placeholder:text-slate-700 focus:border-indigo-400/45"
        />
      </label>
      <label className="mb-3 block">
        <span className="mb-1.5 block text-[11px] font-medium text-slate-400">{t("profile.role")}</span>
        <input
          value={role}
          onChange={(e) => setRole(e.target.value)}
          placeholder={t("profile.rolePh")}
          className="w-full rounded-lg border border-white/[0.09] bg-abyss-900/70 px-3 py-2 text-[12.5px] text-slate-200 outline-none placeholder:text-slate-700 focus:border-indigo-400/45"
        />
      </label>
      <div className="mb-4">
        <span className="mb-1.5 block text-[11px] font-medium text-slate-400">{t("profile.accent")}</span>
        <div className="flex gap-2">
          {(Object.keys(ACCENTS) as AccentKey[]).map((k) => (
            <button
              key={k}
              onClick={() => setAccent(k)}
              className={`h-6 w-6 rounded-full border-2 transition-transform ${accent === k ? "scale-110 border-white/70" : "border-transparent hover:scale-105"}`}
              style={{ background: ACCENTS[k].hex, boxShadow: accent === k ? `0 0 12px ${ACCENTS[k].hex}` : undefined }}
              aria-label={k}
            />
          ))}
        </div>
      </div>
      <div className="flex justify-end gap-2">
        <button
          onClick={onCancel}
          className="rounded-lg border border-white/10 bg-white/[0.04] px-3 py-1.5 text-[11.5px] text-slate-300 transition-colors hover:bg-white/[0.08]"
        >
          {t("common.cancel")}
        </button>
        <button
          onClick={() => valid && onSave({ name: name.trim(), role: role.trim(), accent })}
          disabled={!valid}
          className="rounded-lg bg-gradient-to-r from-indigo-500 to-cyan-500 px-3 py-1.5 text-[11.5px] font-semibold text-white transition-all hover:brightness-115 disabled:cursor-not-allowed disabled:opacity-40"
        >
          {saveLabel}
        </button>
      </div>
    </div>
  );
}

function ProfileSwitcher({
  profiles,
  activeProfileId,
  onSwitch,
  onCreate,
  onDelete,
  onUpdate,
}: {
  profiles: Profile[];
  activeProfileId: string;
  onSwitch: (id: string) => void;
  onCreate: (f: { name: string; role: string; accent: AccentKey }) => void;
  onDelete: (id: string) => void;
  onUpdate: (id: string, patch: Partial<Profile>) => void;
}) {
  const { t } = useT();
  const [open, setOpen] = useState(false);
  const [formMode, setFormMode] = useState<"none" | "create" | "edit">("none");
  const [editing, setEditing] = useState<Profile | null>(null);
  const active = profiles.find((p) => p.id === activeProfileId);
  const accent = ACCENTS[active?.accent ?? "indigo"] ?? ACCENTS.indigo;

  return (
    <div className="relative">
      <button
        onClick={() => setOpen((v) => !v)}
        className="flex w-full items-center gap-2.5 rounded-xl border border-white/[0.08] bg-white/[0.03] px-2 py-2 text-left transition-all hover:border-white/[0.16] hover:bg-white/[0.06]"
        title={t("profile.switch")}
      >
        <span
          className="grid h-9 w-9 shrink-0 place-items-center rounded-full font-display text-[11px] font-bold text-abyss-950"
          style={{
            background: `linear-gradient(135deg, ${accent.hex}, ${accent.hex}cc)`,
            boxShadow: `0 0 16px -4px ${accent.hex}`,
          }}
        >
          {initials(active?.name ?? "?")}
        </span>
        <div className="min-w-0 flex-1">
          <p className="truncate text-[12.5px] font-medium text-slate-100">{active?.name ?? t("profile.empty")}</p>
          <p className="truncate font-mono text-[9px] text-slate-600">{active?.role ?? ""}</p>
        </div>
        <motion.span animate={{ rotate: open ? 180 : 0 }} transition={{ duration: 0.2 }}>
          <ChevronDown size={13} className="text-slate-500" />
        </motion.span>
      </button>

      <AnimatePresence>
        {open && (
          <>
            <div
              className="fixed inset-0 z-40"
              onClick={() => {
                setOpen(false);
                setFormMode("none");
              }}
            />
            <motion.div
              initial={{ opacity: 0, y: 8, scale: 0.96 }}
              animate={{ opacity: 1, y: 0, scale: 1 }}
              exit={{ opacity: 0, y: 8, scale: 0.96 }}
              transition={{ duration: 0.16, ease: "easeOut" }}
              className="absolute bottom-full left-0 right-0 z-50 mb-2 rounded-xl p-px"
              style={{
                background:
                  "linear-gradient(160deg, rgba(129,140,248,0.45), rgba(255,255,255,0.08) 45%, rgba(34,211,238,0.3))",
                boxShadow: "0 20px 50px -14px rgba(0,0,0,0.75)",
              }}
            >
              <div className="rounded-[11px] bg-abyss-850/98 p-2 backdrop-blur-2xl">
                <p className="px-2.5 pb-1 pt-1 font-mono text-[8.5px] tracking-[0.24em] text-slate-600">
                  {t("profile.title").toUpperCase()}
                </p>

                {formMode !== "none" ? (
                  <div className="px-1 py-1">
                    <ProfileForm
                      initial={formMode === "edit" ? (editing ?? undefined) : undefined}
                      saveLabel={formMode === "edit" ? t("common.save") : t("profile.create")}
                      onCancel={() => {
                        setFormMode("none");
                        setEditing(null);
                      }}
                      onSave={(f) => {
                        if (formMode === "edit" && editing) onUpdate(editing.id, f);
                        else onCreate(f);
                        setFormMode("none");
                        setEditing(null);
                        setOpen(false);
                      }}
                    />
                  </div>
                ) : (
                  <>
                    <div className="max-h-52 space-y-1 overflow-y-auto scroll-slim">
                      {profiles.map((p) => {
                        const pa = ACCENTS[p.accent] ?? ACCENTS.indigo;
                        const isActive = p.id === activeProfileId;
                        return (
                          <div
                            key={p.id}
                            className={`group flex items-center gap-2.5 rounded-lg px-2.5 py-2 transition-colors ${
                              isActive ? "bg-indigo-400/[0.12]" : "hover:bg-white/[0.05]"
                            }`}
                          >
                            <button
                              onClick={() => {
                                onSwitch(p.id);
                                setOpen(false);
                              }}
                              className="flex min-w-0 flex-1 items-center gap-2.5 text-left"
                              title={t("profile.switchTo", { name: p.name })}
                            >
                              <span
                                className="grid h-7 w-7 shrink-0 place-items-center rounded-full font-display text-[9.5px] font-bold text-abyss-950"
                                style={{ background: pa.hex }}
                              >
                                {initials(p.name)}
                              </span>
                              <span className="min-w-0 flex-1">
                                <span
                                  className={`block truncate text-[12px] ${isActive ? "text-indigo-100" : "text-slate-300"}`}
                                >
                                  {p.name}
                                </span>
                                <span className="block truncate font-mono text-[8.5px] text-slate-600">{p.role}</span>
                              </span>
                              {isActive && <Check size={13} className="shrink-0 text-indigo-300" strokeWidth={3} />}
                            </button>
                            <button
                              onClick={() => {
                                setEditing(p);
                                setFormMode("edit");
                              }}
                              className="grid h-6 w-6 shrink-0 place-items-center rounded text-slate-600 opacity-0 transition-all hover:bg-white/[0.08] hover:text-indigo-300 group-hover:opacity-100"
                              title={t("common.edit")}
                            >
                              <Pencil size={11} />
                            </button>
                            {profiles.length > 1 && (
                              <button
                                onClick={() => {
                                  onDelete(p.id);
                                  setOpen(false);
                                }}
                                className="grid h-6 w-6 shrink-0 place-items-center rounded text-slate-600 opacity-0 transition-all hover:bg-rose-500/15 hover:text-rose-300 group-hover:opacity-100"
                                title={t("common.delete")}
                              >
                                <Trash2 size={11} />
                              </button>
                            )}
                          </div>
                        );
                      })}
                    </div>
                    <button
                      onClick={() => {
                        setEditing(null);
                        setFormMode("create");
                      }}
                      className="mt-1.5 flex w-full items-center gap-2 rounded-lg border border-dashed border-white/[0.12] px-2.5 py-2 text-[11.5px] text-slate-400 transition-colors hover:border-indigo-400/40 hover:text-indigo-200"
                    >
                      <UserRound size={13} />
                      {t("profile.add")}
                    </button>
                  </>
                )}
              </div>
            </motion.div>
          </>
        )}
      </AnimatePresence>
    </div>
  );
}

/* --------------------------------------------------------------- content -- */

function SidebarContent(props: Props) {
  const t = useT().t;
  const {
    view,
    workspaces,
    activeWsId,
    conversations,
    agents,
    activeAgentId,
    activeConvId,
    counts,
    agentStatus,
    onWorkspace,
    onWorkspaceCreate,
    onWorkspaceDelete,
    onAgent,
    onNewConversation,
    onConversation,
    onConversationDelete,
    onNavigate,
    profiles,
    activeProfileId,
    onProfileSwitch,
    onProfileCreate,
    onProfileDelete,
    onProfileUpdate,
  } = props;

  const [wsOpen, setWsOpen] = useState(false);
  const activeWs = workspaces.find((w) => w.id === activeWsId);

  const NAV: { key: View; icon: typeof Boxes; count: number; rgb: string }[] = [
    {
      key: "settings",
      icon: Settings,
      count:
        counts.skills +
        counts.providers +
        counts.mcp +
        agents.length +
        counts.presets +
        counts.kb +
        counts.network +
        counts.memory,
      rgb: "129,140,248",
    },
  ];

  return (
    <div className="flex h-full flex-col">
      {/* logo */}
      <div className="flex items-center gap-3 border-b border-white/[0.06] px-4 py-4">
        <span className="grid h-9 w-9 place-items-center rounded-xl bg-gradient-to-br from-indigo-500/30 to-cyan-400/15 shadow-[inset_0_0_0_1px_rgba(129,140,248,0.35),0_0_24px_-6px_rgba(129,140,248,0.55)]">
          <Hexagon size={17} className="text-indigo-300" />
        </span>
        <div className="min-w-0">
          <p className="font-display text-[13.5px] font-bold tracking-wide text-slate-100">{t("brand.name")}</p>
          <p className="font-mono text-[8px] tracking-[0.3em] text-slate-600">{t("brand.tagline")}</p>
        </div>
      </div>

      <div className="scroll-slim min-h-0 flex-1 overflow-y-auto px-3 py-4">
        {/* workspaces */}
        <div className="relative mb-5">
          <Label>{t("sidebar.workspaces")}</Label>
          <button
            onClick={() => setWsOpen((v) => !v)}
            className={`flex w-full items-center gap-2.5 rounded-xl border px-3 py-2.5 transition-all ${
              wsOpen
                ? "border-indigo-400/35 bg-indigo-400/[0.08]"
                : "border-white/[0.08] bg-white/[0.03] hover:border-white/[0.16] hover:bg-white/[0.05]"
            }`}
          >
            <FolderOpen size={15} className="shrink-0 text-indigo-300" />
            <span className="min-w-0 flex-1 text-left">
              <span className="block truncate text-[12.5px] font-medium text-slate-100">{activeWs?.name ?? "—"}</span>
              <span className="block truncate font-mono text-[9px] text-slate-600">{activeWs?.folderPath}</span>
            </span>
            <motion.span animate={{ rotate: wsOpen ? 180 : 0 }} transition={{ duration: 0.2 }}>
              <ChevronDown size={14} className="text-slate-500" />
            </motion.span>
          </button>

          <AnimatePresence>
            {wsOpen && (
              <>
                <div className="fixed inset-0 z-30" onClick={() => setWsOpen(false)} />
                <motion.div
                  initial={{ opacity: 0, y: -6, scale: 0.98 }}
                  animate={{ opacity: 1, y: 0, scale: 1 }}
                  exit={{ opacity: 0, y: -6, scale: 0.98 }}
                  transition={{ duration: 0.16, ease: "easeOut" }}
                  className="absolute left-0 right-0 top-full z-40 mt-2 rounded-xl p-px"
                  style={{
                    background:
                      "linear-gradient(160deg, rgba(129,140,248,0.4), rgba(255,255,255,0.08) 45%, rgba(34,211,238,0.3))",
                    boxShadow: "0 22px 55px -16px rgba(0,0,0,0.75)",
                  }}
                >
                  <div className="rounded-[11px] bg-abyss-850/98 p-1.5 backdrop-blur-2xl">
                    {workspaces.map((w) => (
                      <button
                        key={w.id}
                        onClick={() => {
                          onWorkspace(w.id);
                          setWsOpen(false);
                        }}
                        className={`flex w-full items-center gap-2.5 rounded-lg px-2.5 py-2 text-left transition-colors ${
                          w.id === activeWsId ? "bg-indigo-400/[0.1]" : "hover:bg-white/[0.05]"
                        }`}
                      >
                        <FolderOpen size={13} className={w.id === activeWsId ? "text-indigo-300" : "text-slate-500"} />
                        <span className="min-w-0 flex-1">
                          <span
                            className={`block truncate text-[12px] ${w.id === activeWsId ? "text-slate-100" : "text-slate-300"}`}
                          >
                            {w.name}
                          </span>
                          <span className="block truncate font-mono text-[8.5px] text-slate-600">{w.folderPath}</span>
                        </span>
                        {w.fileCount !== undefined && (
                          <span className="rounded border border-white/[0.08] bg-white/[0.03] px-1.5 py-px font-mono text-[9px] text-slate-500">
                            {w.fileCount}
                          </span>
                        )}
                        {w.id === activeWsId && <Check size={12} className="text-indigo-300" />}
                        <span
                          role="button"
                          tabIndex={0}
                          title={t("common.delete")}
                          aria-label={t("common.delete")}
                          onClick={(event) => {
                            event.stopPropagation();
                            onWorkspaceDelete(w.id);
                          }}
                          onKeyDown={(event) => {
                            if (event.key === "Enter" || event.key === " ") {
                              event.preventDefault();
                              event.stopPropagation();
                              onWorkspaceDelete(w.id);
                            }
                          }}
                          className="grid h-7 w-7 shrink-0 place-items-center rounded-md text-slate-600 transition-colors hover:bg-rose-400/10 hover:text-rose-300"
                        >
                          <Trash2 size={13} />
                        </span>
                      </button>
                    ))}

                    <div className="my-1.5 h-px bg-white/[0.06]" />

                    <button
                      onClick={async () => {
                        const folder = await pickFolder();
                        setWsOpen(false);
                        if (folder) onWorkspaceCreate(folder);
                      }}
                      className="flex w-full items-center gap-2.5 rounded-lg px-2.5 py-2 text-left text-[12px] text-slate-300 transition-colors hover:bg-white/[0.05] hover:text-white"
                    >
                      <Plus size={13} className="text-emerald-300" />
                      {t("sidebar.connectFolder")}
                    </button>

                    <p className="px-2.5 pb-1.5 pt-1 text-[9.5px] leading-relaxed text-slate-600">
                      {t("sidebar.connectHint")}
                    </p>
                  </div>
                </motion.div>
              </>
            )}
          </AnimatePresence>
        </div>

        {/* conversations */}
        <div className="mb-5">
          <Label
            right={
              <button
                onClick={onNewConversation}
                className="grid h-5 w-5 place-items-center rounded-md text-slate-500 transition-colors hover:bg-white/[0.07] hover:text-indigo-300"
                title={t("sidebar.newDialog")}
              >
                <Plus size={12} />
              </button>
            }
          >
            {t("sidebar.dialogs").toUpperCase()}
          </Label>
          <div className="space-y-1">
            {conversations.map((c) => {
              const ag = agents.find((a) => a.id === c.agentId);
              const active = c.id === activeConvId && view === "chat";
              return (
                <div key={c.id} className="group relative">
                  <button
                    onClick={() => onConversation(c.id)}
                    className={`flex w-full items-center gap-2.5 rounded-lg border px-2.5 py-2 text-left transition-all ${
                      active
                        ? "border-white/[0.14] bg-white/[0.07]"
                        : "border-transparent hover:border-white/[0.08] hover:bg-white/[0.03]"
                    }`}
                  >
                    <span
                      className="grid h-7 w-7 shrink-0 place-items-center rounded-md"
                      style={{
                        background: ag
                          ? `rgba(${(ACCENTS[ag.accent] ?? ACCENTS.indigo).rgb},0.12)`
                          : "rgba(148,163,184,0.08)",
                        boxShadow: ag
                          ? `inset 0 0 0 1px rgba(${(ACCENTS[ag.accent] ?? ACCENTS.indigo).rgb},0.22)`
                          : undefined,
                      }}
                    >
                      {ag ? (
                        <ag.icon size={12} style={{ color: (ACCENTS[ag.accent] ?? ACCENTS.indigo).hex }} />
                      ) : (
                        <MessageSquare size={12} className="text-slate-500" />
                      )}
                    </span>
                    <span className="min-w-0 flex-1">
                      <span className={`block truncate text-[12px] ${active ? "text-slate-100" : "text-slate-300"}`}>
                        {c.title}
                      </span>
                      <span className="block font-mono text-[8.5px] text-slate-600">
                        {ag?.name} · {formatMessageTime(c.updatedAt)}
                      </span>
                    </span>
                  </button>
                  <button
                    onClick={() => onConversationDelete(c.id)}
                    className="absolute right-1.5 top-1/2 grid h-6 w-6 -translate-y-1/2 place-items-center rounded-md text-slate-600 opacity-0 transition-all hover:bg-rose-500/15 hover:text-rose-300 group-hover:opacity-100"
                    title={t("common.delete")}
                  >
                    <Trash2 size={11} />
                  </button>
                </div>
              );
            })}
            {conversations.length === 0 && (
              <p className="px-2 py-1 text-[11px] text-slate-600">{t("sidebar.noDialogs")}</p>
            )}
          </div>
        </div>

        {/* agents */}
        <div className="mb-5">
          <Label>{t("sidebar.agents").toUpperCase()}</Label>
          <div className="space-y-1">
            {agents.map((a) => {
              const status = agentStatus[a.id] ?? "idle";
              const meta = STATUS_META[status] ?? STATUS_META.idle;
              const accent = ACCENTS[a.accent] ?? ACCENTS.indigo;
              return (
                <button
                  key={a.id}
                  onClick={() => onAgent(a.id)}
                  className={`flex w-full items-center gap-2.5 rounded-lg border px-2.5 py-2 text-left transition-all ${
                    a.id === activeAgentId && view === "chat"
                      ? "border-white/[0.14] bg-white/[0.07]"
                      : "border-transparent hover:border-white/[0.08] hover:bg-white/[0.03]"
                  }`}
                >
                  <span
                    className="grid h-7 w-7 shrink-0 place-items-center rounded-md"
                    style={{
                      background: `rgba(${accent.rgb},0.12)`,
                      boxShadow: `inset 0 0 0 1px rgba(${accent.rgb},0.22)`,
                    }}
                  >
                    <a.icon size={12} style={{ color: accent.hex }} />
                  </span>
                  <span
                    className={`min-w-0 flex-1 truncate text-[12px] ${a.id === activeAgentId && view === "chat" ? "text-slate-100" : "text-slate-300"}`}
                  >
                    {a.name}
                  </span>
                  <span className={`flex shrink-0 items-center gap-1 font-mono text-[8.5px] ${meta.text}`}>
                    <span
                      className={`h-1.5 w-1.5 rounded-full ${meta.dot} ${meta.pulse ? "dot-live" : ""}`}
                      style={{ color: meta.dotHex }}
                    />
                    {t(`status.${status}`)}
                  </span>
                </button>
              );
            })}
          </div>
        </div>

        {/* configuration */}
        <div className="mb-2">
          <Label>{t("sidebar.config").toUpperCase()}</Label>
          <div className="space-y-1">
            {NAV.map((n) => {
              const active = view === n.key;
              const Icon = n.icon;
              return (
                <button
                  key={n.key}
                  onClick={() => onNavigate(n.key)}
                  className={`flex w-full items-center gap-2.5 rounded-lg border px-2.5 py-2 text-left transition-all ${
                    active
                      ? "border-white/[0.14] bg-white/[0.07]"
                      : "border-transparent hover:border-white/[0.08] hover:bg-white/[0.03]"
                  }`}
                >
                  <span
                    className="grid h-7 w-7 shrink-0 place-items-center rounded-md"
                    style={{ background: `rgba(${n.rgb},0.1)`, boxShadow: `inset 0 0 0 1px rgba(${n.rgb},0.2)` }}
                  >
                    <Icon size={12} style={{ color: `rgb(${n.rgb})` }} />
                  </span>
                  <span
                    className={`min-w-0 flex-1 truncate text-[12px] ${active ? "text-slate-100" : "text-slate-300"}`}
                  >
                    {t(`nav.${n.key}`)}
                  </span>
                  <span className="shrink-0 rounded border border-white/[0.08] bg-white/[0.03] px-1.5 py-px font-mono text-[9px] text-slate-500">
                    {n.count}
                  </span>
                </button>
              );
            })}
          </div>
        </div>
      </div>

      {/* profile switcher */}
      <div className="space-y-2 border-t border-white/[0.06] px-3.5 py-3.5">
        <ProfileSwitcher
          profiles={profiles}
          activeProfileId={activeProfileId}
          onSwitch={onProfileSwitch}
          onCreate={onProfileCreate}
          onDelete={onProfileDelete}
          onUpdate={onProfileUpdate}
        />
        <div className="flex items-center justify-between px-0.5">
          <span className="font-mono text-[8.5px] text-slate-700">{t("profile.isolated")}</span>
          <LangSwitcher />
        </div>
      </div>
    </div>
  );
}

/* ------------------------------------------------------------- sidebar -- */

export function Sidebar(props: Props) {
  const t = useT().t;
  const connected = props.connectionStatus === "connected";

  const inner = (
    <aside className="relative z-30 flex h-full w-[290px] shrink-0 flex-col border-r border-white/[0.06] bg-abyss-850/85 backdrop-blur-xl">
      <SidebarContent {...props} />
      <div className="flex items-center justify-between border-t border-white/[0.06] px-4 py-2">
        <span
          className={`flex items-center gap-1.5 font-mono text-[9px] uppercase tracking-wider ${connected ? "text-emerald-400/90" : "text-rose-300"}`}
        >
          <span className={`h-1.5 w-1.5 rounded-full ${connected ? "pulse-online bg-emerald-400" : "bg-rose-400"}`} />
          {connected ? t("system.online") : t("system.offline")}
        </span>
        <VersionButton version={props.version} />
      </div>
    </aside>
  );

  return (
    <>
      <div className="hidden h-full lg:block">{inner}</div>
      <AnimatePresence>
        {props.open && (
          <>
            <motion.div
              className="fixed inset-0 z-40 bg-abyss-950/60 backdrop-blur-sm lg:hidden"
              initial={{ opacity: 0 }}
              animate={{ opacity: 1 }}
              exit={{ opacity: 0 }}
              onClick={props.onClose}
            />
            <motion.div
              className="fixed inset-y-0 left-0 z-50 lg:hidden"
              initial={{ x: -300 }}
              animate={{ x: 0 }}
              exit={{ x: -300 }}
              transition={{ type: "spring", stiffness: 380, damping: 34 }}
            >
              <div className="relative h-full">
                {inner}
                <button
                  onClick={props.onClose}
                  className="absolute right-3 top-4 grid h-7 w-7 place-items-center rounded-lg text-slate-400 hover:bg-white/[0.07] hover:text-white"
                  aria-label={t("common.close")}
                >
                  <X size={14} />
                </button>
              </div>
            </motion.div>
          </>
        )}
      </AnimatePresence>
    </>
  );
}
