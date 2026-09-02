import { useState } from "react";
import { Globe2, Loader2, Pencil, Server, ShieldCheck, Trash2, UserRound } from "lucide-react";
import type { Proxy } from "../../lib/data";
import { wsRequest } from "../../lib/api";
import { useT } from "../../lib/i18n";
import { SBtn, SField, SModal, SToggle, ScreenHeader, inputCls } from "./SkillsProviders";
import type { Notify } from "./SkillsProviders";

export function NetworkScreen({
  proxies,
  patch,
  notify,
}: {
  proxies: Proxy[];
  patch: (p: { proxies: Proxy[] }) => void;
  notify: Notify;
}) {
  const { t } = useT();
  const [edit, setEdit] = useState<Proxy | null>(null);
  const [open, setOpen] = useState(false);
  const [testing, setTesting] = useState<string | null>(null);
  const refresh = async () => patch({ proxies: await wsRequest<Proxy[]>(94) });
  const test = async (id: string) => {
    setTesting(id);
    try {
      const result = await wsRequest<{ ip: string }>(99, { id });
      notify("ok", t("network.testOk", { ip: result.ip }));
    } catch (error) {
      notify("err", error instanceof Error ? error.message : String(error));
    } finally {
      setTesting(null);
    }
  };
  const typeStyle = {
    http: "border-cyan-400/25 bg-cyan-400/[0.08] text-cyan-300",
    https: "border-emerald-400/25 bg-emerald-400/[0.08] text-emerald-300",
    socks5: "border-violet-400/25 bg-violet-400/[0.08] text-violet-300",
  } as const;
  return (
    <div className="mx-auto max-w-3xl">
      <ScreenHeader
        title={t("network.title")}
        count={proxies.length}
        actionLabel={t("network.add")}
        onAction={() => {
          setEdit(null);
          setOpen(true);
        }}
      />
      <div className="space-y-2">
        {proxies.map((p) => (
          <div
            key={p.id}
            className="group relative overflow-hidden rounded-xl border border-white/[0.08] bg-white/[0.015] p-3.5 transition-colors hover:border-white/[0.14] hover:bg-white/[0.025]"
          >
            <div className="absolute inset-y-0 left-0 w-px bg-gradient-to-b from-transparent via-indigo-400/50 to-transparent opacity-0 transition-opacity group-hover:opacity-100" />
            <div className="flex min-w-0 items-center gap-3">
              <span className={`grid h-9 w-9 shrink-0 place-items-center rounded-lg border ${typeStyle[p.type]}`}>
                <Server size={15} />
              </span>
              <div className="min-w-0 flex-1">
                <div className="flex min-w-0 items-center gap-2">
                  <span className="truncate text-[13px] font-medium text-slate-100">{p.name}</span>
                  <span
                    className={`shrink-0 rounded border px-1.5 py-px font-mono text-[8.5px] uppercase tracking-wider ${typeStyle[p.type]}`}
                  >
                    {p.type}
                  </span>
                  {p.username && <UserRound size={11} className="shrink-0 text-slate-500" />}
                </div>
                <div className="mt-1 flex min-w-0 items-center gap-2 text-[10.5px]">
                  <span className="truncate font-mono text-slate-400">
                    {p.host}:{p.port}
                  </span>
                  {p.description && <span className="truncate text-slate-600">· {p.description}</span>}
                </div>
              </div>
              <div className="flex shrink-0 gap-1">
                <button
                  onClick={() => void test(p.id)}
                  disabled={testing !== null}
                  className="grid h-7 w-7 place-items-center rounded-lg text-slate-500 transition-colors hover:bg-emerald-500/10 hover:text-emerald-300 disabled:opacity-50"
                  title={t("network.test")}
                >
                  {testing === p.id ? <Loader2 size={13} className="animate-spin" /> : <Globe2 size={13} />}
                </button>
                <button
                  onClick={() => {
                    setEdit(p);
                    setOpen(true);
                  }}
                  className="grid h-7 w-7 place-items-center rounded-lg text-slate-500 transition-colors hover:bg-white/[0.07] hover:text-indigo-300"
                  title={t("common.edit")}
                >
                  <Pencil size={13} />
                </button>
                <button
                  onClick={async () => {
                    await wsRequest(97, { id: p.id });
                    await refresh();
                    notify("info", t("network.deleted"));
                  }}
                  className="grid h-7 w-7 place-items-center rounded-lg text-slate-500 transition-colors hover:bg-rose-500/15 hover:text-rose-300"
                  title={t("common.delete")}
                >
                  <Trash2 size={13} />
                </button>
              </div>
            </div>
          </div>
        ))}
        {proxies.length === 0 && (
          <div className="rounded-xl border border-dashed border-white/[0.1] px-4 py-10 text-center">
            <ShieldCheck size={22} className="mx-auto mb-2 text-slate-600" />
            <p className="text-[12px] text-slate-500">{t("network.empty")}</p>
          </div>
        )}
      </div>
      {open && (
        <ProxyModal
          initial={edit}
          notify={notify}
          onClose={() => setOpen(false)}
          onSaved={async () => {
            await refresh();
            setOpen(false);
            notify("ok", t("network.saved"));
          }}
        />
      )}
    </div>
  );
}
function ProxyModal({
  initial,
  notify,
  onClose,
  onSaved,
}: {
  initial: Proxy | null;
  notify: Notify;
  onClose: () => void;
  onSaved: () => Promise<void>;
}) {
  const { t } = useT();
  const [name, setName] = useState(initial?.name ?? "");
  const [description, setDescription] = useState(initial?.description ?? "");
  const [type, setType] = useState<"http" | "https" | "socks5">(initial?.type ?? "http");
  const [host, setHost] = useState(initial?.host ?? "");
  const [port, setPort] = useState(initial?.port ? String(initial.port) : "");
  const [username, setUsername] = useState(initial?.username ?? "");
  const [password, setPassword] = useState("");
  const [insecure, setInsecure] = useState(initial?.insecureSkipVerify ?? false);
  const [resetting, setResetting] = useState(false);
  const save = async () => {
    await wsRequest(96, {
      id: initial?.id,
      name,
      description,
      type,
      host,
      port: Number(port),
      username,
      password,
      insecureSkipVerify: insecure,
    });
    await onSaved();
  };
  const resetPassword = async () => {
    if (!initial) return;
    setResetting(true);
    try {
      await wsRequest(98, { id: initial.id });
      notify("ok", t("network.passwordReset"));
      await onSaved();
    } catch (error) {
      notify("err", error instanceof Error ? error.message : String(error));
    } finally {
      setResetting(false);
    }
  };
  return (
    <SModal
      title={initial ? t("network.edit") : t("network.new")}
      onClose={onClose}
      footer={
        <>
          <SBtn onClick={onClose}>{t("common.cancel")}</SBtn>
          <SBtn primary disabled={!name || !host || !port} onClick={() => void save()}>
            {t("common.save")}
          </SBtn>
        </>
      }
    >
      <SField label={t("common.name")}>
        <input value={name} onChange={(e) => setName(e.target.value)} className={inputCls} />
      </SField>
      <SField label={t("network.description")}>
        <input value={description} onChange={(e) => setDescription(e.target.value)} className={inputCls} />
      </SField>
      <div className="grid grid-cols-[110px_1fr_90px] gap-2">
        <select
          value={type}
          onChange={(e) => setType(e.target.value as "http" | "https" | "socks5")}
          className={inputCls}
        >
          <option value="http">HTTP</option>
          <option value="https">HTTPS</option>
          <option value="socks5">SOCKS5</option>
        </select>
        <input
          value={host}
          onChange={(e) => setHost(e.target.value)}
          placeholder={t("network.hostPh")}
          aria-label={t("network.host")}
          className={inputCls}
        />
        <input
          value={port}
          onChange={(e) => setPort(e.target.value.replace(/\D/g, ""))}
          placeholder={t("network.portPh")}
          aria-label={t("network.port")}
          className={inputCls}
        />
      </div>
      {type === "https" && (
        <div className="mt-3 flex items-center justify-between rounded-lg border border-white/[0.07] bg-white/[0.02] px-3 py-2">
          <span className="text-[12px] text-slate-300">{t("network.insecure")}</span>
          <SToggle on={insecure} onChange={setInsecure} />
        </div>
      )}
      <div className="mt-3 grid grid-cols-2 gap-2">
        <input
          value={username}
          onChange={(e) => setUsername(e.target.value)}
          placeholder={t("network.username")}
          className={inputCls}
        />
        <input
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          type="password"
          placeholder={initial?.hasPassword ? t("network.passwordSaved") : t("network.password")}
          className={inputCls}
        />
      </div>
      {initial?.hasPassword && (
        <SBtn onClick={() => void resetPassword()} disabled={resetting}>
          {resetting ? <Loader2 size={12} className="animate-spin" /> : null}
          {t("network.resetPassword")}
        </SBtn>
      )}
    </SModal>
  );
}
