import { useEffect, useState } from "react";
import { ChevronLeft, ChevronRight, Pencil, Plus, Trash2 } from "lucide-react";
import { formatMessageTime } from "../../lib/data";
import type { LongTermNote, MemoryPage, TopicMemory } from "../../lib/data";
import { wsRequest } from "../../lib/api";
import { useT } from "../../lib/i18n";
import { SBtn, SField, SModal, ScreenHeader, inputCls } from "./SkillsProviders";
import type { Notify } from "./SkillsProviders";

type Patch = (p: Partial<{ longTermNotes: LongTermNote[]; topicMemories: TopicMemory[] }>) => void;
type Item = LongTermNote | TopicMemory;
type Paging = { page: number; cursors: string[]; nextCursor: string; hasMore: boolean };

export function MemoryScreen({
  notes,
  topics,
  patch,
  notify,
}: {
  notes: LongTermNote[];
  topics: TopicMemory[];
  patch: Patch;
  notify: Notify;
}) {
  const { t } = useT();
  const [kind, setKind] = useState<"note" | "topic">("note");
  const [editing, setEditing] = useState<Item | null>(null);
  const [reindexing, setReindexing] = useState(false);
  const [notePaging, setNotePaging] = useState<Paging>({ page: 0, cursors: [""], nextCursor: "", hasMore: false });
  const [topicPaging, setTopicPaging] = useState<Paging>({ page: 0, cursors: [""], nextCursor: "", hasMore: false });
  const loadPage = async (target: "note" | "topic", cursor: string, page: number) => {
    if (target === "note") {
      const result = await wsRequest<MemoryPage<LongTermNote>>(44, { limit: 20, cursor });
      patch({ longTermNotes: Array.isArray(result.items) ? result.items : [] });
      setNotePaging((current) => {
        const cursors = [...current.cursors];
        cursors[page] = cursor;
        return { page, cursors, nextCursor: result.nextCursor ?? "", hasMore: Boolean(result.hasMore) };
      });
      return;
    }
    const result = await wsRequest<MemoryPage<TopicMemory>>(47, { limit: 20, cursor });
    patch({ topicMemories: Array.isArray(result.items) ? result.items : [] });
    setTopicPaging((current) => {
      const cursors = [...current.cursors];
      cursors[page] = cursor;
      return { page, cursors, nextCursor: result.nextCursor ?? "", hasMore: Boolean(result.hasMore) };
    });
  };
  const refresh = async () => {
    await Promise.all([loadPage("note", "", 0), loadPage("topic", "", 0)]);
  };
  useEffect(() => {
    void refresh();
  }, []);
  const items = kind === "note" ? notes : topics;
  const paging = kind === "note" ? notePaging : topicPaging;
  const remove = async (item: Item) => {
    try {
      await wsRequest(kind === "note" ? 46 : 49, { id: item.id });
      await refresh();
      notify("info", t("memory.deleted"));
    } catch (error) {
      notify("err", error instanceof Error ? error.message : String(error));
    }
  };
  const reindex = async () => {
    setReindexing(true);
    try {
      const result = await wsRequest<{ notes: number; topics: number }>(50);
      await refresh();
      notify("ok", t("memory.reindexDone", result));
    } catch (error) {
      notify("err", error instanceof Error ? error.message : String(error));
    } finally {
      setReindexing(false);
    }
  };
  const changePage = async (direction: -1 | 1) => {
    const nextPage = paging.page + direction;
    if (nextPage < 0 || (direction > 0 && !paging.hasMore)) return;
    const cursor = direction > 0 ? paging.nextCursor : paging.cursors[nextPage];
    await loadPage(kind, cursor, nextPage);
  };
  return (
    <div className="mx-auto max-w-3xl">
      <ScreenHeader
        title={t("memory.title")}
        count={items.length}
        actionLabel={t("memory.add")}
        onAction={() => setEditing({ id: "", title: "", content: "", summary: "", tags: [] } as Item)}
        secondaryLabel={t("memory.reindex")}
        secondaryBusy={reindexing}
        onSecondary={() => void reindex()}
      />
      <div className="mb-4 flex items-center gap-2">
        <SBtn primary={kind === "note"} onClick={() => setKind("note")}>
          {t("memory.notes")}
        </SBtn>
        <SBtn primary={kind === "topic"} onClick={() => setKind("topic")}>
          {t("memory.topics")}
        </SBtn>
      </div>
      <div className="space-y-2">
        {items.map((item) => (
          <div key={item.id} className="glass flex items-start gap-3 rounded-xl p-4">
            <div className="min-w-0 flex-1">
              <div className="flex items-center justify-between gap-3">
                <p className="min-w-0 truncate font-display text-[13px] font-semibold text-slate-100">{item.title}</p>
                <span className="shrink-0 font-mono text-[9px] text-slate-600">
                  {t("memory.updatedAt", { date: formatMessageTime(item.updatedAt || "") })}
                </span>
              </div>
              <p className="mt-1 line-clamp-2 whitespace-pre-wrap text-[11px] text-slate-400">
                {"content" in item ? item.content : item.summary}
              </p>
              <div className="mt-2 flex flex-wrap gap-2 font-mono text-[9px]">
                <span className="text-indigo-300">{t("memory.profileScope")}</span>
                {item.tags.length > 0 && (
                  <span className="text-amber-300">{item.tags.map((tag) => `#${tag}`).join(" ")}</span>
                )}
              </div>
            </div>
            <button
              onClick={() => setEditing(item)}
              className="grid h-7 w-7 place-items-center rounded-lg text-slate-500 hover:bg-white/[0.07] hover:text-indigo-300"
              title={t("common.edit")}
            >
              <Pencil size={13} />
            </button>
            <button
              onClick={() => void remove(item)}
              className="grid h-7 w-7 place-items-center rounded-lg text-slate-500 hover:bg-rose-500/15 hover:text-rose-300"
              title={t("common.delete")}
            >
              <Trash2 size={13} />
            </button>
          </div>
        ))}
      </div>
      <div className="mt-4 flex items-center justify-end gap-2 font-mono text-[10px] text-slate-500">
        <span>{paging.page + 1}</span>
        <button
          disabled={paging.page === 0}
          onClick={() => void changePage(-1)}
          className="grid h-7 w-7 place-items-center rounded-md border border-white/10 hover:bg-white/[0.06] disabled:opacity-30"
        >
          <ChevronLeft size={14} />
        </button>
        <button
          disabled={!paging.hasMore}
          onClick={() => void changePage(1)}
          className="grid h-7 w-7 place-items-center rounded-md border border-white/10 hover:bg-white/[0.06] disabled:opacity-30"
        >
          <ChevronRight size={14} />
        </button>
      </div>
      {editing && (
        <MemoryModal
          kind={kind}
          item={editing}
          onClose={() => setEditing(null)}
          onSaved={async () => {
            await refresh();
            setEditing(null);
            notify("ok", t("memory.saved"));
          }}
        />
      )}
    </div>
  );
}

function MemoryModal({
  kind,
  item,
  onClose,
  onSaved,
}: {
  kind: "note" | "topic";
  item: Item;
  onClose: () => void;
  onSaved: () => Promise<void>;
}) {
  const { t } = useT();
  const [title, setTitle] = useState(item.title);
  const [content, setContent] = useState("content" in item ? item.content : item.summary);
  const [tags, setTags] = useState(item.tags.join(", "));
  const save = async () => {
    const payload = {
      id: item.id || crypto.randomUUID(),
      title: title.trim(),
      [kind === "note" ? "content" : "summary"]: content,
      tags: tags
        .split(",")
        .map((x) => x.trim())
        .filter(Boolean),
    };
    await wsRequest(kind === "note" ? 45 : 48, payload);
    await onSaved();
  };
  return (
    <SModal
      title={item.id ? t("memory.edit") : t("memory.new")}
      onClose={onClose}
      footer={
        <>
          <SBtn onClick={onClose}>{t("common.cancel")}</SBtn>
          <SBtn primary disabled={!title.trim() || !content.trim()} onClick={() => void save()}>
            {t("common.save")}
          </SBtn>
        </>
      }
    >
      <SField label={t("common.name")}>
        <input value={title} onChange={(event) => setTitle(event.target.value)} className={inputCls} />
      </SField>
      <SField label={t("memory.tags")}>
        <input value={tags} onChange={(event) => setTags(event.target.value)} className={inputCls} />
      </SField>
      <SField label={t("memory.content")}>
        <textarea
          value={content}
          onChange={(event) => setContent(event.target.value)}
          rows={10}
          className={`${inputCls} resize-y font-mono text-[11px]`}
        />
      </SField>
    </SModal>
  );
}
