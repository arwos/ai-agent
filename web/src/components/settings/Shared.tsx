import { motion } from "framer-motion";
import { X } from "lucide-react";
import type { LucideIcon } from "lucide-react";
import type { ReactNode } from "react";

/* ----------------------------------------------------------- modal shell */

export function ModalShell({
  title,
  subtitle,
  icon: Icon,
  rgb = "129,140,248",
  onClose,
  children,
}: {
  title: string;
  subtitle?: string;
  icon?: LucideIcon;
  rgb?: string;
  onClose: () => void;
  children: ReactNode;
}) {
  return (
    <motion.div
      className="fixed inset-0 z-50 grid place-items-center p-4"
      initial={{ opacity: 0 }}
      animate={{ opacity: 1 }}
      exit={{ opacity: 0 }}
    >
      <div className="absolute inset-0 bg-abyss-950/75 backdrop-blur-sm" onClick={onClose} />
      <motion.div
        initial={{ opacity: 0, y: 18, scale: 0.96 }}
        animate={{ opacity: 1, y: 0, scale: 1 }}
        exit={{ opacity: 0, y: 18, scale: 0.96 }}
        transition={{ duration: 0.22, ease: "easeOut" }}
        className="relative w-full max-w-lg rounded-xl p-px"
        style={{
          background: `linear-gradient(160deg, rgba(${rgb},0.5), rgba(255,255,255,0.08) 45%, rgba(34,211,238,0.35))`,
          boxShadow: `0 30px 80px -20px rgba(0,0,0,0.8), 0 0 50px -18px rgba(${rgb},0.4)`,
        }}
      >
        <div className="scroll-slim max-h-[85vh] overflow-y-auto rounded-[11px] bg-abyss-850 p-5">
          <div className="mb-4 flex items-center gap-3">
            {Icon && (
              <span
                className="grid h-9 w-9 shrink-0 place-items-center rounded-lg"
                style={{ background: `rgba(${rgb},0.12)` }}
              >
                <Icon size={16} style={{ color: `rgb(${rgb})` }} />
              </span>
            )}
            <div className="min-w-0 flex-1">
              <p className="font-display text-sm font-semibold text-slate-100">{title}</p>
              {subtitle && <p className="font-mono text-[10px] text-slate-500">{subtitle}</p>}
            </div>
            <button
              onClick={onClose}
              className="grid h-8 w-8 shrink-0 place-items-center rounded-lg text-slate-400 transition-colors hover:bg-white/5 hover:text-white"
              aria-label="Закрыть"
            >
              <X size={15} />
            </button>
          </div>
          {children}
        </div>
      </motion.div>
    </motion.div>
  );
}

/* ----------------------------------------------------------------- toggle */

export function Toggle({
  on,
  onChange,
  rgb = "129,140,248",
}: {
  on: boolean;
  onChange: (v: boolean) => void;
  rgb?: string;
}) {
  return (
    <button
      onClick={() => onChange(!on)}
      role="switch"
      aria-checked={on}
      className={`relative h-[22px] w-10 shrink-0 rounded-full transition-colors duration-200 ${
        on ? "" : "border border-white/15 bg-white/5"
      }`}
      style={on ? { background: `rgba(${rgb},0.85)`, boxShadow: `0 0 12px -2px rgba(${rgb},0.7)` } : undefined}
    >
      <motion.span
        layout
        transition={{ type: "spring", stiffness: 520, damping: 32 }}
        className={`absolute top-[2px] h-4 w-4 rounded-full ${on ? "left-[21px] bg-white" : "left-[2px] bg-slate-400"}`}
      />
    </button>
  );
}

/* ------------------------------------------------------------------ forms */

export const inputCls =
  "w-full rounded-lg border border-white/10 bg-abyss-900/80 px-3 py-2 text-[13px] text-slate-200 outline-none transition-colors placeholder:text-slate-600 focus:border-indigo-400/50 focus:bg-abyss-900";

export function Field({ label, hint, children }: { label: string; hint?: string; children: ReactNode }) {
  return (
    <label className="mb-3.5 block">
      <span className="mb-1.5 flex items-baseline justify-between">
        <span className="text-[11.5px] font-medium text-slate-400">{label}</span>
        {hint && <span className="font-mono text-[9.5px] text-slate-600">{hint}</span>}
      </span>
      {children}
    </label>
  );
}

export function PrimaryBtn({
  children,
  onClick,
  disabled,
  rgb = "129,140,248",
}: {
  children: ReactNode;
  onClick: () => void;
  disabled?: boolean;
  rgb?: string;
}) {
  return (
    <button
      onClick={onClick}
      disabled={disabled}
      className="rounded-lg px-4 py-2 text-xs font-semibold text-white transition-all duration-200 hover:brightness-125 active:scale-[0.97] disabled:cursor-not-allowed disabled:opacity-30"
      style={{ background: `rgba(${rgb},0.85)`, boxShadow: `0 0 22px -6px rgba(${rgb},0.65)` }}
    >
      {children}
    </button>
  );
}

export function GhostBtn({ children, onClick }: { children: ReactNode; onClick: () => void }) {
  return (
    <button
      onClick={onClick}
      className="rounded-lg border border-white/10 bg-white/[0.04] px-4 py-2 text-xs font-medium text-slate-200 transition-colors hover:bg-white/[0.09]"
    >
      {children}
    </button>
  );
}

/* ------------------------------------------------------------ empty state */

export function EmptyState({
  icon: Icon,
  title,
  desc,
  action,
}: {
  icon: LucideIcon;
  title: string;
  desc: string;
  action?: ReactNode;
}) {
  return (
    <div className="grid place-items-center rounded-xl border border-dashed border-white/[0.1] bg-white/[0.015] px-6 py-14 text-center">
      <span className="grid h-12 w-12 place-items-center rounded-xl border border-white/[0.08] bg-white/[0.03]">
        <Icon size={20} className="text-slate-500" />
      </span>
      <p className="mt-4 font-display text-sm font-semibold text-slate-200">{title}</p>
      <p className="mt-1.5 max-w-sm text-xs leading-relaxed text-slate-500">{desc}</p>
      {action && <div className="mt-5">{action}</div>}
    </div>
  );
}

/* ------------------------------------------------------------- card shell */

export function GlassCard({ children, className = "" }: { children: ReactNode; className?: string }) {
  return (
    <motion.div
      initial={{ opacity: 0, y: 12 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.3, ease: "easeOut" }}
      className={`glass rounded-xl transition-colors duration-300 hover:border-white/[0.13] ${className}`}
    >
      {children}
    </motion.div>
  );
}

export function PageToolbar({ children, meta }: { children: ReactNode; meta?: string }) {
  return (
    <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
      {meta && <p className="font-mono text-[10.5px] text-slate-500">{meta}</p>}
      <div className="ml-auto flex items-center gap-2">{children}</div>
    </div>
  );
}
