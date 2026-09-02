import { useEffect, useRef, useState } from "react";
import { AnimatePresence, motion } from "framer-motion";
import { Check, CheckCheck, ChevronDown, Network, Pencil, Puzzle, Trash2, X } from "lucide-react";
import { ACCENTS, SKILL_ICON_KEYS, SKILL_ICONS } from "../../lib/data";
import type {
  AccentKey,
  AgentConfig,
  AgentEntry,
  CompactionLevel,
  IconKey,
  LiveAgent,
  McpServer,
  Provider,
  SkillGroup,
} from "../../lib/data";
import { wsRequest } from "../../lib/api";
import type { Db } from "../../lib/api";
import { useT } from "../../lib/i18n";
import { SBtn, SField, SModal, SSelect, ScreenHeader, inputCls } from "./SkillsProviders";
import type { Notify } from "./SkillsProviders";

type Patch = (p: Partial<Db>) => void;

const blankForm = (models: string[]): AgentConfig => ({
  name: "",
  description: "",
  systemPrompt: "",
  mainModels: models.length ? [models[0]] : [],
  compactionModel: models[0] ?? "openai/gpt-4o-mini",
  compactionLevel: "balanced",
  memoryModel: "",
  iconKey: "bot",
  accent: "indigo",
  skillGroupIds: [],
  mcpIds: [],
});

export function AgentsScreen({
  agents,
  providers,
  groups,
  mcpServers,
  patch,
  notify,
}: {
  agents: LiveAgent[];
  providers: Provider[];
  groups: SkillGroup[];
  mcpServers: McpServer[];
  patch: Patch;
  notify: Notify;
}) {
  const { t } = useT();
  const [modal, setModal] = useState<{ mode: "create" } | { mode: "edit"; agent: LiveAgent } | null>(null);
  const [form, setForm] = useState<AgentConfig | null>(null);
  const [mainModelsOpen, setMainModelsOpen] = useState(false);
  const mainModelsRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    if (!mainModelsOpen) return;
    const closeOnOutsideClick = (event: MouseEvent) => {
      const target = event.target as Node;
      if (!mainModelsRef.current?.contains(target)) setMainModelsOpen(false);
    };
    document.addEventListener("mousedown", closeOnOutsideClick);
    return () => document.removeEventListener("mousedown", closeOnOutsideClick);
  }, [mainModelsOpen]);

  const refresh = async () => patch({ agents: await wsRequest<AgentEntry[]>(17) });

  const modelOptions = providers
    .filter((p) => p.enabled)
    .flatMap((p) => (p.models ?? []).map((m) => `${m}@${p.name}`))
    .sort((a, b) => a.localeCompare(b, undefined, { sensitivity: "base" }));

  const openCreate = () => {
    setForm(blankForm(modelOptions));
    setMainModelsOpen(false);
    setModal({ mode: "create" });
  };

  const openEdit = (a: LiveAgent) => {
    const entry = a as LiveAgent & { iconKey?: IconKey };
    const iconKey: IconKey =
      entry.iconKey ?? (Object.keys(SKILL_ICONS) as IconKey[]).find((k) => SKILL_ICONS[k] === a.icon) ?? "bot";
    setForm({
      name: a.name,
      description: a.description,
      systemPrompt: a.systemPrompt,
      // Providers can be disabled after an agent was saved. Do not keep such
      // models as hidden selections that reappear when another model is added.
      mainModels: a.mainModels.filter((model) => modelOptions.includes(model)),
      compactionModel: a.compactionModel,
      compactionLevel: a.compactionLevel ?? "balanced",
      memoryModel: a.memoryModel ?? "",
      iconKey,
      accent: a.accent,
      skillGroupIds: a.skillGroupIds ?? [],
      mcpIds: a.mcpIds ?? [],
    });
    setMainModelsOpen(false);
    setModal({ mode: "edit", agent: a });
  };

  const save = async () => {
    if (!form || !modal) return;
    const payload = {
      ...form,
      mainModels: (form.mainModels ?? []).filter((model) => modelOptions.includes(model)),
      skillGroupIds: form.skillGroupIds ?? [],
      mcpIds: form.mcpIds ?? [],
    };
    if (modal.mode === "edit") await wsRequest(19, { id: modal.agent.id, patch: payload });
    else await wsRequest(18, payload);
    const createdName = form.name;
    setModal(null);
    await refresh();
    notify("ok", modal.mode === "edit" ? t("agents.saved") : t("agents.created", { name: createdName }));
  };

  const remove = async (a: LiveAgent) => {
    if (agents.length <= 1) {
      notify("err", t("agents.lastError"));
      return;
    }
    await wsRequest(20, { id: a.id });
    await refresh();
    notify("info", t("agents.deleted", { name: a.name }));
  };

  const Select = ({ value, onChange, label }: { value: string; onChange: (v: string) => void; label: string }) => (
    <SField label={label}>
      <SSelect
        value={value}
        onChange={onChange}
        options={[
          ...(label === t("agents.compactionModel") || label === t("agents.memoryModel")
            ? [{ value: "", label: t("agents.mainModelVirtual") }]
            : []),
          ...modelOptions.map((m) => {
            const at = m.lastIndexOf("@");
            return { value: m, label: at > 0 ? `${m.slice(0, at)} · ${m.slice(at + 1)}` : m };
          }),
          ...(value && !modelOptions.includes(value) ? [{ value, label: value }] : []),
        ]}
      />
    </SField>
  );

  return (
    <div className="mx-auto max-w-3xl">
      <ScreenHeader
        title={t("agents.title")}
        count={agents.length}
        actionLabel={t("agents.add")}
        onAction={openCreate}
      />

      <div className="grid gap-2 sm:grid-cols-2">
        {agents.map((a) => {
          const acc = ACCENTS[a.accent];
          return (
            <motion.div
              layout
              key={a.id}
              initial={{ opacity: 0, y: 8 }}
              animate={{ opacity: 1, y: 0 }}
              className="glass group flex flex-col rounded-xl p-4 transition-colors hover:border-white/[0.14]"
            >
              <div className="mb-2 flex items-center gap-2.5">
                <span
                  className="grid h-9 w-9 place-items-center rounded-lg"
                  style={{
                    background: `rgba(${acc.rgb},0.12)`,
                    boxShadow: `inset 0 0 0 1px rgba(${acc.rgb},0.25), 0 0 18px -6px rgba(${acc.rgb},0.55)`,
                  }}
                >
                  <a.icon size={15} style={{ color: acc.hex }} />
                </span>
                <div className="min-w-0 flex-1">
                  <p className="truncate text-[13px] font-semibold text-slate-100">{a.name}</p>
                  <p className="truncate text-[10.5px] text-slate-500">{a.description}</p>
                </div>
                <button
                  onClick={() => void remove(a)}
                  className="grid h-7 w-7 place-items-center rounded-lg text-slate-600 opacity-0 transition-all hover:bg-rose-500/15 hover:text-rose-300 group-hover:opacity-100"
                  title={t("common.delete")}
                >
                  <Trash2 size={13} />
                </button>
              </div>
              <div className="mb-3 flex flex-wrap gap-1">
                <span className="rounded border border-indigo-400/25 bg-indigo-400/[0.07] px-1.5 py-px font-mono text-[9px] text-indigo-300">
                  main: {(a.mainModels ?? []).join(", ") || t("agents.disabled")}
                </span>
                {a.compactionModel && (
                  <span className="rounded border border-amber-400/20 bg-amber-400/[0.06] px-1.5 py-px font-mono text-[9px] text-amber-300/90">
                    ctx: {a.compactionModel}
                  </span>
                )}
                {a.memoryModel && (
                  <span className="rounded border border-cyan-400/20 bg-cyan-400/[0.06] px-1.5 py-px font-mono text-[9px] text-cyan-300/90">
                    mem: {a.memoryModel}
                  </span>
                )}
              </div>
              <div className="mt-auto flex items-center justify-between">
                <span className="flex items-center gap-2 font-mono text-[9.5px] text-slate-600">
                  <span className="flex items-center gap-1">
                    <Puzzle size={10} className="text-violet-300" />
                    {(a.skillGroupIds ?? []).length}
                  </span>
                  <span className="flex items-center gap-1">
                    <Network size={10} className="text-emerald-300" />
                    {(a.mcpIds ?? []).length}
                  </span>
                </span>
                <button
                  onClick={() => openEdit(a)}
                  className="flex items-center gap-1.5 rounded-lg border border-white/10 bg-white/[0.04] px-2.5 py-1.5 text-[11px] font-medium text-slate-200 transition-colors hover:border-indigo-400/40 hover:text-indigo-200"
                >
                  <Pencil size={11} />
                  {t("agents.configure")}
                </button>
              </div>
            </motion.div>
          );
        })}
      </div>

      <AnimatePresence>
        {modal && form && (
          <SModal
            title={modal.mode === "edit" ? t("agents.settingsTitle", { name: modal.agent.name }) : t("agents.newTitle")}
            subtitle={t("agents.settingsSub")}
            onClose={() => setModal(null)}
            w="max-w-xl"
            footer={
              <>
                <SBtn onClick={() => setModal(null)}>{t("common.cancel")}</SBtn>
                <SBtn primary disabled={!form.name.trim()} onClick={() => void save()}>
                  <Check size={13} />
                  {t("common.save")}
                </SBtn>
              </>
            }
          >
            <div className="grid grid-cols-2 gap-3">
              <SField label={t("common.name")}>
                <input
                  value={form.name}
                  onChange={(e) => setForm({ ...form, name: e.target.value })}
                  placeholder={t("agents.namePh")}
                  className={inputCls}
                />
              </SField>
              <SField label={t("common.description")}>
                <input
                  value={form.description}
                  onChange={(e) => setForm({ ...form, description: e.target.value })}
                  placeholder={t("agents.descPh")}
                  className={inputCls}
                />
              </SField>
            </div>

            <div className="mb-1 grid grid-cols-[1fr_auto] items-end gap-3">
              <SField label={t("agents.icon")}>
                <div className="flex gap-1.5">
                  {SKILL_ICON_KEYS.map((k) => {
                    const I = SKILL_ICONS[k];
                    const on = form.iconKey === k;
                    const rgb = ACCENTS[form.accent].rgb;
                    return (
                      <button
                        key={k}
                        onClick={() => setForm({ ...form, iconKey: k })}
                        className={`grid h-9 w-9 place-items-center rounded-lg border transition-all ${on ? "" : "border-white/[0.08] text-slate-500 hover:bg-white/[0.05]"}`}
                        style={
                          on
                            ? {
                                borderColor: `rgba(${rgb},0.5)`,
                                background: `rgba(${rgb},0.12)`,
                                color: ACCENTS[form.accent].hex,
                                boxShadow: `0 0 14px -4px rgba(${rgb},0.6)`,
                              }
                            : undefined
                        }
                      >
                        <I size={15} />
                      </button>
                    );
                  })}
                </div>
              </SField>
              <SField label={t("agents.accent")}>
                <div className="flex gap-1.5 pb-0.5">
                  {(Object.keys(ACCENTS) as AccentKey[]).map((k) => (
                    <button
                      key={k}
                      onClick={() => setForm({ ...form, accent: k })}
                      className={`h-6 w-6 rounded-full border-2 transition-transform ${form.accent === k ? "scale-110 border-white/70" : "border-transparent hover:scale-105"}`}
                      style={{
                        background: ACCENTS[k].hex,
                        boxShadow: form.accent === k ? `0 0 12px ${ACCENTS[k].hex}` : undefined,
                      }}
                      aria-label={k}
                    />
                  ))}
                </div>
              </SField>
            </div>

            <SField label={t("agents.sysPrompt")}>
              <textarea
                value={form.systemPrompt}
                onChange={(e) => setForm({ ...form, systemPrompt: e.target.value })}
                rows={4}
                placeholder={t("agents.sysPromptPh")}
                className={`${inputCls} resize-none font-mono text-[11.5px] leading-relaxed`}
              />
            </SField>

            <div className="grid grid-cols-2 gap-3">
              <SField label={t("agents.mainModels")}>
                <div ref={mainModelsRef} className="relative">
                  <button
                    type="button"
                    onClick={() => setMainModelsOpen((open) => !open)}
                    className={`${inputCls} flex items-center justify-between gap-2 text-left`}
                    aria-expanded={mainModelsOpen}
                  >
                    <span className="min-w-0 truncate font-mono text-[11px]">
                      {(form.mainModels ?? []).length ? (form.mainModels ?? []).join(", ") : t("agents.disabled")}
                    </span>
                    <ChevronDown
                      size={14}
                      className={`shrink-0 transition-transform ${mainModelsOpen ? "rotate-180" : ""}`}
                    />
                  </button>
                  {mainModelsOpen && (
                    <>
                      <div className="absolute z-30 mt-1 max-h-56 w-full overflow-y-auto rounded-lg border border-white/[0.1] bg-abyss-800 p-1 shadow-2xl shadow-black/50">
                        {modelOptions.map((model) => {
                          const selected = (form.mainModels ?? []).includes(model);
                          return (
                            <button
                              type="button"
                              key={model}
                              onClick={() =>
                                setForm({
                                  ...form,
                                  mainModels: selected
                                    ? (form.mainModels ?? []).filter((value) => value !== model)
                                    : [...(form.mainModels ?? []), model],
                                })
                              }
                              className={`flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-[11px] transition-colors ${selected ? "bg-indigo-400/[0.1] text-indigo-100" : "text-slate-500 hover:bg-white/[0.04]"}`}
                            >
                              <span
                                className={`grid h-4 w-4 shrink-0 place-items-center rounded border ${selected ? "border-indigo-300 bg-indigo-400/90 text-abyss-950" : "border-slate-600"}`}
                              >
                                {selected && <Check size={11} strokeWidth={3} />}
                              </span>
                              <span className="min-w-0 flex-1 truncate font-mono">{model}</span>
                            </button>
                          );
                        })}
                        {modelOptions.length === 0 && (
                          <p className="px-2 py-1 text-[11px] text-slate-600">{t("providers.noModels")}</p>
                        )}
                      </div>
                    </>
                  )}
                </div>
              </SField>
              <Select
                label={t("agents.compactionModel")}
                value={form.compactionModel}
                onChange={(v) => setForm({ ...form, compactionModel: v })}
              />
              <SField label={t("agents.compactionLevel")}>
                <SSelect
                  value={form.compactionLevel}
                  onChange={(value) => setForm({ ...form, compactionLevel: value as CompactionLevel })}
                  options={[
                    { value: "brief", label: t("agents.compactionBrief") },
                    { value: "balanced", label: t("agents.compactionBalanced") },
                    { value: "detailed", label: t("agents.compactionDetailed") },
                    { value: "comprehensive", label: t("agents.compactionComprehensive") },
                    { value: "epic", label: t("agents.compactionEpic") },
                  ]}
                />
              </SField>
              <Select
                label={t("agents.memoryModel")}
                value={form.memoryModel ?? ""}
                onChange={(v) => setForm({ ...form, memoryModel: v })}
              />
            </div>

            <div className="mt-5">
              <div className="mb-3">
                <div className="mb-1.5 flex items-center justify-between">
                  <span className="text-[11px] font-medium text-slate-400">
                    {t("agents.availGroups", { n: (form.skillGroupIds ?? []).length })}
                  </span>
                  <div className="flex gap-1">
                    <button
                      type="button"
                      onClick={() => setForm({ ...form, skillGroupIds: groups.map((group) => group.id) })}
                      className="grid h-6 w-6 place-items-center rounded-md border border-white/10 text-slate-500 hover:bg-white/[0.06] hover:text-emerald-300"
                      title={t("agents.enableAllGroups")}
                      aria-label={t("agents.enableAllGroups")}
                    >
                      <CheckCheck size={13} />
                    </button>
                    <button
                      type="button"
                      onClick={() => setForm({ ...form, skillGroupIds: [] })}
                      className="grid h-6 w-6 place-items-center rounded-md border border-white/10 text-slate-500 hover:bg-white/[0.06] hover:text-rose-300"
                      title={t("agents.disableAllGroups")}
                      aria-label={t("agents.disableAllGroups")}
                    >
                      <X size={13} />
                    </button>
                  </div>
                </div>
                <div className="max-h-36 space-y-1 overflow-y-auto scroll-slim rounded-lg border border-white/[0.07] bg-abyss-900/50 p-2">
                  {groups.map((group) => {
                    const on = (form.skillGroupIds ?? []).includes(group.id);
                    return (
                      <button
                        key={group.id}
                        onClick={() =>
                          setForm({
                            ...form,
                            skillGroupIds: on
                              ? (form.skillGroupIds ?? []).filter((x) => x !== group.id)
                              : [...(form.skillGroupIds ?? []), group.id],
                          })
                        }
                        className={`flex w-full items-center gap-2.5 rounded-md px-2 py-1.5 text-left transition-colors ${on ? "bg-violet-400/[0.1]" : "hover:bg-white/[0.04]"}`}
                      >
                        <span
                          className={`grid h-4 w-4 shrink-0 place-items-center rounded border ${on ? "border-violet-300 bg-violet-400/90 text-abyss-950" : "border-slate-600"}`}
                        >
                          {on && <Check size={11} strokeWidth={3} />}
                        </span>
                        <Puzzle size={13} className="shrink-0 text-violet-300" />
                        <span className="min-w-0 flex-1 truncate text-[12px]">
                          <span className={on ? "text-slate-200" : "text-slate-400"}>{group.name}</span>
                          {group.description && <span className="text-slate-600"> · {group.description}</span>}
                        </span>
                      </button>
                    );
                  })}
                  {groups.length === 0 && (
                    <p className="px-2 py-1 text-[11px] text-slate-600">{t("agents.noGroups")}</p>
                  )}
                </div>
              </div>

              <div className="mb-3">
                <div className="mb-1.5 flex items-center justify-between">
                  <span className="text-[11px] font-medium text-slate-400">
                    {t("agents.availMcp", { n: (form.mcpIds ?? []).length })}
                  </span>
                  <div className="flex gap-1">
                    <button
                      type="button"
                      onClick={() => setForm({ ...form, mcpIds: mcpServers.map((server) => server.id) })}
                      className="grid h-6 w-6 place-items-center rounded-md border border-white/10 text-slate-500 hover:bg-white/[0.06] hover:text-emerald-300"
                      title={t("agents.enableAllMcp")}
                      aria-label={t("agents.enableAllMcp")}
                    >
                      <CheckCheck size={13} />
                    </button>
                    <button
                      type="button"
                      onClick={() => setForm({ ...form, mcpIds: [] })}
                      className="grid h-6 w-6 place-items-center rounded-md border border-white/10 text-slate-500 hover:bg-white/[0.06] hover:text-rose-300"
                      title={t("agents.disableAllMcp")}
                      aria-label={t("agents.disableAllMcp")}
                    >
                      <X size={13} />
                    </button>
                  </div>
                </div>
                <div className="max-h-36 space-y-1 overflow-y-auto scroll-slim rounded-lg border border-white/[0.07] bg-abyss-900/50 p-2">
                  {mcpServers.map((s) => {
                    const on = (form.mcpIds ?? []).includes(s.id);
                    return (
                      <button
                        key={s.id}
                        onClick={() =>
                          setForm({
                            ...form,
                            mcpIds: on ? (form.mcpIds ?? []).filter((x) => x !== s.id) : [...(form.mcpIds ?? []), s.id],
                          })
                        }
                        className={`flex w-full items-center gap-2.5 rounded-md px-2 py-1.5 transition-colors ${on ? "bg-emerald-400/[0.09]" : "hover:bg-white/[0.04]"}`}
                      >
                        <span
                          className={`grid h-4 w-4 shrink-0 place-items-center rounded border ${on ? "border-emerald-300 bg-emerald-400/90 text-abyss-950" : "border-slate-600"}`}
                        >
                          {on && <Check size={11} strokeWidth={3} />}
                        </span>
                        <Network size={13} className="text-emerald-300" />
                        <span className={`truncate text-[12px] ${on ? "text-slate-200" : "text-slate-400"}`}>
                          {s.name}
                        </span>
                        <span className="ml-auto font-mono text-[9px] text-slate-600">{s.prefix}.*</span>
                      </button>
                    );
                  })}
                  {mcpServers.length === 0 && (
                    <p className="px-2 py-1 text-[11px] text-slate-600">{t("agents.noServers")}</p>
                  )}
                </div>
              </div>
            </div>
          </SModal>
        )}
      </AnimatePresence>
    </div>
  );
}
