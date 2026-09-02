import { useEffect, useMemo, useRef, useState } from "react";
import type { ReactNode } from "react";
import { AnimatePresence, motion } from "framer-motion";
import {
  BookOpen,
  Check,
  ChevronDown,
  ChevronLeft,
  ChevronRight,
  FolderOpen,
  Globe,
  Link2,
  Loader2,
  Network,
  Pencil,
  Plug,
  Plus,
  RefreshCw,
  Search,
  Trash2,
  X,
} from "lucide-react";
import { fmtSize, formatMessageTime, uid } from "../../lib/data";
import type { KbDoc, McpServer } from "../../lib/data";
import { pickFolder, wsRequest } from "../../lib/api";
import { SBtn, SField, SModal, SToggle, ScreenHeader, inputCls } from "./SkillsProviders";
import type { Notify } from "./SkillsProviders";
import type { Db } from "../../lib/api";
import { useT } from "../../lib/i18n";

type Patch = (p: Partial<Db>) => void;
const normalizeKbDoc = (doc: KbDoc): KbDoc => ({
  ...doc,
  tags: Array.isArray(doc.tags) ? doc.tags : [],
  content: doc.content ?? "",
  source: doc.source ?? "",
  kind: doc.kind || "doc",
});

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
function wildcard(pattern: string, value: string) {
  const escaped = pattern
    .trim()
    .split("*")
    .map((part) => part.replace(/[.+?^${}()|[\]\\]/g, "\\$&"))
    .join(".*");
  return new RegExp(`^${escaped}$`, "i").test(value);
}

/* -------------------------------------------------------------------- mcp */

const TRANSPORTS = {
  builtin: { label: "SYSTEM", cls: "border-amber-400/25 bg-amber-400/10 text-amber-300" },
  stdio: { label: "STDIO", cls: "border-white/10 bg-white/[0.04] text-slate-400" },
  sse: { label: "SSE", cls: "border-cyan-400/25 bg-cyan-400/10 text-cyan-300" },
  http: { label: "HTTP JSON-RPC", cls: "border-violet-400/25 bg-violet-400/10 text-violet-300" },
} as const;

type ToolDraft = {
  name: string;
  desc?: string;
  inputSchema?: Record<string, unknown>;
  alias: string;
  enabled: boolean;
};

function McpModal({
  initial,
  onClose,
  onSaved,
  notify,
}: {
  initial: McpServer | null;
  onClose: () => void;
  onSaved: () => Promise<void>;
  notify: Notify;
}) {
  const { t } = useT();
  const [name, setName] = useState(initial?.name ?? "");
  const [transport, setTransport] = useState<McpServer["transport"]>(initial?.transport ?? "stdio");
  const [command, setCommand] = useState(initial?.command ?? "");
  const [url, setUrl] = useState(initial?.url ?? "");
  const [prefix, setPrefix] = useState(initial?.prefix ?? "");
  const [headers, setHeaders] = useState<{ id: string; k: string; v: string }[]>(initial?.headers ?? []);
  const [tools, setTools] = useState<ToolDraft[] | null>(
    initial
      ? initial.tools.map((tl) => ({
          name: tl.name,
          desc: tl.description,
          inputSchema: tl.inputSchema,
          alias: tl.alias,
          enabled: tl.enabled,
        }))
      : null,
  );
  const [fetching, setFetching] = useState(false);
  const [saving, setSaving] = useState(false);
  const system = Boolean(initial?.system);

  const valid = system || (name.trim() && prefix.trim() && (transport === "stdio" ? command.trim() : url.trim()));

  const fetchTools = async () => {
    if (!name.trim()) {
      notify("err", t("mcp.needName"));
      return;
    }
    setFetching(true);
    try {
      const res = await wsRequest<{
        tools: { name: string; desc: string; inputSchema?: Record<string, unknown>; alias: string; enabled: boolean }[];
      }>(83, {
        name: name.trim(),
        transport,
        command: command.trim(),
        url: url.trim(),
        prefix: prefix.trim(),
      });
      setTools(
        res.tools.map((tl) => ({
          name: tl.name,
          desc: tl.desc,
          inputSchema: tl.inputSchema,
          alias: tl.alias || tl.name,
          enabled: tl.enabled,
        })),
      );
      notify("info", t("mcp.fetched", { n: res.tools.length }));
    } catch (error) {
      notify("err", error instanceof Error ? error.message : String(error));
    } finally {
      setFetching(false);
    }
  };

  const save = async () => {
    setSaving(true);
    try {
      const payload: Omit<McpServer, "id"> = {
        name: name.trim(),
        transport,
        prefix: prefix.trim(),
        command: transport === "stdio" ? command.trim() : undefined,
        url: transport !== "stdio" ? url.trim() : undefined,
        headers: transport === "stdio" ? [] : headers.filter((h) => h.k.trim()),
        enabled: initial?.enabled ?? false,
        tools: (tools ?? []).map((tl) => ({
          name: tl.name,
          description: tl.desc,
          inputSchema: tl.inputSchema,
          alias: tl.alias.trim() || tl.name,
          enabled: tl.enabled,
        })),
      };
      if (initial) await wsRequest(78, { id: initial.id, patch: payload });
      else await wsRequest(77, payload);
      await onSaved();
      notify("ok", initial ? t("mcp.updated") : t("mcp.added", { name }));
      onClose();
    } catch (error) {
      notify("err", error instanceof Error ? error.message : String(error));
    } finally {
      setSaving(false);
    }
  };

  return (
    <SModal
      title={initial ? t("mcp.editTitle") : t("mcp.newTitle")}
      subtitle={t("mcp.sub")}
      onClose={onClose}
      w="max-w-xl"
      footer={
        <>
          <SBtn onClick={onClose}>{t("common.cancel")}</SBtn>
          <SBtn primary disabled={!valid || saving} onClick={() => void save()}>
            {saving ? <Loader2 size={13} className="animate-spin" /> : <Check size={13} />}
            {t("common.save")}
          </SBtn>
        </>
      }
    >
      {!system && (
        <div className="grid grid-cols-[1fr_120px] gap-3">
          <SField label={t("mcp.serverName")}>
            <input
              value={name}
              onChange={(e) => {
                setName(e.target.value);
                if (!initial && !prefix) setPrefix(e.target.value.slice(0, 2).toLowerCase());
              }}
              placeholder="filesystem"
              className={`${inputCls} font-mono text-[12px]`}
            />
          </SField>
          <SField label={t("mcp.prefix")}>
            <div className="relative">
              <input
                value={prefix}
                onChange={(e) => setPrefix(e.target.value.replace(/[^a-z0-9_-]/gi, ""))}
                placeholder="fs"
                className={`${inputCls} pr-6 font-mono text-[12px]`}
              />
              <span className="absolute right-2.5 top-1/2 -translate-y-1/2 font-mono text-[11px] text-slate-600">
                .*
              </span>
            </div>
          </SField>
        </div>
      )}
      {system && (
        <div className="rounded-lg border border-amber-400/20 bg-amber-400/[0.05] px-3 py-2 text-[11px] text-amber-200">
          {t("mcp.systemHint")}
        </div>
      )}

      {!system && (
        <SField label={t("mcp.transport")}>
          <div className="grid grid-cols-3 gap-1.5">
            {(Object.keys(TRANSPORTS).filter((tr) => tr !== "builtin") as McpServer["transport"][]).map((tr) => (
              <button
                key={tr}
                onClick={() => setTransport(tr)}
                className={`rounded-lg border px-2 py-2 font-mono text-[10.5px] transition-all ${transport === tr ? TRANSPORTS[tr].cls : "border-white/[0.07] text-slate-500 hover:bg-white/[0.04]"}`}
              >
                {TRANSPORTS[tr].label}
              </button>
            ))}
          </div>
        </SField>
      )}

      {!system &&
        (transport === "stdio" ? (
          <SField label={t("mcp.command")}>
            <input
              value={command}
              onChange={(e) => setCommand(e.target.value)}
              placeholder="npx -y @modelcontextprotocol/server-filesystem"
              className={`${inputCls} font-mono text-[11px]`}
            />
          </SField>
        ) : (
          <SField label={transport === "http" ? t("mcp.urlHttp") : t("mcp.urlSse")}>
            <input
              value={url}
              onChange={(e) => setUrl(e.target.value)}
              placeholder={transport === "http" ? "https://mcp.example.com/jsonrpc" : "https://mcp.example.com/sse"}
              className={`${inputCls} font-mono text-[11px]`}
            />
          </SField>
        ))}

      {!system && transport !== "stdio" && (
        <div className="mb-3 rounded-lg border border-white/[0.07] bg-abyss-900/50 p-3">
          <div className="mb-2 flex items-center justify-between">
            <span className="text-[11.5px] font-medium text-slate-300">{t("mcp.headers")}</span>
            <button
              onClick={() => setHeaders((h) => [...h, { id: uid(), k: "", v: "" }])}
              className="flex items-center gap-1 rounded-md border border-white/[0.08] px-2 py-1 text-[10.5px] text-slate-400 transition-colors hover:border-indigo-400/40 hover:text-indigo-300"
            >
              <Plus size={10} />
              {t("mcp.addHeader")}
            </button>
          </div>
          {headers.length === 0 && <p className="text-[10.5px] text-slate-600">{t("mcp.headersHint")}</p>}
          <div className="space-y-1.5">
            {headers.map((h) => (
              <div key={h.id} className="flex items-center gap-1.5">
                <input
                  value={h.k}
                  onChange={(e) =>
                    setHeaders((list) => list.map((x) => (x.id === h.id ? { ...x, k: e.target.value } : x)))
                  }
                  placeholder="Authorization"
                  className={`${inputCls} py-1.5 font-mono text-[10.5px]`}
                />
                <input
                  value={h.v}
                  onChange={(e) =>
                    setHeaders((list) => list.map((x) => (x.id === h.id ? { ...x, v: e.target.value } : x)))
                  }
                  placeholder="Bearer …"
                  className={`${inputCls} py-1.5 font-mono text-[10.5px]`}
                />
                <button
                  onClick={() => setHeaders((list) => list.filter((x) => x.id !== h.id))}
                  className="grid h-7 w-7 shrink-0 place-items-center rounded-md text-slate-600 transition-colors hover:bg-rose-500/15 hover:text-rose-300"
                  aria-label={t("common.delete")}
                >
                  <X size={12} />
                </button>
              </div>
            ))}
          </div>
        </div>
      )}

      <div className="rounded-lg border border-white/[0.07] bg-abyss-900/50 p-3">
        <div className="mb-2 flex items-center justify-between">
          <span className="text-[11.5px] font-medium text-slate-300">{t("mcp.tools")}</span>
          {!system && (
            <SBtn onClick={() => void fetchTools()} disabled={fetching}>
              {fetching ? <Loader2 size={12} className="animate-spin" /> : <RefreshCw size={12} />}
              {tools ? t("mcp.refresh") : t("mcp.fetch")}
            </SBtn>
          )}
        </div>
        {tools === null && <p className="py-1 text-[10.5px] text-slate-600">{t("mcp.toolsHint")}</p>}
        {tools !== null && tools.length === 0 && (
          <p className="py-1 text-[10.5px] text-slate-600">{t("mcp.noTools")}</p>
        )}
        {tools && tools.length > 0 && (
          <div className="max-h-52 space-y-1.5 overflow-y-auto scroll-slim">
            {tools.map((tl) => (
              <div
                key={tl.name}
                className={`flex items-center gap-2.5 rounded-lg border px-2.5 py-2 transition-all ${tl.enabled ? "border-emerald-400/25 bg-emerald-400/[0.05]" : "border-white/[0.06] bg-white/[0.01]"}`}
              >
                <SToggle
                  rgb="52,211,153"
                  on={tl.enabled}
                  onChange={(v) =>
                    setTools((list) => list?.map((x) => (x.name === tl.name ? { ...x, enabled: v } : x)) ?? null)
                  }
                />
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-1.5 font-mono text-[10px] text-slate-600">
                    <span className="text-emerald-400/90">
                      {prefix || "…"}.{tl.alias.trim() || tl.name}
                    </span>
                    <span>←</span>
                    <span>{tl.name}</span>
                  </div>
                  {tl.desc && <p className="truncate text-[10px] text-slate-600">{tl.desc}</p>}
                </div>
                <input
                  value={tl.alias}
                  onChange={(e) => {
                    if (!system)
                      setTools(
                        (list) => list?.map((x) => (x.name === tl.name ? { ...x, alias: e.target.value } : x)) ?? null,
                      );
                  }}
                  placeholder={t("mcp.aliasPh")}
                  disabled={system}
                  className="w-24 rounded-md border border-white/[0.09] bg-abyss-900/80 px-2 py-1 font-mono text-[10.5px] text-slate-200 outline-none focus:border-emerald-400/40 disabled:cursor-not-allowed disabled:opacity-50"
                />
              </div>
            ))}
          </div>
        )}
      </div>
    </SModal>
  );
}

export function McpScreen({ servers, patch, notify }: { servers: McpServer[]; patch: Patch; notify: Notify }) {
  const { t } = useT();
  const [modal, setModal] = useState<{ open: boolean; editing: McpServer | null }>({ open: false, editing: null });
  const [connecting, setConnecting] = useState<Record<string, "connecting" | "online">>({});
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const refresh = async () => patch({ mcpServers: await wsRequest<McpServer[]>(76) });

  const connect = async (s: McpServer) => {
    setConnecting((c) => ({ ...c, [s.id]: "connecting" }));
    await new Promise((r) => setTimeout(r, 1100));
    setConnecting((c) => ({ ...c, [s.id]: "online" }));
    notify("ok", t("mcp.handshake", { name: s.name, n: Array.isArray(s.tools) ? s.tools.length : 0 }));
  };

  return (
    <div className="mx-auto max-w-3xl">
      <ScreenHeader
        title={t("mcp.title")}
        count={servers.length}
        actionLabel={t("mcp.add")}
        onAction={() => setModal({ open: true, editing: null })}
      />
      <div className="space-y-2">
        {servers.map((s) => {
          const st = connecting[s.id];
          const tools = Array.isArray(s.tools) ? s.tools : [];
          const enabledTools = tools.filter((tl) => tl.enabled).length;
          const isExpanded = expanded.has(s.id);
          return (
            <div key={s.id} className="space-y-1.5">
              <Row>
                <button
                  onClick={() =>
                    setExpanded((current) => {
                      const next = new Set(current);
                      if (next.has(s.id)) next.delete(s.id);
                      else next.add(s.id);
                      return next;
                    })
                  }
                  className="grid h-7 w-7 shrink-0 place-items-center rounded-lg text-slate-500 transition-colors hover:bg-white/[0.07] hover:text-slate-200"
                  title={t(isExpanded ? "mcp.collapseTools" : "mcp.expandTools")}
                  aria-label={t(isExpanded ? "mcp.collapseTools" : "mcp.expandTools")}
                >
                  {isExpanded ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
                </button>
                <span
                  className={`grid h-9 w-9 shrink-0 place-items-center rounded-lg ${s.enabled ? "bg-emerald-400/[0.1] text-emerald-300" : "bg-white/[0.04] text-slate-600"}`}
                  style={s.enabled ? { boxShadow: "inset 0 0 0 1px rgba(52,211,153,0.25)" } : undefined}
                >
                  <Network size={15} />
                </span>
                <div className="min-w-0 flex-1">
                  <div className="flex flex-wrap items-center gap-2">
                    <span className={`text-[13px] font-medium ${s.enabled ? "text-slate-100" : "text-slate-500"}`}>
                      {s.name}
                    </span>
                    <span
                      className={`rounded border px-1.5 py-px font-mono text-[8.5px] tracking-wider ${TRANSPORTS[s.transport].cls}`}
                    >
                      {TRANSPORTS[s.transport].label}
                    </span>
                    <span className="rounded border border-emerald-400/20 bg-emerald-400/[0.06] px-1.5 py-px font-mono text-[9px] text-emerald-300">
                      {s.prefix}.*
                    </span>
                    {s.headers.length > 0 && (
                      <span className="flex items-center gap-1 rounded border border-white/[0.08] bg-white/[0.03] px-1.5 py-px font-mono text-[8.5px] text-slate-500">
                        <Link2 size={8} />
                        {s.headers.length} hdr
                      </span>
                    )}
                    {st === "online" && (
                      <span className="flex items-center gap-1 font-mono text-[9px] text-emerald-400">
                        <span
                          className="dot-live h-1.5 w-1.5 rounded-full bg-emerald-400"
                          style={{ color: "#34d399" }}
                        />
                        online
                      </span>
                    )}
                  </div>
                  <p className="mt-0.5 truncate font-mono text-[10px] text-slate-600">
                    {s.system ? t("mcp.system") : s.transport === "stdio" ? s.command : s.url}
                  </p>
                  <p className="font-mono text-[9px] text-slate-600">
                    {t("mcp.tools").toLowerCase()}: <span className="text-emerald-400/80">{enabledTools}</span>/
                    {tools.length}
                    {tools
                      .filter((tl) => tl.enabled)
                      .slice(0, 3)
                      .map((tl) => ` · ${s.prefix}.${tl.alias}`)
                      .join("")}
                  </p>
                </div>
                {!s.system && (
                  <button
                    onClick={() => void connect(s)}
                    disabled={st === "connecting"}
                    className="grid h-7 w-7 place-items-center rounded-lg text-slate-500 transition-colors hover:bg-white/[0.07] hover:text-emerald-300 disabled:opacity-60"
                    title={t("mcp.connect")}
                  >
                    {st === "connecting" ? <Loader2 size={13} className="animate-spin" /> : <Plug size={13} />}
                  </button>
                )}
                <button
                  onClick={() => setModal({ open: true, editing: s })}
                  className="grid h-7 w-7 place-items-center rounded-lg text-slate-500 transition-colors hover:bg-white/[0.07] hover:text-indigo-300"
                  title={t("common.edit")}
                >
                  <Pencil size={13} />
                </button>
                {!s.system && (
                  <button
                    onClick={async () => {
                      await wsRequest(80, { id: s.id });
                      await refresh();
                      notify("info", t("mcp.deleted", { name: s.name }));
                    }}
                    className="grid h-7 w-7 place-items-center rounded-lg text-slate-500 transition-colors hover:bg-rose-500/15 hover:text-rose-300"
                    title={t("common.delete")}
                  >
                    <Trash2 size={13} />
                  </button>
                )}
                <SToggle
                  rgb="52,211,153"
                  on={s.enabled}
                  onChange={async (v) => {
                    await wsRequest(78, { id: s.id, patch: { enabled: v } });
                    await refresh();
                  }}
                />
              </Row>
              <AnimatePresence initial={false}>
                {isExpanded && (
                  <motion.div
                    initial={{ opacity: 0, height: 0 }}
                    animate={{ opacity: 1, height: "auto" }}
                    exit={{ opacity: 0, height: 0 }}
                    className="overflow-hidden pl-14"
                  >
                    <div className="glass rounded-xl px-4 py-3">
                      <div className="mb-2 flex items-center justify-between text-[10px] uppercase tracking-wider text-slate-500">
                        <span>{t("mcp.toolsCount")}</span>
                        <span>{tools.length}</span>
                      </div>
                      {tools.length === 0 ? (
                        <p className="text-[11px] text-slate-600">{t("mcp.noTools")}</p>
                      ) : (
                        <div className="space-y-1.5">
                          {tools.map((tool) => (
                            <div
                              key={tool.name}
                              className="rounded-lg border border-white/[0.06] bg-white/[0.02] px-3 py-2"
                            >
                              <div className="flex flex-wrap items-center gap-2">
                                <span className="font-mono text-[10px] text-emerald-300">
                                  {s.prefix}.{tool.alias || tool.name}
                                </span>
                              </div>
                              {tool.description && (
                                <p className="mt-1 whitespace-pre-wrap text-[11px] leading-relaxed text-slate-500">
                                  {tool.description}
                                </p>
                              )}
                            </div>
                          ))}
                        </div>
                      )}
                    </div>
                  </motion.div>
                )}
              </AnimatePresence>
            </div>
          );
        })}
        {servers.length === 0 && <p className="py-8 text-center text-[12px] text-slate-600">{t("mcp.empty")}</p>}
      </div>
      <AnimatePresence>
        {modal.open && (
          <McpModal
            initial={modal.editing}
            notify={notify}
            onClose={() => setModal({ open: false, editing: null })}
            onSaved={refresh}
          />
        )}
      </AnimatePresence>
    </div>
  );
}

/* --------------------------------------------------------------- knowledge */

export function KbScreen({
  docs,
  quotaBytes,
  patch,
  notify,
}: {
  docs: KbDoc[];
  quotaBytes: number;
  patch: Patch;
  notify: Notify;
}) {
  const { t } = useT();
  const [query, setQuery] = useState("");
  const [selectedTags, setSelectedTags] = useState<string[]>([]);
  const [page, setPage] = useState(0);
  const [pageDocs, setPageDocs] = useState<KbDoc[]>(docs);
  const [hasNextPage, setHasNextPage] = useState(false);
  const [pageLoading, setPageLoading] = useState(false);
  const [pageCursors, setPageCursors] = useState<Record<number, string>>({ 0: "" });
  const [totalResults, setTotalResults] = useState(docs.length);
  const [allTags, setAllTags] = useState<string[]>([]);
  const loadingQuery = useRef<string | null>(null);
  const [expanded, setExpanded] = useState<string | null>(null);
  const [addOpen, setAddOpen] = useState(false);
  const [editing, setEditing] = useState<KbDoc | null>(null);
  const [reindexing, setReindexing] = useState(false);
  const [progress, setProgress] = useState(0);
  const [form, setForm] = useState({ title: "", source: "", kind: "doc" as KbDoc["kind"], tags: "", content: "" });
  const [link, setLink] = useState("");
  const [linkOpen, setLinkOpen] = useState(false);
  const [folder, setFolder] = useState("");
  const [folderFiles, setFolderFiles] = useState<{ path: string; name: string; ext: string; size: number }[]>([]);
  const [selectedFiles, setSelectedFiles] = useState<string[]>([]);
  const [folderOpen, setFolderOpen] = useState(false);
  const [extFilter, setExtFilter] = useState("");
  const [nameFilter, setNameFilter] = useState("");
  const [pickingFolder, setPickingFolder] = useState(false);
  const resetFolderSelection = () => {
    setFolder("");
    setFolderFiles([]);
    setSelectedFiles([]);
    setExtFilter("");
    setNameFilter("");
    setFolderOpen(false);
  };

  const KIND_META = {
    doc: { label: t("kb.kindDoc"), rgb: "129,140,248", icon: BookOpen },
    note: { label: t("kb.kindNote"), rgb: "34,211,238", icon: Pencil },
    link: { label: t("kb.kindLink"), rgb: "167,139,250", icon: Globe },
  } as const;

  const tags = useMemo(
    () =>
      allTags.length
        ? allTags
        : Array.from(new Set(docs.flatMap((d) => (Array.isArray(d.tags) ? d.tags : [])))).sort((a, b) =>
            a.localeCompare(b),
          ),
    [allTags, docs],
  );
  // Search and tag filtering are performed by the cursor query on the server.
  const visibleDocs = pageDocs;

  const totalSize = docs.reduce((a, d) => a + d.size, 0);
  const refresh = async () => {
    const result = await wsRequest<{ docs: KbDoc[]; quotaBytes: number }>(34);
    patch({
      kbDocs: (Array.isArray(result.docs) ? result.docs : []).map(normalizeKbDoc),
      kbQuotaBytes: result.quotaBytes,
    });
  };
  const loadPage = async (nextPage: number) => {
    setPageLoading(true);
    try {
      const cursor = nextPage === 0 ? "" : (pageCursors[nextPage] ?? "");
      const first = await wsRequest<{
        docs: KbDoc[];
        total: number;
        tags?: string[];
        nextLastId: string;
        hasMore: boolean;
      }>(34, { limit: 10, lastId: cursor, query, tags: selectedTags });
      const second = first.hasMore
        ? await wsRequest<{ docs: KbDoc[]; total: number; tags?: string[]; nextLastId: string; hasMore: boolean }>(34, {
            limit: 10,
            lastId: first.nextLastId,
            query,
            tags: selectedTags,
          })
        : { docs: [], total: first.total, nextLastId: first.nextLastId, hasMore: false };
      const combined = [...(first.docs ?? []), ...(second.docs ?? [])].map(normalizeKbDoc);
      setPageDocs(combined);
      setTotalResults(first.total);
      setAllTags(first.tags ?? []);
      setPage(nextPage);
      setHasNextPage(second.hasMore);
      setPageCursors((current) => ({ ...current, [nextPage + 1]: second.nextLastId }));
    } finally {
      setPageLoading(false);
    }
  };
  useEffect(() => {
    const key = `${query}\u0000${selectedTags.join("\u0001")}`;
    if (loadingQuery.current === key) return;
    loadingQuery.current = key;
    setPage(0);
    setPageCursors({ 0: "" });
    void loadPage(0);
  }, [query, selectedTags]);

  const reindex = async () => {
    setReindexing(true);
    setProgress(0);
    const iv = setInterval(() => setProgress((p) => Math.min(p + 4 + Math.random() * 10, 96)), 120);
    const res = await wsRequest<{ indexed: number; chunks: number }>(40);
    clearInterval(iv);
    setProgress(100);
    setTimeout(() => {
      setReindexing(false);
      setProgress(0);
    }, 500);
    notify("ok", t("kb.reindexDone", { docs: res.indexed, chunks: res.chunks }));
  };

  const add = async () => {
    if (editing) {
      await wsRequest(35, {
        id: editing.id,
        title: form.title.trim(),
        source: form.source.trim(),
        kind: form.kind,
        tags: form.tags
          .split(",")
          .map((tag) => tag.trim())
          .filter(Boolean),
        content: form.content || editing.content,
      });
      setAddOpen(false);
      setEditing(null);
      await refresh();
      notify("ok", t("kb.updated"));
      return;
    }
    if (form.kind === "link") {
      await importLink();
      return;
    }
    if (form.kind === "doc" && selectedFiles.length) {
      await commitFolderFiles();
      return;
    }
    await wsRequest(35, {
      title: form.title.trim(),
      source: form.source.trim() || t("kb.manualSrc"),
      kind: form.kind,
      tags: form.tags
        .split(",")
        .map((tg) => tg.trim())
        .filter(Boolean),
      content: form.content,
    });
    setAddOpen(false);
    setForm({ title: "", source: "", kind: "doc", tags: "", content: "" });
    await refresh();
    notify("ok", t("kb.added"));
  };
  const importLink = async () => {
    await wsRequest(36, {
      url: form.source.trim(),
      title: form.title.trim(),
      tags: form.tags
        .split(",")
        .map((tag) => tag.trim())
        .filter(Boolean),
    });
    setLinkOpen(false);
    setLink("");
    setAddOpen(false);
    setForm({ title: "", source: "", kind: "doc", tags: "", content: "" });
    await refresh();
    notify("ok", t("kb.imported"));
  };
  const chooseFolder = async () => {
    if (pickingFolder) return;
    setPickingFolder(true);
    try {
      const picked = await pickFolder();
      if (!picked) return;
      const files = await wsRequest<typeof folderFiles>(37, { path: picked.folderPath });
      setFolder(picked.folderPath);
      setFolderFiles(files);
      setSelectedFiles([]);
      setFolderOpen(true);
    } catch (error) {
      notify("err", error instanceof Error ? error.message : t("kb.folderImportError"));
    } finally {
      setPickingFolder(false);
    }
  };
  const importFolderFiles = async () => {
    setFolderOpen(false);
  };
  const commitFolderFiles = async () => {
    await wsRequest(38, {
      folder,
      files: selectedFiles,
      title: form.title.trim(),
      tags: form.tags
        .split(",")
        .map((tag) => tag.trim())
        .filter(Boolean),
    });
    setAddOpen(false);
    setForm({ title: "", source: "", kind: "doc", tags: "", content: "" });
    resetFolderSelection();
    await refresh();
    notify("ok", t("kb.imported"));
  };
  const deleteDocument = async (id: string) => {
    await wsRequest(39, { id });
    await refresh();
    // `refresh` updates the bootstrap collection, while the paginated view is
    // kept separately. Reload the current cursor page so the deleted row is
    // removed immediately and total/page state stays consistent.
    await loadPage(page);
    notify("info", t("kb.deleted"));
  };
  const visibleFiles = folderFiles.filter(
    (f) =>
      (!extFilter || f.ext === extFilter.replace(/^\./, "").toLowerCase()) &&
      (!nameFilter || wildcard(nameFilter, f.name)),
  );
  const toggleAllVisible = () => {
    const paths = visibleFiles.map((file) => file.path);
    const allSelected = paths.length > 0 && paths.every((path) => selectedFiles.includes(path));
    setSelectedFiles((current) =>
      allSelected ? current.filter((path) => !paths.includes(path)) : Array.from(new Set([...current, ...paths])),
    );
  };

  return (
    <div className="mx-auto max-w-3xl">
      <div className="mb-5 flex flex-wrap items-center justify-between gap-3">
        <div>
          <h2 className="font-display text-lg font-semibold text-slate-100">{t("kb.title")}</h2>
          <p className="font-mono text-[10px] tracking-[0.2em] text-slate-600">{t("kb.sub")}</p>
        </div>
        <div className="flex gap-2">
          <SBtn onClick={() => void reindex()} disabled={reindexing}>
            {reindexing ? <Loader2 size={13} className="animate-spin" /> : <RefreshCw size={13} />}
            {reindexing ? t("kb.indexing", { p: Math.round(progress) }) : t("kb.reindex")}
          </SBtn>
          <button
            onClick={() => {
              resetFolderSelection();
              setEditing(null);
              setForm({ title: "", source: "", kind: "doc", tags: "", content: "" });
              setAddOpen(true);
            }}
            className="flex items-center gap-2 rounded-lg bg-gradient-to-r from-indigo-500 to-cyan-500 px-3.5 py-2 text-[12px] font-semibold text-white shadow-[0_0_24px_-8px_rgba(129,140,248,0.7)] transition-all hover:brightness-115"
          >
            <Plus size={14} />
            {t("kb.add")}
          </button>
        </div>
      </div>

      {linkOpen && (
        <SModal
          title={t("kb.importLink")}
          onClose={() => setLinkOpen(false)}
          footer={
            <>
              <SBtn onClick={() => setLinkOpen(false)}>{t("common.cancel")}</SBtn>
              <SBtn primary disabled={!link.trim()} onClick={() => void importLink()}>
                {t("common.add")}
              </SBtn>
            </>
          }
        >
          <SField label={t("kb.source")}>
            <input
              value={link}
              onChange={(e) => setLink(e.target.value)}
              placeholder="https://example.com/document.md"
              className={inputCls}
            />
          </SField>
        </SModal>
      )}
      {folderOpen && (
        <SModal
          title={t("kb.importFolder")}
          onClose={() => setFolderOpen(false)}
          footer={
            <>
              <SBtn onClick={() => setFolderOpen(false)}>{t("common.cancel")}</SBtn>
              <SBtn primary disabled={!selectedFiles.length} onClick={() => void importFolderFiles()}>
                {t("kb.importSelected", { n: selectedFiles.length })}
              </SBtn>
            </>
          }
        >
          <p className="mb-2 truncate font-mono text-[10px] text-slate-600">{folder}</p>
          <div className="mb-2 grid grid-cols-2 gap-2">
            <input
              value={extFilter}
              onChange={(e) => setExtFilter(e.target.value)}
              placeholder={t("kb.extensionFilter")}
              className={inputCls}
            />
            <input
              value={nameFilter}
              onChange={(e) => setNameFilter(e.target.value)}
              placeholder={t("kb.wildcardFilter")}
              className={inputCls}
            />
          </div>
          <div className="mb-2 flex justify-end">
            <SBtn onClick={toggleAllVisible} disabled={!visibleFiles.length}>
              {t("kb.selectAll")}
            </SBtn>
          </div>
          <div className="max-h-72 space-y-1 overflow-y-auto">
            {visibleFiles.map((f) => {
              const selected = selectedFiles.includes(f.path);
              const toggle = () =>
                setSelectedFiles((xs) => (selected ? xs.filter((x) => x !== f.path) : [...xs, f.path]));
              return (
                <div
                  role="button"
                  tabIndex={0}
                  key={f.path}
                  onClick={toggle}
                  onKeyDown={(e) => {
                    if (e.key === "Enter" || e.key === " ") toggle();
                  }}
                  className="flex w-full cursor-pointer items-center gap-2 rounded border border-white/[0.06] px-2 py-1.5 text-left font-mono text-[10px] text-slate-300"
                >
                  <span className="min-w-0 flex-1 truncate">{f.path}</span>
                  <SToggle
                    rgb="129,140,248"
                    on={selected}
                    onChange={(value) =>
                      setSelectedFiles((xs) => (value ? [...xs, f.path] : xs.filter((x) => x !== f.path)))
                    }
                  />
                </div>
              );
            })}
          </div>
        </SModal>
      )}

      <div className="glass mb-4 rounded-xl p-3.5">
        <div className="mb-1.5 flex items-center justify-between">
          <span className="font-mono text-[9.5px] tracking-[0.2em] text-slate-600">{t("kb.storage")}</span>
          <span className="font-mono text-[10px] text-slate-400">
            {fmtSize(totalSize)} / {fmtSize(quotaBytes)}
          </span>
        </div>
        <div className="mb-3 h-1.5 overflow-hidden rounded-full bg-white/[0.07]">
          <motion.div
            className="h-full rounded-full bg-gradient-to-r from-indigo-400 to-cyan-400"
            animate={{ width: `${Math.min((totalSize / quotaBytes) * 100, 100)}%` }}
            transition={{ duration: 0.5 }}
          />
        </div>
        <div className="relative">
          <Search size={13} className="absolute left-3 top-1/2 -translate-y-1/2 text-slate-600" />
          <input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder={t("kb.searchPh")}
            className={`${inputCls} pl-8`}
          />
        </div>
        {tags.length > 0 && (
          <div className="mt-3 flex flex-wrap items-center gap-1.5">
            <span className="mr-1 font-mono text-[9px] uppercase tracking-wider text-slate-600">
              {t("kb.tagsList")}
            </span>
            {tags.map((tag) => (
              <button
                key={tag}
                onClick={() =>
                  setSelectedTags((current) =>
                    current.includes(tag) ? current.filter((item) => item !== tag) : [...current, tag],
                  )
                }
                className={`rounded-md border px-2 py-1 font-mono text-[10px] transition-colors ${selectedTags.includes(tag) ? "border-amber-400/50 bg-amber-400/15 text-amber-300" : "border-amber-400/20 bg-amber-400/[0.04] text-amber-400/75 hover:border-amber-400/40 hover:text-amber-300"}`}
              >
                #{tag}
              </button>
            ))}
          </div>
        )}
      </div>

      <div className="space-y-2">
        {visibleDocs.map((d) => {
          const meta = KIND_META[d.kind];
          const KIcon = meta.icon;
          const open = expanded === d.id;
          return (
            <Row key={d.id}>
              <button
                onClick={() => setExpanded(open ? null : d.id)}
                className="grid h-7 w-7 shrink-0 place-items-center rounded-lg text-slate-500 transition-colors hover:bg-white/[0.07] hover:text-slate-200"
                title={t("kb.preview")}
              >
                <motion.span animate={{ rotate: open ? 180 : 0 }}>
                  <ChevronDown size={14} />
                </motion.span>
              </button>
              <span
                className="grid h-9 w-9 shrink-0 place-items-center rounded-lg"
                style={{ background: `rgba(${meta.rgb},0.1)`, boxShadow: `inset 0 0 0 1px rgba(${meta.rgb},0.22)` }}
              >
                <KIcon size={14} style={{ color: `rgb(${meta.rgb})` }} />
              </span>
              <button className="min-w-0 flex-1 text-left" onClick={() => setExpanded(open ? null : d.id)}>
                <div className="flex items-center justify-between gap-3">
                  <span className="min-w-0 truncate text-[13px] font-medium text-slate-100">{d.title}</span>
                  <span className="shrink-0 font-mono text-[9px] text-slate-600">
                    {t("kb.updatedAt", { date: formatMessageTime(d.updatedAt || "") })}
                  </span>
                </div>
                <div className="mt-0.5 flex flex-wrap items-center gap-2">
                  <span className="rounded border border-white/[0.08] bg-white/[0.03] px-1.5 py-px font-mono text-[8.5px] uppercase tracking-wider text-slate-500">
                    {meta.label}
                  </span>
                  <span className="font-mono text-[9px] text-slate-600">{fmtSize(d.size)}</span>
                </div>
                <p className="mt-0.5 flex items-center gap-1.5 truncate font-mono text-[10px] text-slate-600">
                  <Link2 size={9} />
                  {d.source}
                  {(Array.isArray(d.tags) ? d.tags : []).map((tg) => (
                    <span
                      key={tg}
                      className="rounded border border-amber-400/20 bg-amber-400/[0.06] px-1 py-px text-[8.5px] text-amber-300/80"
                    >
                      #{tg}
                    </span>
                  ))}
                </p>
                <AnimatePresence>
                  {open && (
                    <motion.p
                      initial={{ height: 0, opacity: 0 }}
                      animate={{ height: "auto", opacity: 1 }}
                      exit={{ height: 0, opacity: 0 }}
                      className="overflow-hidden text-[11.5px] leading-relaxed text-slate-400"
                    >
                      <span className="mt-2 block max-h-80 overflow-y-auto whitespace-pre-wrap break-words rounded-lg border border-white/[0.06] bg-abyss-900/60 p-2.5 font-mono text-[10.5px] leading-relaxed text-slate-300">
                        {d.content}
                      </span>
                    </motion.p>
                  )}
                </AnimatePresence>
              </button>
              <button
                onClick={() => {
                  setEditing(d);
                  setForm({
                    title: d.title,
                    source: d.source,
                    kind: d.kind as KbDoc["kind"],
                    tags: d.tags.join(", "),
                    content: d.content,
                  });
                  setAddOpen(true);
                }}
                className="grid h-7 w-7 place-items-center rounded-lg text-slate-500 transition-colors hover:bg-white/[0.07] hover:text-indigo-300"
                title={t("common.edit")}
              >
                <Pencil size={13} />
              </button>
              <button
                onClick={() => void deleteDocument(d.id)}
                className="grid h-7 w-7 place-items-center rounded-lg text-slate-500 transition-colors hover:bg-rose-500/15 hover:text-rose-300"
                title={t("common.delete")}
              >
                <Trash2 size={13} />
              </button>
            </Row>
          );
        })}
        {visibleDocs.length === 0 && (
          <p className="py-8 text-center text-[12px] text-slate-600">
            {query || selectedTags.length ? t("kb.notFound") : t("kb.empty")}
          </p>
        )}
      </div>
      {(page > 0 || hasNextPage) && (
        <div className="mt-4 flex items-center justify-end gap-2 font-mono text-[10px] text-slate-500">
          <span>{page + 1}</span>
          <button
            disabled={page === 0 || pageLoading}
            onClick={() => void loadPage(page - 1)}
            className="grid h-7 w-7 place-items-center rounded-md border border-white/10 hover:bg-white/[0.06] disabled:cursor-not-allowed disabled:opacity-30"
            title={t("kb.previous")}
          >
            <ChevronLeft size={14} />
          </button>
          <button
            disabled={!hasNextPage || pageLoading}
            onClick={() => void loadPage(page + 1)}
            className="grid h-7 w-7 place-items-center rounded-md border border-white/10 hover:bg-white/[0.06] disabled:cursor-not-allowed disabled:opacity-30"
            title={t("kb.next")}
          >
            <ChevronRight size={14} />
          </button>
        </div>
      )}

      <AnimatePresence>
        {addOpen && (
          <SModal
            layer={40}
            title={editing ? t("kb.editTitle") : t("kb.newTitle")}
            subtitle={t("kb.newSub")}
            onClose={() => {
              setAddOpen(false);
              setEditing(null);
            }}
            footer={
              <>
                <SBtn
                  onClick={() => {
                    setAddOpen(false);
                    setEditing(null);
                  }}
                >
                  {t("common.cancel")}
                </SBtn>
                <SBtn
                  primary
                  disabled={
                    !form.title.trim() ||
                    (editing
                      ? false
                      : form.kind === "note"
                        ? !form.content.trim()
                        : form.kind === "link"
                          ? !form.source.trim()
                          : !selectedFiles.length)
                  }
                  onClick={() => void add()}
                >
                  <Check size={13} />
                  {editing ? t("common.save") : t("common.add")}
                </SBtn>
              </>
            }
          >
            <div className="grid grid-cols-[1fr_130px] gap-3">
              <SField label={t("common.name")}>
                <input
                  value={form.title}
                  onChange={(e) => setForm({ ...form, title: e.target.value })}
                  placeholder="Deploy runbook"
                  className={inputCls}
                />
              </SField>
              <SField label={t("common.type")}>
                <select
                  value={form.kind}
                  onChange={(e) => setForm({ ...form, kind: e.target.value as KbDoc["kind"] })}
                  className={inputCls}
                >
                  <option value="doc">{t("kb.kindDoc")}</option>
                  <option value="note">{t("kb.kindNote")}</option>
                  <option value="link">{t("kb.kindLink")}</option>
                </select>
              </SField>
            </div>
            {(form.kind === "link" || editing) && (
              <SField label={t("kb.source")}>
                <input
                  value={form.source}
                  onChange={(e) => setForm({ ...form, source: e.target.value })}
                  placeholder="https://example.com/document.md"
                  className={inputCls}
                />
              </SField>
            )}
            {form.kind === "doc" && (
              <div className="mb-3 rounded-lg border border-white/[0.07] bg-abyss-900/50 p-3">
                <div className="flex items-center justify-between gap-3">
                  <div className="min-w-0">
                    <p className="text-[11.5px] text-slate-300">
                      {folder ? t("kb.folderSelected") : t("kb.chooseFolderHint")}
                    </p>
                    {folder && (
                      <p className="truncate font-mono text-[9.5px] text-slate-600">
                        {folder} · {selectedFiles.length}
                      </p>
                    )}
                  </div>
                  <SBtn onClick={() => void chooseFolder()} disabled={pickingFolder}>
                    {pickingFolder ? <Loader2 size={12} className="animate-spin" /> : <FolderOpen size={12} />}
                    {t("kb.chooseFolder")}
                  </SBtn>
                </div>
              </div>
            )}
            <SField label={t("kb.tags")}>
              <input
                value={form.tags}
                onChange={(e) => setForm({ ...form, tags: e.target.value })}
                placeholder="deploy, ci"
                className={inputCls}
              />
            </SField>
            {(form.kind === "note" || editing) && (
              <SField label={t("kb.content")}>
                <textarea
                  value={form.content}
                  onChange={(e) => setForm({ ...form, content: e.target.value })}
                  rows={4}
                  placeholder={t("kb.contentPh")}
                  className={`${inputCls} resize-none`}
                />
              </SField>
            )}
          </SModal>
        )}
      </AnimatePresence>
    </div>
  );
}
