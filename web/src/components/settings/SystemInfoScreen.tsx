import { useEffect, useState } from "react";
import { Cpu, Download, MemoryStick, Monitor, Plus, RefreshCw, Trash2 } from "lucide-react";
import {
  subscribeFrames,
  WS_EVENT_OLLAMA_MODEL_PULL,
  WS_EVENT_OLLAMA_MODEL_REMOVE,
  WS_EVENT_OLLAMA_MODELS_LIST,
  WS_EVENT_OLLAMA_MODELS_REFRESH,
  WS_EVENT_LLAMA_MODEL_PULL,
  WS_EVENT_LLAMA_MODEL_REMOVE,
  WS_EVENT_LLAMA_MODELS_LIST,
  WS_EVENT_LLAMA_MODELS_REFRESH,
  wsRequest,
} from "../../lib/api";
import { useT } from "../../lib/i18n";
import { SSelect, SToggle } from "./SkillsProviders";

type SystemInfo = {
  cpuType: string;
  cpuFrequencyMHz: number;
  cpuCores: number;
  memoryType: string;
  memoryGB: number;
  gpuType: string;
  vramGB: number;
  ollamaInstalled: boolean;
  llamaInstalled: boolean;
  disks: { name: string; mountPoint: string; totalGB: number; freeGB: number; tags: string[] | null }[] | null;
};
type LocalLLMSettings = {
  id?: string;
  profileId?: string;
  runtime: "ollama" | "llama";
  enabled: boolean;
  binaryPath: string;
  launchArgs: string[];
  modelsPath: string;
  env: Record<string, string>;
};
type OllamaCatalogModel = { name: string; sizes: string[] };
type OllamaInstalledModel = { name: string; id: string; size: string; modified: string };
type OllamaModels = { catalog: OllamaCatalogModel[]; installed: OllamaInstalledModel[] };
type LlamaCatalogModel = { id: string; downloads: number };
type LlamaInstalledModel = { id: string; size: number };
type LlamaModels = { catalog: LlamaCatalogModel[]; installed: LlamaInstalledModel[] };

export function SystemInfoScreen() {
  const { t } = useT();
  const [info, setInfo] = useState<SystemInfo | null>(null);
  const [installing, setInstalling] = useState(false);
  const [installedPath, setInstalledPath] = useState("");
  const [llamaInstalling, setLlamaInstalling] = useState(false);
  const [llamaInstalled, setLlamaInstalled] = useState(false);
  const [ollamaError, setOllamaError] = useState("");
  const [llamaError, setLlamaError] = useState("");
  const [ollamaStatus, setOllamaStatus] = useState("");
  const [llamaStatus, setLlamaStatus] = useState("");
  const [ollamaPercent, setOllamaPercent] = useState<number | null>(null);
  const [llamaPercent, setLlamaPercent] = useState<number | null>(null);
  const [llmSettings, setLlmSettings] = useState<Record<"ollama" | "llama", LocalLLMSettings>>({
    ollama: { runtime: "ollama", enabled: false, binaryPath: "", launchArgs: [], modelsPath: "", env: {} },
    llama: { runtime: "llama", enabled: false, binaryPath: "", launchArgs: [], modelsPath: "", env: {} },
  });
  const [savingRuntime, setSavingRuntime] = useState<"ollama" | "llama" | "">("");
  const [catalog, setCatalog] = useState<OllamaCatalogModel[]>([]);
  const [installedModels, setInstalledModels] = useState<OllamaInstalledModel[]>([]);
  const [selectedModel, setSelectedModel] = useState("");
  const [modelsBusy, setModelsBusy] = useState(false);
  const [modelsError, setModelsError] = useState("");
  const [ollamaModelsSort, setOllamaModelsSort] = useState("name-asc");
  const [llamaCatalog, setLlamaCatalog] = useState<LlamaCatalogModel[]>([]);
  const [llamaInstalledModels, setLlamaInstalledModels] = useState<LlamaInstalledModel[]>([]);
  const [selectedLlamaModel, setSelectedLlamaModel] = useState("");
  const [llamaModelsBusy, setLlamaModelsBusy] = useState(false);
  const [llamaModelsError, setLlamaModelsError] = useState("");
  const [llamaModelsSort, setLlamaModelsSort] = useState("name-asc");
  const [activeRuntime, setActiveRuntime] = useState<"ollama" | "llama">("ollama");
  const progressText = (status: string) => {
    if (status.startsWith("downloading:")) {
      return `${t("systemInfo.downloading")} ${status.slice("downloading:".length).replace(/:\d+$/, "")}`;
    }
    const key = `systemInfo.progress.${status}`;
    return t(key);
  };
  const diskTagTitle = (tag: string) => t(`systemInfo.diskTags.${tag}`);
  const modelFitsGPU = (size: string) => {
    const match = size.match(/^e?(\d+(?:\.\d+)?)b$/i);
    if (!match || !info?.vramGB) return false;
    const estimatedGB = Number(match[1]) * 0.7;
    return estimatedGB <= info.vramGB * 0.8;
  };
  const llamaModelFitsGPU = (modelID: string) => {
    const match = modelID.match(/(?:^|[-_.])e?(\d+(?:\.\d+)?)b(?:[-_.]|$)/i);
    return match ? modelFitsGPU(`${match[1]}b`) : false;
  };
  const compareModelNames = (left: string, right: string) => {
    const leftName = left.trim().toLowerCase();
    const rightName = right.trim().toLowerCase();
    if (leftName < rightName) return -1;
    if (leftName > rightName) return 1;
    return 0;
  };
  const modelOptions = catalog
    .flatMap((model) =>
      model.sizes
        .filter(modelFitsGPU)
        .map((size) => ({ value: `${model.name}:${size}`, label: `${model.name}:${size}` })),
    )
    .sort((left, right) => compareModelNames(left.label, right.label));
  const llamaModelOptions = llamaCatalog
    .filter((model) => llamaModelFitsGPU(model.id))
    .map((model) => ({ value: model.id, label: model.id }))
    .sort((left, right) => compareModelNames(left.label, right.label));
  const loadModels = () =>
    wsRequest<OllamaModels>(WS_EVENT_OLLAMA_MODELS_LIST)
      .then((result) => {
        setCatalog(result.catalog ?? []);
        setInstalledModels(result.installed ?? []);
      })
      .catch((error: unknown) => setModelsError(error instanceof Error ? error.message : String(error)));
  const loadLlamaModels = () =>
    wsRequest<LlamaModels>(WS_EVENT_LLAMA_MODELS_LIST)
      .then((result) => {
        setLlamaCatalog(result.catalog ?? []);
        setLlamaInstalledModels(result.installed ?? []);
      })
      .catch((error: unknown) => setLlamaModelsError(error instanceof Error ? error.message : String(error)));
  const formatSize = (size: number) =>
    size >= 1024 ** 3 ? `${(size / 1024 ** 3).toFixed(1)} GB` : `${(size / 1024 ** 2).toFixed(1)} MB`;
  const sortOptions = [
    { value: "name-asc", label: t("systemInfo.models.sortNameAsc") },
    { value: "name-desc", label: t("systemInfo.models.sortNameDesc") },
    { value: "size-desc", label: t("systemInfo.models.sortSizeDesc") },
    { value: "size-asc", label: t("systemInfo.models.sortSizeAsc") },
  ];
  const parseOllamaSize = (value: string) => {
    const match = value.trim().match(/^(\d+(?:\.\d+)?)\s*(B|KB|MB|GB|TB)\b/i);
    if (!match) return 0;
    const units: Record<string, number> = {
      B: 1,
      KB: 1024,
      MB: 1024 ** 2,
      GB: 1024 ** 3,
      TB: 1024 ** 4,
    };
    return Number(match[1]) * units[match[2].toUpperCase()];
  };
  const sortModels = <T,>(items: T[], sort: string, name: (item: T) => string, size: (item: T) => number) => {
    const direction = sort.endsWith("asc") ? 1 : -1;
    const compareNames = (left: T, right: T) => {
      const leftName = name(left).trim().toLowerCase();
      const rightName = name(right).trim().toLowerCase();
      if (leftName < rightName) return -1;
      if (leftName > rightName) return 1;
      return 0;
    };
    return [...items].sort((left, right) => {
      if (sort.startsWith("name")) return direction * compareNames(left, right);
      const difference = size(left) - size(right);
      return difference === 0 ? compareNames(left, right) : direction * difference;
    });
  };
  const sortedOllamaModels = sortModels(
    installedModels,
    ollamaModelsSort,
    (model) => model.name,
    (model) => parseOllamaSize(model.size),
  );
  const sortedLlamaModels = sortModels(
    llamaInstalledModels,
    llamaModelsSort,
    (model) => model.id,
    (model) => model.size,
  );
  useEffect(() => {
    void wsRequest<SystemInfo>(117)
      .then((result) => {
        setInfo(result);
        setInstalledPath(result.ollamaInstalled ? "installed" : "");
        setLlamaInstalled(result.llamaInstalled);
      })
      .catch(() => setInfo(null));
  }, []);
  useEffect(() => {
    void loadModels();
    void loadLlamaModels();
  }, []);
  useEffect(() => {
    void wsRequest<LocalLLMSettings[]>(120)
      .then((items) => {
        setLlmSettings((current) => {
          const next = { ...current };
          items.forEach((item) => {
            if (item.runtime === "ollama" || item.runtime === "llama")
              next[item.runtime] = {
                ...next[item.runtime],
                ...item,
                launchArgs: item.launchArgs ?? [],
                env: item.env ?? {},
              };
          });
          return next;
        });
      })
      .catch(() => undefined);
  }, []);
  useEffect(
    () =>
      subscribeFrames((frame) => {
        if (frame.type !== "local_llm.install" || typeof frame.status !== "string") return;
        const match = frame.status.match(/^downloading:.*:(\d+)$/);
        const percent = match ? Number(match[1]) : null;
        if (frame.runtime === "ollama") {
          setOllamaStatus(frame.status);
          setOllamaPercent(percent);
        }
        if (frame.runtime === "llama") {
          setLlamaStatus(frame.status);
          setLlamaPercent(percent);
        }
      }),
    [],
  );
  const cards = info
    ? [
        {
          icon: Cpu,
          title: t("systemInfo.cpu"),
          rows: [
            [t("systemInfo.type"), info.cpuType],
            [t("systemInfo.frequency"), info.cpuFrequencyMHz ? `${info.cpuFrequencyMHz} MHz` : "—"],
            [t("systemInfo.cores"), String(info.cpuCores)],
          ],
        },
        {
          icon: MemoryStick,
          title: t("systemInfo.memory"),
          rows: [
            [t("systemInfo.type"), info.memoryType],
            [t("systemInfo.amount"), info.memoryGB ? `${info.memoryGB.toFixed(1)} GB` : "—"],
          ],
        },
        {
          icon: Monitor,
          title: t("systemInfo.gpu"),
          rows: [
            [t("systemInfo.type"), info.gpuType],
            [t("systemInfo.videoMemory"), info.vramGB ? `${info.vramGB.toFixed(1)} GB` : "—"],
          ],
        },
      ]
    : [];
  return (
    <div className="mx-auto max-w-3xl">
      <div className="mb-4">
        <h2 className="font-display text-lg font-semibold text-slate-100">{t("systemInfo.title")}</h2>
        <p className="mt-1 text-xs text-slate-500">{t("systemInfo.subtitle")}</p>
      </div>
      {!info ? (
        <div className="rounded-xl border border-white/10 p-8 text-center text-xs text-slate-500">
          {t("common.loading")}
        </div>
      ) : (
        <div className="overflow-hidden rounded-xl border border-white/[0.08] bg-white/[0.02]">
          {cards.map(({ icon: Icon, title, rows }) => (
            <div
              key={title}
              className="flex min-w-0 items-start gap-3 border-b border-white/[0.06] p-2.5 last:border-b-0"
            >
              <div className="flex w-6 shrink-0 items-center justify-center pt-0.5" aria-label={title} title={title}>
                <Icon size={14} className="text-cyan-300" />
              </div>
              <div className="grid min-w-0 flex-1 gap-x-5 gap-y-1 sm:grid-cols-[minmax(0,2fr)_minmax(0,1fr)_minmax(0,1fr)]">
                {rows.map(([label, value]) => (
                  <div key={label} className="min-w-0">
                    <div className="flex min-w-0 items-baseline gap-2 whitespace-nowrap" title={`${label}: ${value}`}>
                      <span className="shrink-0 text-[9px] uppercase tracking-wider text-slate-600">{label}</span>
                      <span className="font-mono text-[11px] leading-4 text-slate-300">{value}</span>
                    </div>
                  </div>
                ))}
              </div>
            </div>
          ))}
        </div>
      )}
      {info && (
        <div className="mt-3 rounded-xl border border-white/[0.08] bg-white/[0.02] p-2.5">
          <div className="mb-2 text-xs text-slate-500">{t("systemInfo.disks")}</div>
          <div className="space-y-1">
            {info.disks?.length ? (
              info.disks.map((disk) => (
                <div
                  key={`${disk.name}-${disk.mountPoint}`}
                  className="grid grid-cols-[minmax(0,2fr)_minmax(0,1fr)_minmax(0,1fr)] gap-3 text-[11px]"
                >
                  <div className="flex min-w-0 items-center gap-1.5">
                    <span
                      className="min-w-0 truncate font-mono text-slate-300"
                      title={`${disk.name} · ${disk.mountPoint}`}
                    >
                      {disk.name} · {disk.mountPoint}
                    </span>
                    {(disk.tags ?? []).map((tag) => (
                      <span
                        key={tag}
                        title={diskTagTitle(tag)}
                        aria-label={diskTagTitle(tag)}
                        className="shrink-0 rounded border border-cyan-400/25 bg-cyan-400/10 px-1.5 py-0.5 font-mono text-[9px] text-cyan-200"
                      >
                        {tag}
                      </span>
                    ))}
                  </div>
                  <span className="font-mono text-slate-400">{disk.totalGB.toFixed(1)} GB</span>
                  <span className="font-mono text-emerald-300">{disk.freeGB.toFixed(1)} GB free</span>
                </div>
              ))
            ) : (
              <span className="text-xs text-slate-600">—</span>
            )}
          </div>
        </div>
      )}
      <div className="mt-5">
        <h3 className="mb-2 text-xs text-slate-500">{t("systemInfo.installations")}</h3>
        <div className="mb-3 flex gap-1 rounded-lg border border-white/[0.08] bg-white/[0.02] p-1">
          {(["ollama", "llama"] as const).map((runtime) => (
            <button
              key={runtime}
              type="button"
              onClick={() => setActiveRuntime(runtime)}
              className={`flex-1 rounded-md px-3 py-2 text-xs transition-colors ${
                activeRuntime === runtime
                  ? runtime === "ollama"
                    ? "bg-cyan-400/15 text-cyan-200"
                    : "bg-violet-400/15 text-violet-200"
                  : "text-slate-500 hover:bg-white/[0.04] hover:text-slate-300"
              }`}
            >
              {t(`systemInfo.runtimeTabs.${runtime}`)}
            </button>
          ))}
        </div>
        <div className="space-y-2">
          <div className="flex flex-wrap gap-2">
            {activeRuntime === "ollama" && (
              <button
                disabled={installing}
                onClick={() => {
                  setInstalling(true);
                  setOllamaError("");
                  setOllamaStatus("");
                  void wsRequest<{ path: string }>(118)
                    .then((result) => setInstalledPath(result.path))
                    .catch((error: unknown) => {
                      setInstalledPath("");
                      setOllamaError(error instanceof Error ? error.message : String(error));
                    })
                    .finally(() => setInstalling(false));
                }}
                className="inline-flex items-center gap-2 rounded-lg border border-cyan-400/25 bg-cyan-400/[0.08] px-3 py-2 text-xs text-cyan-200 transition-colors hover:border-cyan-300/50 hover:bg-cyan-400/[0.14]"
              >
                {installedPath ? <RefreshCw size={13} /> : <Download size={13} />}
                {installing
                  ? t("systemInfo.installing")
                  : installedPath
                    ? t("systemInfo.updateOllama")
                    : t("systemInfo.installOllama")}{" "}
              </button>
            )}
            {activeRuntime === "llama" && (
              <button
                disabled={llamaInstalling}
                onClick={() => {
                  setLlamaInstalling(true);
                  setLlamaError("");
                  setLlamaStatus("");
                  void wsRequest<{ path: string }>(119)
                    .then(() => setLlamaInstalled(true))
                    .catch((error: unknown) => {
                      setLlamaInstalled(false);
                      setLlamaError(error instanceof Error ? error.message : String(error));
                    })
                    .finally(() => setLlamaInstalling(false));
                }}
                className="inline-flex items-center gap-2 rounded-lg border border-violet-400/25 bg-violet-400/[0.08] px-3 py-2 text-xs text-violet-200 transition-colors hover:border-violet-300/50 hover:bg-violet-400/[0.14]"
              >
                {llamaInstalled ? <RefreshCw size={13} /> : <Download size={13} />}
                {llamaInstalling
                  ? t("systemInfo.installingLlama")
                  : llamaInstalled
                    ? t("systemInfo.updateLlama")
                    : t("systemInfo.installLlama")}{" "}
              </button>
            )}
          </div>
          <div className="space-y-1 text-xs text-slate-400">
            {activeRuntime === "ollama" && ollamaStatus && <div>{progressText(ollamaStatus)}</div>}
            {activeRuntime === "llama" && llamaStatus && <div>{progressText(llamaStatus)}</div>}
            {activeRuntime === "ollama" && ollamaPercent !== null && (
              <div className="h-1 overflow-hidden rounded bg-white/10">
                <div className="h-full bg-cyan-300" style={{ width: `${ollamaPercent}%` }} />
              </div>
            )}
            {activeRuntime === "llama" && llamaPercent !== null && (
              <div className="h-1 overflow-hidden rounded bg-white/10">
                <div className="h-full bg-violet-300" style={{ width: `${llamaPercent}%` }} />
              </div>
            )}
          </div>
          <div className="space-y-2">
            {activeRuntime === "ollama" && ollamaError && (
              <div className="rounded-lg border border-amber-400/30 bg-amber-400/10 px-3 py-2 text-xs text-amber-200">
                {ollamaError}
              </div>
            )}
            {activeRuntime === "llama" && llamaError && (
              <div className="rounded-lg border border-amber-400/30 bg-amber-400/10 px-3 py-2 text-xs text-amber-200">
                {llamaError}
              </div>
            )}
          </div>
        </div>
      </div>
      <div className="mt-5 grid gap-3">
        {(["ollama", "llama"] as const).map((runtime) => {
          if (runtime !== activeRuntime) return null;
          const settings = llmSettings[runtime];
          return (
            <div key={runtime} className="rounded-xl border border-white/[0.08] bg-white/[0.02] p-3">
              <div className="mb-3 flex items-center justify-between gap-3">
                <h3 className="text-sm font-medium text-slate-200">{t(`systemInfo.configure.${runtime}`)}</h3>
                <SToggle
                  on={settings.enabled}
                  onChange={(enabled) => {
                    const updated = { ...settings, enabled };
                    setLlmSettings((current) => ({ ...current, [runtime]: updated }));
                    setSavingRuntime(runtime);
                    void wsRequest<LocalLLMSettings>(121, updated)
                      .then((saved) =>
                        setLlmSettings((current) => ({ ...current, [runtime]: { ...updated, ...saved } })),
                      )
                      .catch((error: unknown) => {
                        setLlmSettings((current) => ({ ...current, [runtime]: settings }));
                        const message = error instanceof Error ? error.message : String(error);
                        if (runtime === "ollama") setOllamaError(message);
                        else setLlamaError(message);
                      })
                      .finally(() => setSavingRuntime(""));
                  }}
                  small
                />
              </div>
              <div className="space-y-2">
                <input
                  value={settings.binaryPath}
                  onChange={(event) =>
                    setLlmSettings((current) => ({
                      ...current,
                      [runtime]: { ...settings, binaryPath: event.target.value },
                    }))
                  }
                  placeholder={t("systemInfo.binaryPath")}
                  className="w-full rounded-lg border border-white/10 bg-black/10 px-3 py-2 text-xs text-slate-300 outline-none"
                  spellCheck={false}
                  autoComplete="off"
                />
                <input
                  value={settings.launchArgs.join(" ")}
                  onChange={(event) =>
                    setLlmSettings((current) => ({
                      ...current,
                      [runtime]: { ...settings, launchArgs: event.target.value.split(/\s+/).filter(Boolean) },
                    }))
                  }
                  placeholder={t("systemInfo.launchArgs")}
                  className="w-full rounded-lg border border-white/10 bg-black/10 px-3 py-2 text-xs text-slate-300 outline-none"
                  spellCheck={false}
                  autoComplete="off"
                />
                <input
                  value={settings.modelsPath}
                  onChange={(event) =>
                    setLlmSettings((current) => ({
                      ...current,
                      [runtime]: { ...settings, modelsPath: event.target.value },
                    }))
                  }
                  placeholder={t("systemInfo.modelsPath")}
                  className="w-full rounded-lg border border-white/10 bg-black/10 px-3 py-2 text-xs text-slate-300 outline-none"
                  spellCheck={false}
                  autoComplete="off"
                />
                <div className="space-y-1.5">
                  <div className="text-[10px] uppercase tracking-wider text-slate-600">{t("systemInfo.env")}</div>
                  {Object.entries(settings.env).map(([key, value]) => (
                    <div key={key} className="flex gap-1.5">
                      <input
                        value={key}
                        placeholder={t("systemInfo.envKey")}
                        onChange={(event) => {
                          const env = { ...settings.env };
                          delete env[key];
                          if (event.target.value) env[event.target.value] = value;
                          setLlmSettings((current) => ({ ...current, [runtime]: { ...settings, env } }));
                        }}
                        className="min-w-0 flex-1 rounded-lg border border-white/10 bg-black/10 px-2.5 py-2 font-mono text-xs text-slate-300 outline-none"
                        spellCheck={false}
                        autoComplete="off"
                      />
                      <input
                        value={value}
                        placeholder={t("systemInfo.envValue")}
                        onChange={(event) =>
                          setLlmSettings((current) => ({
                            ...current,
                            [runtime]: { ...settings, env: { ...settings.env, [key]: event.target.value } },
                          }))
                        }
                        className="min-w-0 flex-1 rounded-lg border border-white/10 bg-black/10 px-2.5 py-2 font-mono text-xs text-slate-300 outline-none"
                        spellCheck={false}
                        autoComplete="off"
                      />
                      <button
                        type="button"
                        title={t("systemInfo.removeEnv")}
                        onClick={() => {
                          const env = { ...settings.env };
                          delete env[key];
                          setLlmSettings((current) => ({ ...current, [runtime]: { ...settings, env } }));
                        }}
                        className="rounded-lg px-2 text-slate-500 hover:text-rose-300"
                      >
                        <Trash2 size={13} />
                      </button>
                    </div>
                  ))}
                  <button
                    type="button"
                    onClick={() =>
                      setLlmSettings((current) => ({
                        ...current,
                        [runtime]: { ...settings, env: { ...settings.env, [`NEW_${Date.now()}`]: "" } },
                      }))
                    }
                    className="inline-flex items-center gap-1 text-xs text-cyan-300 hover:text-cyan-200"
                  >
                    <Plus size={13} />
                    {t("systemInfo.addEnv")}
                  </button>
                </div>
                <button
                  disabled={savingRuntime === runtime}
                  onClick={() => {
                    setSavingRuntime(runtime);
                    void wsRequest<LocalLLMSettings>(121, settings)
                      .then((saved) =>
                        setLlmSettings((current) => ({ ...current, [runtime]: { ...settings, ...saved } })),
                      )
                      .catch((error: unknown) => {
                        const message = error instanceof Error ? error.message : String(error);
                        if (runtime === "ollama") setOllamaError(message);
                        else setLlamaError(message);
                      })
                      .finally(() => setSavingRuntime(""));
                  }}
                  className="rounded-lg border border-cyan-400/25 bg-cyan-400/[0.08] px-3 py-2 text-xs text-cyan-200 hover:bg-cyan-400/[0.14]"
                >
                  {savingRuntime === runtime ? t("systemInfo.saving") : t("systemInfo.save")}
                </button>
              </div>
            </div>
          );
        })}
      </div>
      {activeRuntime === "ollama" && (
        <div className="mt-5 rounded-xl border border-white/[0.08] bg-white/[0.02] p-3">
          <div className="mb-3 flex items-center justify-between gap-3">
            <h3 className="text-sm font-medium text-slate-200">{t("systemInfo.models.title")}</h3>
            <div className="flex items-center gap-2">
              <div className="w-36">
                <SSelect
                  value={ollamaModelsSort}
                  onChange={setOllamaModelsSort}
                  options={sortOptions}
                  dropUp
                  maxVisible={4}
                />
              </div>
              <button
                type="button"
                title={t("systemInfo.models.refresh")}
                disabled={modelsBusy}
                onClick={() => {
                  setModelsBusy(true);
                  setModelsError("");
                  void wsRequest(WS_EVENT_OLLAMA_MODELS_REFRESH)
                    .then(() => loadModels())
                    .catch((error: unknown) => setModelsError(error instanceof Error ? error.message : String(error)))
                    .finally(() => setModelsBusy(false));
                }}
                className="rounded-lg border border-white/10 p-2 text-slate-400 hover:border-cyan-400/40 hover:text-cyan-200 disabled:opacity-50"
              >
                <RefreshCw size={14} className={modelsBusy ? "animate-spin" : ""} />
              </button>
            </div>
          </div>
          <div className="flex gap-2">
            <div className="min-w-0 flex-1">
              <SSelect
                value={selectedModel}
                onChange={setSelectedModel}
                options={[{ value: "", label: t("systemInfo.models.select") }, ...modelOptions]}
                dropUp
                searchable
                searchPlaceholder={t("systemInfo.models.search")}
                maxVisible={5}
              />
            </div>
            <button
              type="button"
              title={t("systemInfo.models.download")}
              disabled={!selectedModel || modelsBusy}
              onClick={() => {
                setModelsBusy(true);
                setModelsError("");
                void wsRequest<{ installed: OllamaInstalledModel[] }>(WS_EVENT_OLLAMA_MODEL_PULL, {
                  name: selectedModel,
                })
                  .then((result) => setInstalledModels(result.installed ?? []))
                  .catch((error: unknown) => setModelsError(error instanceof Error ? error.message : String(error)))
                  .finally(() => setModelsBusy(false));
              }}
              className="rounded-lg border border-cyan-400/25 bg-cyan-400/[0.08] p-2 text-cyan-200 hover:bg-cyan-400/[0.14] disabled:opacity-50"
            >
              <Download size={14} />
            </button>
          </div>
          {modelsError && (
            <div className="mt-2 rounded-lg border border-amber-400/30 bg-amber-400/10 px-3 py-2 text-xs text-amber-200">
              {modelsError}
            </div>
          )}
          <div className="mt-3 space-y-1">
            {sortedOllamaModels.length ? (
              sortedOllamaModels.map((model) => (
                <div
                  key={model.name}
                  className="flex min-w-0 items-center gap-2 rounded-lg border border-white/[0.06] px-2.5 py-2"
                >
                  <div className="min-w-0 flex-1">
                    <div className="truncate font-mono text-xs text-slate-200" title={model.name}>
                      {model.name}
                    </div>
                    <div className="text-[10px] text-slate-500">
                      {model.size}
                      {model.modified ? ` · ${model.modified}` : ""}
                    </div>
                  </div>
                  <button
                    type="button"
                    title={t("systemInfo.models.remove")}
                    disabled={modelsBusy}
                    onClick={() => {
                      setModelsBusy(true);
                      setModelsError("");
                      void wsRequest<{ installed: OllamaInstalledModel[] }>(WS_EVENT_OLLAMA_MODEL_REMOVE, {
                        name: model.name,
                      })
                        .then((result) => setInstalledModels(result.installed ?? []))
                        .catch((error: unknown) =>
                          setModelsError(error instanceof Error ? error.message : String(error)),
                        )
                        .finally(() => setModelsBusy(false));
                    }}
                    className="rounded-lg p-1.5 text-slate-500 hover:bg-rose-400/10 hover:text-rose-300 disabled:opacity-50"
                  >
                    <Trash2 size={14} />
                  </button>
                </div>
              ))
            ) : (
              <div className="py-2 text-xs text-slate-600">{t("systemInfo.models.empty")}</div>
            )}
          </div>
        </div>
      )}
      {activeRuntime === "llama" && (
        <div className="mt-3 rounded-xl border border-white/[0.08] bg-white/[0.02] p-3">
          <div className="mb-3 flex items-center justify-between gap-3">
            <div>
              <h3 className="text-sm font-medium text-slate-200">{t("systemInfo.llamaModels.title")}</h3>
              <div className="mt-0.5 text-[10px] text-slate-600">{t("systemInfo.llamaModels.quantization")}</div>
            </div>
            <div className="flex items-center gap-2">
              <div className="w-36">
                <SSelect
                  value={llamaModelsSort}
                  onChange={setLlamaModelsSort}
                  options={sortOptions}
                  dropUp
                  maxVisible={4}
                />
              </div>
              <button
                type="button"
                title={t("systemInfo.llamaModels.refresh")}
                disabled={llamaModelsBusy}
                onClick={() => {
                  setLlamaModelsBusy(true);
                  setLlamaModelsError("");
                  void wsRequest(WS_EVENT_LLAMA_MODELS_REFRESH)
                    .then(() => loadLlamaModels())
                    .catch((error: unknown) =>
                      setLlamaModelsError(error instanceof Error ? error.message : String(error)),
                    )
                    .finally(() => setLlamaModelsBusy(false));
                }}
                className="rounded-lg border border-white/10 p-2 text-slate-400 hover:border-violet-400/40 hover:text-violet-200 disabled:opacity-50"
              >
                <RefreshCw size={14} className={llamaModelsBusy ? "animate-spin" : ""} />
              </button>
            </div>
          </div>
          <div className="flex gap-2">
            <div className="min-w-0 flex-1">
              <SSelect
                value={selectedLlamaModel}
                onChange={setSelectedLlamaModel}
                options={[{ value: "", label: t("systemInfo.llamaModels.select") }, ...llamaModelOptions]}
                dropUp
                searchable
                searchPlaceholder={t("systemInfo.llamaModels.search")}
                maxVisible={5}
              />
            </div>
            <button
              type="button"
              title={t("systemInfo.llamaModels.download")}
              disabled={!selectedLlamaModel || llamaModelsBusy}
              onClick={() => {
                setLlamaModelsBusy(true);
                setLlamaModelsError("");
                void wsRequest<{ installed: LlamaInstalledModel[] }>(WS_EVENT_LLAMA_MODEL_PULL, {
                  id: selectedLlamaModel,
                })
                  .then((result) => setLlamaInstalledModels(result.installed ?? []))
                  .catch((error: unknown) =>
                    setLlamaModelsError(error instanceof Error ? error.message : String(error)),
                  )
                  .finally(() => setLlamaModelsBusy(false));
              }}
              className="rounded-lg border border-violet-400/25 bg-violet-400/[0.08] p-2 text-violet-200 hover:bg-violet-400/[0.14] disabled:opacity-50"
            >
              <Download size={14} className={llamaModelsBusy ? "animate-pulse" : ""} />
            </button>
          </div>
          {llamaModelsError && (
            <div className="mt-2 rounded-lg border border-amber-400/30 bg-amber-400/10 px-3 py-2 text-xs text-amber-200">
              {llamaModelsError}
            </div>
          )}
          <div className="mt-3 space-y-1">
            {sortedLlamaModels.length ? (
              sortedLlamaModels.map((model) => (
                <div
                  key={model.id}
                  className="flex min-w-0 items-center gap-2 rounded-lg border border-white/[0.06] px-2.5 py-2"
                >
                  <div className="min-w-0 flex-1">
                    <div className="truncate font-mono text-xs text-slate-200" title={model.id}>
                      {model.id}
                    </div>
                    <div className="text-[10px] text-slate-500">{formatSize(model.size)}</div>
                  </div>
                  <button
                    type="button"
                    title={t("systemInfo.llamaModels.remove")}
                    disabled={llamaModelsBusy}
                    onClick={() => {
                      setLlamaModelsBusy(true);
                      setLlamaModelsError("");
                      void wsRequest<{ installed: LlamaInstalledModel[] }>(WS_EVENT_LLAMA_MODEL_REMOVE, {
                        id: model.id,
                      })
                        .then((result) => setLlamaInstalledModels(result.installed ?? []))
                        .catch((error: unknown) =>
                          setLlamaModelsError(error instanceof Error ? error.message : String(error)),
                        )
                        .finally(() => setLlamaModelsBusy(false));
                    }}
                    className="rounded-lg p-1.5 text-slate-500 hover:bg-rose-400/10 hover:text-rose-300 disabled:opacity-50"
                  >
                    <Trash2 size={14} />
                  </button>
                </div>
              ))
            ) : (
              <div className="py-2 text-xs text-slate-600">{t("systemInfo.llamaModels.empty")}</div>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
