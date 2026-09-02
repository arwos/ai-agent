import { useEffect, useRef, useState } from "react";
import { motion } from "framer-motion";
import { Check, Copy, Paperclip, RefreshCw, RotateCcw, UserRound, Wrench } from "lucide-react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { Prism as SyntaxHighlighter } from "react-syntax-highlighter";
import { vscDarkPlus } from "react-syntax-highlighter/dist/esm/styles/prism";
import type { ChatMessage, LiveAgent, MessagePart, Preset } from "../lib/data";
import { ACCENTS, formatMessageTime } from "../lib/data";
import { useT } from "../lib/i18n";

/* ------------------------------------------------------------ code block */

function CodeBlock({ code, lang }: { code: string; lang: string }) {
  const { t } = useT();
  const [copied, setCopied] = useState(false);
  const lines = code.split("\n");

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(code);
    } catch {
      const ta = document.createElement("textarea");
      ta.value = code;
      document.body.appendChild(ta);
      ta.select();
      document.execCommand("copy");
      ta.remove();
    }
    setCopied(true);
    setTimeout(() => setCopied(false), 1600);
  };

  return (
    <motion.div
      initial={{ opacity: 0, y: 10, scale: 0.99 }}
      animate={{ opacity: 1, y: 0, scale: 1 }}
      transition={{ duration: 0.3, ease: "easeOut" }}
      className="overflow-hidden rounded-xl border border-white/[0.07] bg-[#090d17]"
    >
      <div className="flex items-center justify-between border-b border-white/[0.06] bg-white/[0.025] px-4 py-2">
        <div className="flex items-center gap-3">
          <span className="flex gap-1.5">
            <span className="h-2.5 w-2.5 rounded-full bg-rose-400/70" />
            <span className="h-2.5 w-2.5 rounded-full bg-amber-400/70" />
            <span className="h-2.5 w-2.5 rounded-full bg-emerald-400/70" />
          </span>
          <span className="font-mono text-[10px] uppercase tracking-[0.22em] text-slate-500">{lang}</span>
        </div>
        <button
          onClick={() => void copy()}
          className={`flex items-center gap-1.5 rounded-md border px-2 py-1 font-mono text-[10px] transition-all ${
            copied
              ? "border-emerald-400/30 bg-emerald-400/10 text-emerald-300"
              : "border-white/[0.08] bg-white/[0.03] text-slate-400 hover:border-white/20 hover:text-slate-100"
          }`}
        >
          {copied ? <Check size={11} /> : <Copy size={11} />}
          {copied ? t("chat.copied") : t("chat.copy")}
        </button>
      </div>
      <div className="scroll-slim flex max-h-[380px] overflow-auto py-3 pl-4 pr-2">
        <div className="select-none pr-4 text-right font-mono text-[11px] leading-6 text-slate-700">
          {lines.map((_, i) => (
            <div key={i}>{i + 1}</div>
          ))}
        </div>
        <SyntaxHighlighter
          language={lang === "code" ? "text" : lang}
          style={vscDarkPlus}
          customStyle={{ margin: 0, padding: 0, background: "transparent", fontSize: "12.5px", lineHeight: "1.5" }}
          codeTagProps={{ className: "font-mono" }}
        >
          {code}
        </SyntaxHighlighter>
      </div>
    </motion.div>
  );
}

/* ----------------------------------------------------------- md-lite text */

function Inline({ text }: { text: string }) {
  const parts = text.split(/(\*\*[^*]+\*\*|`[^`]+`)/g).filter(Boolean);
  return (
    <>
      {parts.map((p, i) =>
        p.startsWith("**") ? (
          <strong key={i} className="font-semibold text-slate-100">
            {p.slice(2, -2)}
          </strong>
        ) : p.startsWith("`") ? (
          <code
            key={i}
            className="rounded-md border border-white/10 bg-white/[0.06] px-1.5 py-0.5 font-mono text-[11.5px] text-cyan-200"
          >
            {p.slice(1, -1)}
          </code>
        ) : (
          <span key={i}>{p}</span>
        ),
      )}
    </>
  );
}

async function copyMessage(text: string): Promise<void> {
  try {
    await navigator.clipboard.writeText(text);
  } catch {
    const area = document.createElement("textarea");
    area.value = text;
    document.body.appendChild(area);
    area.select();
    document.execCommand("copy");
    area.remove();
  }
}

function CopyMessageButton({ text }: { text: string }) {
  const { t } = useT();
  const [copied, setCopied] = useState(false);
  const copy = async () => {
    await copyMessage(text);
    setCopied(true);
    window.setTimeout(() => setCopied(false), 1400);
  };
  return (
    <button
      onClick={() => void copy()}
      title={copied ? t("chat.copied") : t("chat.copyMessage")}
      aria-label={copied ? t("chat.copied") : t("chat.copyMessage")}
      className={`inline-flex h-6 w-6 items-center justify-center rounded-md border transition-all duration-200 ${copied ? "scale-110 border-emerald-400/30 bg-emerald-400/10 text-emerald-300" : "border-transparent text-slate-600 hover:border-white/[0.08] hover:bg-white/[0.05] hover:text-slate-300"}`}
    >
      <motion.span
        initial={false}
        animate={{ scale: copied ? [1, 1.3, 1] : 1, rotate: copied ? [0, -8, 0] : 0 }}
        transition={{ duration: 0.3 }}
      >
        {copied ? <Check size={12} /> : <Copy size={12} />}
      </motion.span>
    </button>
  );
}

function MarkdownLite({ text, accentRgb }: { text: string; accentRgb: string }) {
  return (
    <div className="markdown-content text-[13.5px] leading-relaxed text-slate-300">
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        components={{
          h1: ({ children }) => (
            <h1 className="mb-3 mt-4 font-display text-xl font-semibold text-slate-100">{children}</h1>
          ),
          h2: ({ children }) => (
            <h2 className="mb-2 mt-4 font-display text-lg font-semibold text-slate-100">{children}</h2>
          ),
          h3: ({ children }) => (
            <h3 className="mb-2 mt-3 font-display text-base font-semibold text-slate-100">{children}</h3>
          ),
          p: ({ children }) => <p className="mb-2.5 last:mb-0">{children}</p>,
          ul: ({ children }) => <ul className="mb-2.5 list-disc space-y-1 pl-5 marker:text-cyan-300">{children}</ul>,
          ol: ({ children }) => <ol className="mb-2.5 list-decimal space-y-1 pl-5 marker:text-cyan-300">{children}</ol>,
          li: ({ children }) => <li className="pl-1">{children}</li>,
          blockquote: ({ children }) => (
            <blockquote
              className="my-3 border-l-2 pl-3 italic text-slate-400"
              style={{ borderColor: `rgba(${accentRgb},0.7)` }}
            >
              {children}
            </blockquote>
          ),
          table: ({ children }) => (
            <div className="scroll-slim my-3 overflow-x-auto">
              <table className="min-w-full border-collapse text-left text-[12px]">{children}</table>
            </div>
          ),
          th: ({ children }) => (
            <th className="border border-white/10 bg-white/[0.06] px-3 py-2 font-semibold text-slate-100">
              {children}
            </th>
          ),
          td: ({ children }) => <td className="border border-white/10 px-3 py-2 align-top">{children}</td>,
          code: ({ className, children, ...props }) =>
            className ? (
              <code className="font-mono text-[12.5px] leading-6" {...props}>
                {children}
              </code>
            ) : (
              <code
                className="rounded-md border border-white/10 bg-white/[0.06] px-1.5 py-0.5 font-mono text-[11.5px] text-cyan-200"
                {...props}
              >
                {children}
              </code>
            ),
          pre: ({ children }) => (
            <pre className="scroll-slim my-3 overflow-x-auto rounded-xl border border-white/[0.07] bg-[#090d17] p-4">
              {children}
            </pre>
          ),
          a: ({ children, href }) => (
            <a
              href={href}
              target="_blank"
              rel="noreferrer"
              className="text-cyan-300 underline decoration-cyan-300/40 underline-offset-2 hover:text-cyan-200"
            >
              {children}
            </a>
          ),
          img: ({ src, alt, title }) => (
            <img
              src={src}
              alt={alt ?? ""}
              title={title}
              loading="lazy"
              decoding="async"
              className="my-3 max-h-[520px] max-w-full rounded-xl border border-white/[0.09] object-contain shadow-[0_0_24px_-12px_rgba(34,211,238,0.35)]"
            />
          ),
          hr: () => <hr className="my-4 border-white/10" />,
        }}
      >
        {text}
      </ReactMarkdown>
    </div>
  );
}

/* ------------------------------------------------------------- avatar row */

function AgentAvatar({ agent, glow }: { agent: LiveAgent; glow: boolean }) {
  const accent = ACCENTS[agent.accent];
  return (
    <span
      className="grid h-6 w-6 shrink-0 place-items-center rounded-md"
      style={{
        background: `rgba(${accent.rgb},0.12)`,
        boxShadow: glow
          ? `inset 0 0 0 1px rgba(${accent.rgb},0.35), 0 0 22px -4px rgba(${accent.rgb},0.6)`
          : `inset 0 0 0 1px rgba(${accent.rgb},0.2)`,
      }}
    >
      <agent.icon size={12} style={{ color: accent.hex }} />
    </span>
  );
}

/* -------------------------------------------------------------- messages */

function AgentMessage({
  agent,
  message,
  isStreaming,
  onContinue,
  onSwitchAndContinue,
  canSwitchModel,
  onDelete,
}: {
  agent: LiveAgent;
  message: ChatMessage;
  isStreaming: boolean;
  onContinue: (messageID: string) => void;
  onSwitchAndContinue?: (messageID: string) => void;
  canSwitchModel: boolean;
  onDelete: (id: string) => void;
}) {
  const { t } = useT();
  const text = message.parts
    .filter((part) => part.type !== "reasoning")
    .map((part) => part.content)
    .join("\n");
  const reasoningParts = message.parts.filter((part) => part.type === "reasoning");
  const answerParts = message.parts.filter((part) => part.type !== "reasoning");
  const messageModel = message.model
    ? `${message.model}${message.provider ? `@${message.provider}` : ""}`
    : agent.model;
  const renderPart = (part: MessagePart, i: number) =>
    part.type === "reasoning" ? (
      <details
        key={i}
        open
        className="mb-2 rounded-lg border border-amber-400/30 bg-amber-400/[0.07] px-3 py-2 text-xs text-amber-200/80"
      >
        <summary className="cursor-pointer select-none font-mono text-[10px] uppercase tracking-wider text-amber-300/80">
          Reasoning
        </summary>
        <div className="mt-2 whitespace-pre-wrap">{part.content}</div>
      </details>
    ) : part.type === "code" ? (
      <CodeBlock key={i} code={part.content} lang={part.lang ?? "code"} />
    ) : (
      part.content && <MarkdownLite key={i} text={part.content} accentRgb={ACCENTS[agent.accent].rgb} />
    );

  return (
    <motion.div
      initial={{ opacity: 0, y: 16 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.35, ease: "easeOut" }}
      className="group"
    >
      <div className="min-w-0">
        <div className="mb-1.5 flex items-center gap-2">
          <AgentAvatar agent={agent} glow={isStreaming} />
          <span className="font-display text-[13px] font-semibold text-slate-100">{agent.name}</span>
          <span className="rounded border border-white/[0.08] bg-white/[0.03] px-1.5 py-px font-mono text-[9px] text-slate-500">
            {messageModel}
          </span>
          <span className="font-mono text-[10px] text-slate-600">{formatMessageTime(message.time)}</span>
        </div>
        {reasoningParts.map(renderPart)}
        {(answerParts.length > 0 || message.retryableError) && (
          <div className="glass space-y-3 rounded-xl rounded-tl-sm px-4 py-2 transition-colors duration-300 group-hover:border-white/[0.12]">
            {answerParts.map(renderPart)}
            {message.retryableError && (
              <div className="flex flex-wrap gap-2">
                <button
                  type="button"
                  disabled={isStreaming}
                  onClick={() => onContinue(message.id)}
                  className="inline-flex items-center gap-1.5 rounded-md border border-amber-400/25 bg-amber-400/[0.08] px-2.5 py-1.5 text-[11px] text-amber-200 transition-colors hover:bg-amber-400/[0.15] disabled:cursor-wait disabled:opacity-60"
                >
                  <RotateCcw size={12} />
                  {t("chat.continue")}
                </button>
                {canSwitchModel && onSwitchAndContinue && (
                  <button
                    type="button"
                    disabled={isStreaming}
                    onClick={() => onSwitchAndContinue(message.id)}
                    className="inline-flex items-center gap-1.5 rounded-md border border-indigo-400/25 bg-indigo-400/[0.08] px-2.5 py-1.5 text-[11px] text-indigo-200 transition-colors hover:bg-indigo-400/[0.15] disabled:cursor-wait disabled:opacity-60"
                  >
                    <RefreshCw size={12} />
                    {t("chat.switchAndContinue")}
                  </button>
                )}
              </div>
            )}
            {isStreaming && message.parts.length > 0 && (
              <span className="ml-0.5 inline-flex items-center gap-1 translate-y-0.5" aria-hidden="true">
                {[0, 1, 2].map((i) => (
                  <span
                    key={i}
                    className="typing-dot h-1.5 w-1.5 rounded-full bg-cyan-300/80"
                    style={{ animationDelay: `${i * 0.16}s` }}
                  />
                ))}
              </span>
            )}
          </div>
        )}
        <div className="mt-1 flex items-center gap-2">
          <CopyMessageButton text={text} />
          <button
            type="button"
            onClick={() => onDelete(message.id)}
            className="text-slate-600 hover:text-rose-300"
            title={t("chat.deleteConfirm")}
          >
            ×
          </button>
        </div>
      </div>
    </motion.div>
  );
}

function UserMessage({
  message,
  accentRgb,
  userName,
  onDelete,
}: {
  message: ChatMessage;
  accentRgb: string;
  userName: string;
  onDelete: (id: string) => void;
}) {
  const { t } = useT();
  const text = message.parts.map((p) => p.content).join("\n");
  return (
    <motion.div
      initial={{ opacity: 0, y: 16 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.3, ease: "easeOut" }}
      className="flex justify-end"
    >
      <div className="max-w-[86%] sm:max-w-[75%]">
        <div className="mb-1.5 flex items-center justify-end gap-2">
          <span className="font-mono text-[10px] text-slate-600">{formatMessageTime(message.time)}</span>
          <span className="font-display text-[13px] font-semibold text-slate-100">{userName}</span>
          <span
            className="grid h-6 w-6 place-items-center rounded-md border"
            style={{ borderColor: `rgba(${accentRgb},0.28)`, background: `rgba(${accentRgb},0.1)` }}
          >
            <UserRound size={12} style={{ color: `rgb(${accentRgb})` }} />
          </span>
        </div>
        {message.attachments && message.attachments.length > 0 && (
          <div className="mb-1.5 flex flex-wrap justify-end gap-1.5">
            {message.attachments.map((f, i) => (
              <span
                key={i}
                className="flex items-center gap-1.5 rounded-md border border-white/10 bg-white/[0.04] px-2 py-1 font-mono text-[10px] text-slate-400"
              >
                <Paperclip size={10} />
                {f}
              </span>
            ))}
          </div>
        )}
        {message.skills && message.skills.length > 0 && (
          <div className="mb-1.5 flex flex-wrap justify-end gap-1.5">
            {message.skills.map((s, i) => (
              <span
                key={i}
                className="rounded-md border border-violet-400/25 bg-violet-400/[0.08] px-2 py-1 font-mono text-[10px] text-violet-300"
              >
                ⚡ {s}
              </span>
            ))}
          </div>
        )}
        <div
          className="rounded-xl rounded-tr-sm px-4 py-2 text-[13.5px] leading-relaxed text-slate-200"
          style={{
            background: `rgba(${accentRgb},0.09)`,
            border: `1px solid rgba(${accentRgb},0.24)`,
            boxShadow: `0 0 30px -14px rgba(${accentRgb},0.45)`,
          }}
        >
          <MarkdownLite text={text} accentRgb={accentRgb} />
        </div>
        <div className="mt-1 flex justify-end gap-2">
          <CopyMessageButton text={text} />
          <button
            type="button"
            onClick={() => onDelete(message.id)}
            className="text-slate-600 hover:text-rose-300"
            title={t("chat.deleteConfirm")}
          >
            ×
          </button>
        </div>
      </div>
    </motion.div>
  );
}

function ToolMessage({ agent, message }: { agent: LiveAgent; message: ChatMessage }) {
  const { t } = useT();
  return (
    <motion.div initial={{ opacity: 0, y: 10 }} animate={{ opacity: 1, y: 0 }} className="flex gap-3 pl-12">
      <div className="min-w-0 flex-1">
        <div className="mb-1.5 flex items-center gap-2 pl-1">
          <AgentAvatar agent={agent} glow={message.toolRunning === true} />
          <span className="font-display text-[13px] font-semibold text-slate-100">{agent.name}</span>
          {(message.model || message.provider) && (
            <span className="rounded border border-white/[0.08] bg-white/[0.03] px-1.5 py-px font-mono text-[9px] text-slate-500">
              {message.model}
              {message.provider ? `@${message.provider}` : ""}
            </span>
          )}
          <span className="font-mono text-[10px] text-slate-600">{formatMessageTime(message.time)}</span>
        </div>
        <div className="rounded-xl border border-cyan-400/20 bg-cyan-400/[0.05] px-4 py-3 text-[12px] text-slate-300">
          <div className="mb-2 flex items-center gap-2 text-cyan-300">
            <Wrench size={14} />
            <span className="font-mono">{message.toolName}</span>
            {message.toolRunning && <span className="text-slate-500">{t("chat.toolCalling")}</span>}
          </div>
          {message.toolArguments && Object.keys(message.toolArguments).length > 0 && (
            <pre className="mb-2 max-h-32 overflow-auto whitespace-pre-wrap break-words rounded bg-black/20 p-2 font-mono text-[11px] text-slate-400">
              {JSON.stringify(message.toolArguments, null, 2)}
            </pre>
          )}
          {!message.toolRunning && (
            <>
              <div className="mb-1 text-[11px] uppercase tracking-wider text-slate-500">
                {message.toolError ? t("chat.toolError") : t("chat.toolResult")}
              </div>
              <pre
                className={`max-h-48 overflow-auto whitespace-pre-wrap break-words rounded bg-black/20 p-2 font-mono text-[11px] ${message.toolError ? "text-rose-300" : "text-slate-300"}`}
              >
                {message.toolError ?? message.toolResult ?? ""}
              </pre>
            </>
          )}
        </div>
      </div>
    </motion.div>
  );
}

/* -------------------------------------------------------- thinking state */

function ThinkingRow({ agent }: { agent: LiveAgent }) {
  const { t } = useT();
  const accent = ACCENTS[agent.accent];
  return (
    <motion.div initial={{ opacity: 0, y: 10 }} animate={{ opacity: 1, y: 0 }} className="flex gap-3">
      <motion.span
        animate={{ opacity: [1, 0.55, 1] }}
        transition={{ repeat: Infinity, duration: 1.6, ease: "easeInOut" }}
      >
        <AgentAvatar agent={agent} glow />
      </motion.span>
      <div className="glass flex items-center gap-3 self-start rounded-xl rounded-tl-sm px-4 py-3">
        <span className="flex gap-1">
          {[0, 1, 2].map((i) => (
            <span
              key={i}
              className="typing-dot h-1.5 w-1.5 rounded-full"
              style={{ background: accent.hex, animationDelay: `${i * 0.16}s` }}
            />
          ))}
        </span>
        <span className="font-mono text-[11px] text-slate-400">
          {agent.status === "thinking" ? t("chat.thinking", { name: agent.name }) : t("chat.executing")}
        </span>
      </div>
    </motion.div>
  );
}

/* ------------------------------------------------------------- chat area */

type Props = {
  agent: LiveAgent;
  messages: ChatMessage[];
  busy: boolean;
  hasWorkspace: boolean;
  onPreset: (text: string) => void;
  presets: Preset[];
  userName: string;
  onContinue: (messageID: string) => void;
  onSwitchAndContinue?: (messageID: string) => void;
  continuingErrorMessageId: string | null;
  onDeleteMessage: (id: string) => void;
};

export function ChatArea({
  agent,
  messages,
  busy,
  hasWorkspace,
  onPreset,
  presets,
  userName,
  onContinue,
  onSwitchAndContinue,
  continuingErrorMessageId,
  onDeleteMessage,
}: Props) {
  const { t } = useT();
  const [deleteMessageId, setDeleteMessageId] = useState<string | null>(null);
  const [deleting, setDeleting] = useState(false);
  const confirmDelete = async () => {
    if (!deleteMessageId) return;
    setDeleting(true);
    try {
      await onDeleteMessage(deleteMessageId);
      setDeleteMessageId(null);
    } finally {
      setDeleting(false);
    }
  };
  const scrollRef = useRef<HTMLDivElement>(null);
  const accent = ACCENTS[agent.accent];

  const contentLen = messages.reduce((acc, m) => acc + m.parts.reduce((a, p) => a + p.content.length, 0), 0);

  const prevLen = useRef(0);
  useEffect(() => {
    const el = scrollRef.current;
    if (!el) return;
    const isNewMessage = messages.length !== prevLen.current || prevLen.current === 0;
    prevLen.current = messages.length;
    el.scrollTo({ top: el.scrollHeight, behavior: isNewMessage ? "smooth" : "auto" });
  }, [messages.length, contentLen, busy, agent.id]);

  const streamingId =
    busy && messages.length && messages[messages.length - 1].id !== continuingErrorMessageId
      ? messages[messages.length - 1].id
      : null;
  const lastMsg = messages[messages.length - 1];
  const showThinking =
    busy &&
    (!lastMsg || lastMsg.role === "user" || lastMsg.parts.length === 0 || lastMsg.id === continuingErrorMessageId);

  return (
    <div ref={scrollRef} className="scroll-slim relative z-10 flex-1 overflow-y-auto">
      <div className="mx-auto max-w-3xl space-y-6 px-4 py-4 sm:px-6 sm:py-6">
        {!hasWorkspace && (
          <div className="rounded-xl border border-amber-400/30 bg-amber-400/[0.08] px-4 py-3 text-center text-sm text-amber-200">
            {t("chat.workspaceRequired")}
          </div>
        )}
        {messages.length === 0 && !busy ? (
          <motion.div
            initial={{ opacity: 0, scale: 0.96 }}
            animate={{ opacity: 1, scale: 1 }}
            transition={{ duration: 0.35 }}
            className="flex flex-col items-center py-20 text-center"
          >
            <div
              className="grid h-14 w-14 place-items-center rounded-2xl"
              style={{
                background: `rgba(${accent.rgb},0.1)`,
                boxShadow: `inset 0 0 0 1px rgba(${accent.rgb},0.28), 0 0 40px -8px rgba(${accent.rgb},0.5)`,
              }}
            >
              <agent.icon size={24} style={{ color: accent.hex }} />
            </div>
            <h2 className="mt-5 font-display text-xl font-semibold text-slate-100">
              {t("chat.emptyTitle", { name: agent.name })}
            </h2>
            <p className="mt-2 max-w-sm text-[13px] leading-relaxed text-slate-500">{t("chat.emptyText")}</p>
            <div className="mt-6 flex max-w-md flex-wrap justify-center gap-2">
              {presets.map((p) => (
                <button
                  key={p.id}
                  onClick={() => onPreset(p.text)}
                  title={p.text}
                  className="rounded-lg border border-white/[0.08] bg-white/[0.03] px-3 py-2 text-xs text-slate-300 transition-all hover:-translate-y-0.5 hover:border-white/20 hover:bg-white/[0.07] hover:text-white"
                >
                  {p.title}
                </button>
              ))}
            </div>
          </motion.div>
        ) : (
          <>
            <div className="flex items-center gap-3">
              <span className="h-px flex-1 bg-white/[0.05]" />
              <span className="font-mono text-[9.5px] tracking-[0.28em] text-slate-600">{t("chat.today")}</span>
              <span className="h-px flex-1 bg-white/[0.05]" />
            </div>

            {messages.map((m) =>
              m.role === "user" ? (
                <UserMessage
                  key={m.id}
                  message={m}
                  accentRgb={accent.rgb}
                  userName={userName}
                  onDelete={setDeleteMessageId}
                />
              ) : m.role === "tool" ? (
                <ToolMessage key={m.id} agent={agent} message={m} />
              ) : (
                <AgentMessage
                  key={m.id}
                  agent={agent}
                  message={m}
                  isStreaming={m.id === streamingId}
                  onContinue={onContinue}
                  onSwitchAndContinue={onSwitchAndContinue}
                  canSwitchModel={agent.mainModels.length > 1}
                  onDelete={setDeleteMessageId}
                />
              ),
            )}

            {showThinking && <ThinkingRow agent={agent} />}

            <div className="h-2" />
          </>
        )}
      </div>
      {deleteMessageId && (
        <div className="fixed inset-0 z-[100] grid place-items-center bg-abyss-950/70 px-4 backdrop-blur-sm">
          <div className="w-full max-w-sm rounded-xl border border-white/10 bg-abyss-850 p-5 shadow-2xl">
            <h3 className="font-display text-base font-semibold text-slate-100">{t("chat.deleteMessageTitle")}</h3>
            <p className="mt-2 text-[12px] leading-relaxed text-slate-400">{t("chat.deleteMessageText")}</p>
            <div className="mt-5 flex justify-end gap-2">
              <button
                type="button"
                onClick={() => setDeleteMessageId(null)}
                disabled={deleting}
                className="rounded-md border border-white/10 px-3 py-1.5 text-[11px] text-slate-300 hover:bg-white/[0.05]"
              >
                {t("common.cancel")}
              </button>
              <button
                type="button"
                disabled={deleting}
                onClick={() => void confirmDelete()}
                className="rounded-md border border-rose-400/30 bg-rose-400/10 px-3 py-1.5 text-[11px] text-rose-200 hover:bg-rose-400/20 disabled:opacity-50"
              >
                {deleting ? t("common.saving") : t("common.delete")}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
