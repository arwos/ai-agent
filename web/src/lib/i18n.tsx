import type { ReactNode } from "react";
import { createContext, useContext, useEffect, useMemo, useState } from "react";
import ru from "../locales/ru.json";
import en from "../locales/en.json";
import es from "../locales/es.json";
import it from "../locales/it.json";
import ko from "../locales/ko.json";
import fr from "../locales/fr.json";
import zh from "../locales/zh.json";
import hi from "../locales/hi.json";
import bn from "../locales/bn.json";
import pt from "../locales/pt.json";

export type Lang = "ru" | "en" | "es" | "it" | "ko" | "fr" | "zh" | "hi" | "bn" | "pt";

export type TVars = Record<string, string | number>;

const DICTS: Record<Lang, unknown> = { ru, en, es, it, ko, fr, zh, hi, bn, pt };

function lookup(dict: unknown, key: string): unknown {
  return key
    .split(".")
    .reduce<unknown>(
      (acc, k) => (acc && typeof acc === "object" ? (acc as Record<string, unknown>)[k] : undefined),
      dict,
    );
}

type Ctx = {
  lang: Lang;
  t: (key: string, vars?: TVars) => string;
  setLang: (l: Lang) => void;
};

const I18nCtx = createContext<Ctx>({ lang: "en", t: (k) => k, setLang: () => {} });

export function I18nProvider({ children }: { children: ReactNode }) {
  const [lang, setLang] = useState<Lang>(() => {
    try {
      const saved = localStorage.getItem("arwos.lang") as Lang | null;
      return saved && saved in DICTS ? saved : "en";
    } catch {
      return "en";
    }
  });

  useEffect(() => {
    try {
      localStorage.setItem("arwos.lang", lang);
    } catch {
      /* noop */
    }
    document.documentElement.lang = lang;
  }, [lang]);

  const t = useMemo(() => {
    return (key: string, vars?: TVars) => {
      const raw = lookup(DICTS[lang], key) ?? lookup(en, key);
      let s = typeof raw === "string" ? raw : key;
      if (vars) {
        for (const [k, v] of Object.entries(vars)) s = s.split(`{${k}}`).join(String(v));
      }
      return s;
    };
  }, [lang]);

  return <I18nCtx.Provider value={{ lang, t, setLang }}>{children}</I18nCtx.Provider>;
}

export function useT() {
  return useContext(I18nCtx);
}
