import type { ReactNode } from "react";
import { useEffect, useMemo, useRef, useState } from "react";
import { DndContext, PointerSensor, closestCenter, useSensor, useSensors } from "@dnd-kit/core";
import type { DragEndEvent } from "@dnd-kit/core";
import { SortableContext, arrayMove, useSortable, verticalListSortingStrategy } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { AlertCircle, Boxes, ChevronDown, ChevronUp, Copy, Download, GripVertical, Loader2, Pencil, Plus, Search, Trash2, Upload } from "lucide-react";
import type { Workflow, WorkflowStep, WorkflowSubstep } from "../../lib/api";
import { deleteWorkflow, exportWorkflow, getWorkflow, importWorkflow, listWorkflows, saveWorkflow } from "../../lib/api";
import type { AgentEntry, McpServer, Skill, SkillGroup } from "../../lib/data";
import { useT } from "../../lib/i18n";
import { SBtn, SField, SModal, ScreenHeader, SToggle, inputCls } from "./SkillsProviders";
import type { Notify } from "./SkillsProviders";

const draftKey = (profileID: string, id: string) => `arwos.workflow.draft.${profileID}.${id || "new"}`;
const newID = () => crypto.randomUUID();
const emptyStep = (): WorkflowStep => ({ id: newID(), title: "", description: "", acceptance: "", agent_id: "", retry: { maxAttempts: 1 } });
const emptyWorkflow = (): Workflow => ({ id: newID(), name: "", description: "", version: 0, enabled: true, steps: [emptyStep()] });
const normalizeWorkflow = (workflow: Workflow): Workflow => ({
  ...workflow,
  steps: workflow.steps.map((step) => ({ ...step, acceptance: step.acceptance ?? "", agent_id: step.agent_id ?? "", retry: step.retry ?? { maxAttempts: 1 } })),
});
function validate(workflow: Workflow) {
  const errors: string[] = [];
  if (!workflow.name.trim()) errors.push("workflow.name");
  if (!workflow.steps.length) errors.push("workflow.steps");
  const ids = new Set<string>();
  workflow.steps.forEach((step, index) => {
    if (!step.id.trim() || ids.has(step.id)) errors.push(`step.${index}.id`);
    ids.add(step.id);
    if (!step.title.trim()) errors.push(`step.${index}.title`);
    if (!step.agent_id) errors.push(`step.${index}.agent`);
    if (!step.acceptance.trim()) errors.push(`step.${index}.acceptance`);
    if (step.retry.maxAttempts < 1 || step.retry.maxAttempts > 10) errors.push(`step.${index}.retry`);
    const previous = new Set(workflow.steps.slice(0, index).map((item) => item.id));
    if ((step.dependencies ?? []).some((id) => !previous.has(id))) errors.push(`step.${index}.dependencies`);
  });
  return errors;
}

export function WorkflowsScreen({
  notify, onChange, profileId, agents, skills, skillGroups, mcpServers,
}: {
  notify: Notify;
  onChange: (items: Workflow[]) => void;
  profileId: string;
  agents: AgentEntry[];
  skills: Skill[];
  skillGroups: SkillGroup[];
  mcpServers: McpServer[];
}) {
  const { t } = useT();
  const [items, setItems] = useState<Workflow[]>([]);
  const [editing, setEditing] = useState<Workflow | null>(null);
  const [busy, setBusy] = useState(false);
  const [yaml, setYaml] = useState<{ id: string; content: string } | null>(null);
  const fileRef = useRef<HTMLInputElement>(null);
  const refresh = async () => {
    setBusy(true);
    try {
      const result = await listWorkflows();
      setItems(result ?? []);
      onChange(result ?? []);
    } catch (error) {
      notify("err", error instanceof Error ? error.message : String(error));
    } finally {
      setBusy(false);
    }
  };
  useEffect(() => { void refresh(); }, []);
  const persist = async (workflow: Workflow) => {
    const saved = await saveWorkflow(workflow);
    setItems((current) => [...current.filter((item) => item.id !== saved.id), saved]);
    onChange(await listWorkflows());
    return saved;
  };
  const remove = async (workflow: Workflow) => {
    if (!window.confirm(t("workflow.deleteConfirm", { name: workflow.name }))) return;
    try {
      await deleteWorkflow(workflow.id);
      await refresh();
      notify("ok", t("workflow.deleted"));
    } catch (error) {
      notify("err", error instanceof Error ? error.message : String(error));
    }
  };
  const importYaml = async (file: File) => {
    try {
      const result = await importWorkflow(await file.text());
		setEditing(normalizeWorkflow(result));
      notify("ok", t("workflow.imported"));
    } catch (error) {
      notify("err", t("workflow.importError", { error: error instanceof Error ? error.message : String(error) }));
    }
  };
  return (
    <div className="mx-auto max-w-6xl">
      <ScreenHeader
        title={t("workflow.title")}
        count={items.length}
        actionLabel={t("workflow.new")}
        onAction={() => setEditing(emptyWorkflow())}
        secondaryLabel={t("workflow.refresh")}
        secondaryBusy={busy}
        onSecondary={() => void refresh()}
        extraAction={<button onClick={() => fileRef.current?.click()} className="flex items-center gap-1.5 rounded-lg border border-white/10 bg-white/[0.04] px-2.5 py-2 text-[11px] text-slate-300 hover:bg-white/[0.08]"><Upload size={12} />{t("workflow.import")}</button>}
      />
      <input ref={fileRef} type="file" accept=".yaml,.yml,text/yaml" className="hidden" onChange={(event) => { const file = event.target.files?.[0]; event.target.value = ""; if (file) void importYaml(file); }} />
      <div className="space-y-2">
        {items.length === 0 && <div className="glass rounded-xl p-8 text-center text-sm text-slate-500">{t("workflow.empty")}</div>}
        {items.map((workflow) => (
          <div key={workflow.id} className="group relative overflow-hidden rounded-xl border border-white/[0.08] bg-white/[0.015] p-3.5 transition-colors hover:border-white/[0.14] hover:bg-white/[0.025]">
            <div className="absolute inset-y-0 left-0 w-px bg-gradient-to-b from-transparent via-cyan-400/50 to-transparent opacity-0 transition-opacity group-hover:opacity-100" />
            <div className="flex min-w-0 items-center gap-3">
              <span className="grid h-9 w-9 shrink-0 place-items-center rounded-lg border border-cyan-400/25 bg-cyan-400/[0.08] text-cyan-300"><Boxes size={15} /></span>
              <div className="min-w-0 flex-1"><p className="truncate font-display text-[13px] font-semibold text-slate-100">{workflow.name}</p><p className="mt-1 truncate font-mono text-[10px] text-slate-500">v{workflow.version} · {workflow.steps.length} {t("workflow.steps")}</p><p className="mt-1 line-clamp-2 text-[11px] text-slate-400">{workflow.description}</p></div>
              <SToggle on={workflow.enabled} onChange={(enabled) => void persist({ ...workflow, enabled }).catch((error) => notify("err", String(error)))} />
              <div className="flex shrink-0 gap-1">
				<IconButton label={t("common.edit")} onClick={() => void getWorkflow(workflow.id).then((result) => setEditing(normalizeWorkflow(result))).catch((error) => notify("err", String(error)))}><Pencil size={13} /></IconButton>
                <IconButton label={t("workflow.duplicate")} onClick={() => setEditing({ ...structuredClone(workflow), id: newID(), name: `${workflow.name} ${t("workflow.copySuffix")}`, version: 0 })}><Copy size={13} /></IconButton>
                <IconButton label={t("workflow.export")} onClick={() => void exportWorkflow(workflow.id).then((result) => setYaml({ id: workflow.id, content: result.content })).catch((error) => notify("err", String(error)))}><Download size={13} /></IconButton>
                <IconButton label={t("common.delete")} danger onClick={() => void remove(workflow)}><Trash2 size={13} /></IconButton>
              </div>
            </div>
          </div>
        ))}
      </div>
      {editing && <WorkflowEditor workflow={editing} profileId={profileId} agents={agents} skills={skills} skillGroups={skillGroups} mcpServers={mcpServers} onClose={() => setEditing(null)} onSave={persist} notify={notify} />}
      {yaml && <YamlModal data={yaml} onClose={() => setYaml(null)} notify={notify} />}
    </div>
  );
}

function WorkflowEditor({ workflow, profileId, agents, skills, skillGroups, mcpServers, onClose, onSave, notify }: {
  workflow: Workflow; profileId: string; agents: AgentEntry[]; skills: Skill[]; skillGroups: SkillGroup[]; mcpServers: McpServer[]; onClose: () => void; onSave: (value: Workflow) => Promise<Workflow>; notify: Notify;
}) {
  const { t } = useT();
  const [storageKey, setStorageKey] = useState(() => draftKey(profileId, workflow.id));
  const savedSnapshot = useRef("");
  const [value, setValue] = useState<Workflow>(() => {
	try { const stored = localStorage.getItem(storageKey); return stored ? normalizeWorkflow(JSON.parse(stored) as Workflow) : normalizeWorkflow(structuredClone(workflow)); }
	catch { return normalizeWorkflow(structuredClone(workflow)); }
  });
  const [selected, setSelected] = useState(0);
  const [status, setStatus] = useState<"idle" | "saving" | "saved" | "error">("idle");
  const [attempted, setAttempted] = useState(false);
  const errors = validate(value);
  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 6 } }));
  useEffect(() => {
    const snapshot = JSON.stringify(value);
    if (snapshot === savedSnapshot.current) localStorage.removeItem(storageKey);
    else localStorage.setItem(storageKey, snapshot);
  }, [storageKey, value]);
  const persist = async (forced = false) => {
    if (errors.length) { if (forced) setAttempted(true); setStatus("error"); return; }
    setStatus("saving");
    try {
      const saved = await onSave(value);
      savedSnapshot.current = JSON.stringify(saved);
      if (storageKey !== draftKey(profileId, saved.id)) {
        localStorage.removeItem(storageKey);
        setStorageKey(draftKey(profileId, saved.id));
      }
      setValue(saved);
      setStatus("saved");
    } catch (error) {
      setStatus("error");
      notify("err", error instanceof Error ? error.message : String(error));
    }
  };
  useEffect(() => {
    if (errors.length || JSON.stringify(value) === savedSnapshot.current) return;
    const timer = window.setTimeout(() => void persist(), 700);
    return () => window.clearTimeout(timer);
  }, [value]);
  const update = (next: Workflow) => { setStatus("idle"); setValue(next); };
  const updateStep = (index: number, patch: Partial<WorkflowStep>) => update({ ...value, steps: value.steps.map((step, position) => position === index ? { ...step, ...patch } : step) });
  const addStep = () => { update({ ...value, steps: [...value.steps, emptyStep()] }); setSelected(value.steps.length); };
  const removeStep = (index: number) => { if (value.steps.length === 1) return; update({ ...value, steps: value.steps.filter((_, position) => position !== index) }); setSelected(Math.max(0, Math.min(selected, value.steps.length - 2))); };
  const validOrder = (steps: WorkflowStep[]) => steps.every((step, index) => (step.dependencies ?? []).every((id) => steps.slice(0, index).some((item) => item.id === id)));
  const reorder = (from: number, to: number) => {
    if (to < 0 || to >= value.steps.length) return;
    const steps = arrayMove(value.steps, from, to);
    if (!validOrder(steps)) { notify("err", t("workflow.dependencyOrder")); return; }
    update({ ...value, steps });
    setSelected(to);
  };
  const onDragEnd = ({ active, over }: DragEndEvent) => {
    if (!over || active.id === over.id) return;
    reorder(value.steps.findIndex((step) => step.id === active.id), value.steps.findIndex((step) => step.id === over.id));
  };
  const current = value.steps[selected] ?? value.steps[0];
  return (
    <SModal
      title={workflow.id ? t("workflow.edit") : t("workflow.new")}
      onClose={onClose}
      w="max-w-7xl"
      layer={100}
      sticky
      footer={<><div className="mr-auto flex items-center gap-2 text-[11px] text-slate-500">{status === "saving" && <><Loader2 size={13} className="animate-spin" />{t("workflow.saving")}</>}{status === "saved" && t("workflow.savedState")}{status === "error" && <><AlertCircle size={13} className="text-rose-300" />{attempted && errors.length ? t("workflow.validationError") : t("workflow.unsaved")}</>}</div><label className="flex items-center gap-2 text-[11px] text-slate-400">{t("workflow.enabled")}<SToggle on={value.enabled} onChange={(enabled) => update({ ...value, enabled })} /></label><SBtn onClick={onClose}>{t("common.close")}</SBtn><SBtn primary disabled={status === "saving"} onClick={() => void persist(true)}>{t("common.save")}</SBtn></>}
    >
      <><div className="grid gap-3 border-b border-white/[0.08] pb-4 sm:grid-cols-[1fr_1.4fr]">
        <Field invalid={attempted && errors.includes("workflow.name")} label={t("common.name")}><input value={value.name} onChange={(event) => update({ ...value, name: event.target.value })} className={inputCls} /></Field>
        <Field label={t("common.description")}><input value={value.description} onChange={(event) => update({ ...value, description: event.target.value })} className={inputCls} /></Field>
      </div>
      <div className="mt-4 grid min-h-[460px] gap-4 lg:grid-cols-[250px_minmax(0,1fr)]">
        <aside className="rounded-xl border border-white/[0.08] bg-abyss-900/40 p-2">
          <div className="mb-2 flex items-center justify-between px-1"><span className="font-mono text-[10px] tracking-[0.18em] text-slate-500">{t("workflow.steps")}</span><button type="button" onClick={addStep} className="grid h-6 w-6 place-items-center rounded-md text-cyan-300 hover:bg-cyan-400/10" title={t("workflow.addStep")}><Plus size={14} /></button></div>
          <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={onDragEnd}><SortableContext items={value.steps.map((step) => step.id)} strategy={verticalListSortingStrategy}>{value.steps.map((step, index) => <StepItem key={step.id} step={step} index={index} selected={index === selected} onSelect={() => setSelected(index)} onRemove={() => removeStep(index)} onMove={(direction) => reorder(index, index + direction)} />)}</SortableContext></DndContext>
        </aside>
        {current && <StepDetails step={current} index={selected} allSteps={value.steps} agents={agents} skills={skills} groups={skillGroups} servers={mcpServers} errors={errors} attempted={attempted} onChange={(patch) => updateStep(selected, patch)} />}
      </div>
      </>
    </SModal>
  );
}

function StepItem({ step, index, selected, onSelect, onRemove, onMove }: { step: WorkflowStep; index: number; selected: boolean; onSelect: () => void; onRemove: () => void; onMove: (direction: -1 | 1) => void }) {
  const { t } = useT();
  const sortable = useSortable({ id: step.id });
  return <div ref={sortable.setNodeRef} style={{ transform: CSS.Transform.toString(sortable.transform), transition: sortable.transition }} className={`mb-1 flex items-center gap-1 rounded-lg ${selected ? "bg-indigo-400/[0.12]" : "hover:bg-white/[0.04]"}`}><button type="button" {...sortable.attributes} {...sortable.listeners} className="grid h-8 w-5 cursor-grab place-items-center text-slate-600"><GripVertical size={13} /></button><button type="button" onClick={onSelect} className="min-w-0 flex-1 py-2 text-left"><span className="block truncate text-[11px] text-slate-200">{step.title || `${t("workflow.step")} ${index + 1}`}</span></button><div className="flex"><button type="button" disabled={index === 0} onClick={() => onMove(-1)} className="px-1 text-slate-500 disabled:opacity-20"><ChevronUp size={12} /></button><button type="button" onClick={() => onMove(1)} className="px-1 text-slate-500"><ChevronDown size={12} /></button><button type="button" onClick={onRemove} className="px-1 text-slate-500 hover:text-rose-300"><Trash2 size={12} /></button></div></div>;
}

function StepDetails({ step, index, allSteps, agents, skills, groups, servers, errors, attempted, onChange }: { step: WorkflowStep; index: number; allSteps: WorkflowStep[]; agents: AgentEntry[]; skills: Skill[]; groups: SkillGroup[]; servers: McpServer[]; errors: string[]; attempted: boolean; onChange: (patch: Partial<WorkflowStep>) => void }) {
  const { t } = useT();
  const invalid = (field: string) => attempted && errors.includes(`step.${index}.${field}`);
  const agent = agents.find((item) => item.id === step.agent_id);
  const scopedGroups = agent ? groups.filter((group) => (agent.skillGroupIds ?? []).includes(group.id)) : groups;
  const scopedServers = (agent ? servers.filter((server) => agent.mcpIds.includes(server.id)) : servers).filter((server) => server.enabled);
  const skillCatalog = useMemo(() => [{ title: "", values: scopedGroups.map((group) => ({ id: group.id, label: group.name })) }], [scopedGroups]);
  const toolCatalog = useMemo(() => scopedServers.map((server) => ({ title: server.name, values: server.tools.filter((tool) => tool.enabled).map((tool) => ({ id: `${server.prefix}.${tool.alias}`, label: `${server.prefix}.${tool.alias}` })) })), [scopedServers]);
  return <section className="min-w-0 rounded-xl border border-white/[0.08] bg-white/[0.015] p-4">
    <div className="mb-4 flex items-center gap-2"><span className="grid h-7 w-7 place-items-center rounded-lg bg-indigo-400/[0.12] font-mono text-[10px] text-indigo-200">{index + 1}</span><h3 className="font-display text-sm text-slate-100">{t("workflow.step")} {index + 1}</h3></div>
    <div className="grid gap-3 sm:grid-cols-2">
      <Field invalid={invalid("title")} label={t("workflow.stepTitle")}><input value={step.title} onChange={(event) => onChange({ title: event.target.value })} className={inputCls} /></Field>
      <Field label={t("common.description")}><textarea value={step.description} onChange={(event) => onChange({ description: event.target.value })} rows={2} className={inputCls} /></Field>
    </div>
    <div className="grid gap-3 sm:grid-cols-2">
      <Field invalid={invalid("agent")} label={t("workflow.agentId")}><select value={step.agent_id} onChange={(event) => onChange({ agent_id: event.target.value })} className={inputCls}><option value="">{t("workflow.selectAgent")}</option>{agents.map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}</select></Field>
      <Field invalid={invalid("retry")} label={t("workflow.retry")}><input type="number" min={1} max={10} value={step.retry.maxAttempts} onChange={(event) => onChange({ retry: { maxAttempts: Math.max(1, Math.min(10, Number(event.target.value) || 1)) } })} className={inputCls} /></Field>
    </div>
    <Section title={t("workflow.substeps")}><Collection title={t("workflow.substeps")} items={step.substeps ?? []} onChange={(substeps) => onChange({ substeps })} create={() => ({ id: newID(), title: "", description: "" })} render={(item, position) => <><input value={item.title} onChange={(event) => updateSubstep(step, position, { title: event.target.value }, onChange)} className={`${inputCls} flex-1`} /><input value={item.description ?? ""} onChange={(event) => updateSubstep(step, position, { description: event.target.value }, onChange)} className={`${inputCls} flex-1`} /></>} /></Section>
    <Section title={t("workflow.dependencies")}><div className="space-y-1">{allSteps.slice(0, index).map((candidate) => <CheckRow key={candidate.id} label={candidate.title || candidate.id} checked={(step.dependencies ?? []).includes(candidate.id)} onChange={(checked) => onChange({ dependencies: checked ? [...(step.dependencies ?? []), candidate.id] : (step.dependencies ?? []).filter((id) => id !== candidate.id) })} />)}{index === 0 && <p className="text-[11px] text-slate-600">{t("workflow.noDependencies")}</p>}</div></Section>
    <Field invalid={invalid("acceptance")} label={t("workflow.acceptance")}><textarea value={step.acceptance} onChange={(event) => onChange({ acceptance: event.target.value })} rows={3} className={inputCls} /></Field>
    <Section title={`${t("workflow.tools")} · ${t("workflow.skillGroups")}`}><p className="mb-3 text-[10px] text-slate-500">{agent ? agent.name : t("workflow.resourcesAtRun")}</p><div className="grid gap-4 md:grid-cols-2"><ResourceCatalog title={t("workflow.tools")} groups={toolCatalog} selected={step.required_tools ?? []} onChange={(required_tools) => onChange({ required_tools })} /><ResourceCatalog title={t("workflow.skillGroups")} groups={skillCatalog} selected={step.required_skills ?? []} onChange={(required_skills) => onChange({ required_skills })} /></div></Section>
  </section>;
}

function Field({ label, invalid, children }: { label: string; invalid?: boolean; children: ReactNode }) { return <div className={invalid ? "rounded-lg ring-1 ring-rose-400/70" : ""}><SField label={label}>{children}</SField></div>; }
function Section({ title, children }: { title: string; children: ReactNode }) { const [open, setOpen] = useState(false); return <div className="mt-3 rounded-lg border border-white/[0.07] bg-abyss-900/30"><button type="button" onClick={() => setOpen((value) => !value)} className="flex w-full items-center justify-between px-3 py-2.5 text-left text-[11px] font-medium text-slate-300">{title}{open ? <ChevronUp size={14} /> : <ChevronDown size={14} />}</button>{open && <div className="border-t border-white/[0.06] p-3">{children}</div>}</div>; }
function CheckRow({ label, checked, onChange }: { label: string; checked: boolean; onChange: (checked: boolean) => void }) { return <button type="button" onClick={() => onChange(!checked)} className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left hover:bg-white/[0.04]"><span className={`grid h-4 w-4 place-items-center rounded border text-[10px] ${checked ? "border-indigo-300 bg-indigo-400 text-abyss-950" : "border-slate-600"}`}>{checked && "✓"}</span><span className="truncate text-[11px] text-slate-300">{label}</span></button>; }
function ResourceCatalog({ title, groups, selected, onChange }: { title: string; groups: { title: string; values: { id: string; label: string }[] }[]; selected: string[]; onChange: (value: string[]) => void }) { const [query, setQuery] = useState(""); const allValues = groups.flatMap((group) => group.values); const labelFor = (id: string) => allValues.find((value) => value.id === id)?.label ?? id; const matches = (value: { id: string; label: string }) => `${value.id} ${value.label}`.toLowerCase().includes(query.toLowerCase()); return <div className="mb-3 last:mb-0"><p className="mb-1.5 text-[11px] font-medium text-slate-400">{title}</p>{selected.length > 0 && <div className="mb-2 flex flex-wrap gap-1">{selected.map((id) => <button key={id} type="button" onClick={() => onChange(selected.filter((item) => item !== id))} className="rounded border border-cyan-400/20 bg-cyan-400/[0.08] px-1.5 py-px font-mono text-[9px] text-cyan-200">{labelFor(id)} ×</button>)}</div>}<div className="relative mb-2"><Search size={12} className="absolute left-2 top-2 text-slate-600" /><input value={query} onChange={(event) => setQuery(event.target.value)} className={`${inputCls} py-1.5 pl-7 text-[11px]`} /></div><div className="max-h-40 overflow-y-auto rounded-lg border border-white/[0.07] bg-abyss-900/50 p-2"><div className="grid grid-cols-[repeat(auto-fit,minmax(140px,1fr))] gap-1.5">{groups.map((group, index) => { const values = group.values.filter(matches); return values.length ? <div key={group.title || index} className="contents">{group.title && <p className="col-span-full mb-1 px-1 font-mono text-[9px] text-slate-600">{group.title}</p>}{values.map((value) => <CheckRow key={value.id} label={value.label} checked={selected.includes(value.id)} onChange={(checked) => onChange(checked ? [...selected, value.id] : selected.filter((item) => item !== value.id))} />)}</div> : null; })}</div></div></div>; }
function updateSubstep(step: WorkflowStep, index: number, patch: Partial<WorkflowSubstep>, onChange: (patch: Partial<WorkflowStep>) => void) { const substeps = [...(step.substeps ?? [])]; substeps[index] = { ...substeps[index], ...patch }; onChange({ substeps }); }
function Collection<T>({ title, items, onChange, render, create }: { title: string; items: T[]; onChange: (items: T[]) => void; render: (item: T, index: number) => ReactNode; create: () => T }) { const { t } = useT(); return <div className="mb-3 last:mb-0"><div className="mb-1.5 flex items-center justify-between"><span className="text-[11px] font-medium text-slate-400">{title}</span><button type="button" onClick={() => onChange([...items, create()])} className="text-[10px] text-cyan-300">+ {t("workflow.add")}</button></div>{items.map((item, index) => <div key={index} className="mb-2 flex flex-wrap gap-2">{render(item, index)}<button type="button" onClick={() => onChange(items.filter((_, position) => position !== index))} className="px-1 text-slate-500 hover:text-rose-300"><Trash2 size={13} /></button></div>)}</div>; }
function IconButton({ label, danger, onClick, children }: { label: string; danger?: boolean; onClick: () => void; children: ReactNode }) { return <button className={`grid h-7 w-7 place-items-center rounded-lg text-slate-500 ${danger ? "hover:text-rose-300" : "hover:text-cyan-300"}`} title={label} aria-label={label} onClick={onClick}>{children}</button>; }
function YamlModal({ data, onClose, notify }: { data: { id: string; content: string }; onClose: () => void; notify: Notify }) { const { t } = useT(); return <SModal title={t("workflow.exportTitle")} onClose={onClose} w="max-w-3xl" footer={<><SBtn onClick={() => void navigator.clipboard.writeText(data.content).then(() => notify("ok", t("workflow.copied")))}><Copy size={13} />{t("workflow.copy")}</SBtn><SBtn primary onClick={() => { const url = URL.createObjectURL(new Blob([data.content], { type: "text/yaml" })); const link = document.createElement("a"); link.href = url; link.download = `${data.id}.yaml`; link.click(); URL.revokeObjectURL(url); }}><Download size={13} />{t("workflow.download")}</SBtn></>}><textarea readOnly value={data.content} className={`${inputCls} h-80 resize-y font-mono text-[11px]`} /></SModal>; }
