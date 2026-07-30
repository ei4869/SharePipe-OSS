import { ArrowRight, Hash, ShieldCheck } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { APIError, api } from "../api";
import { AppHeader } from "../components/AppHeader";
import type { PublicConfig } from "../types";
import { zhCN } from "../i18n/zh-CN";
import "./PairPage.css";

declare global { interface Window { turnstile?: { render: (element: HTMLElement, options: { sitekey: string; action: string; callback: (token: string) => void; "expired-callback": () => void }) => string } } }

export function PairPage() {
  const [code, setCode] = useState("");
  const [problem, setProblem] = useState("");
  const [busy, setBusy] = useState(false);
  const [config, setConfig] = useState<PublicConfig | null>(null);
  const [turnstileToken, setTurnstileToken] = useState("");
  const [challenge, setChallenge] = useState(false);
  const challengeRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => { inputRef.current?.focus(); void api.publicConfig().then(setConfig); }, []);
  useEffect(() => {
    if (!challenge || !config?.turnstileSiteKey || !challengeRef.current) return;
    const render = () => window.turnstile?.render(challengeRef.current!, { sitekey: config.turnstileSiteKey, action: "pair_resolve", callback: setTurnstileToken, "expired-callback": () => setTurnstileToken("") });
    if (window.turnstile) { render(); return; }
    const script = document.createElement("script"); script.src = "https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit"; script.async = true; script.defer = true; script.onload = render; document.head.appendChild(script);
  }, [challenge, config?.turnstileSiteKey]);

  const submit = async (event: React.FormEvent) => {
    event.preventDefault(); if (code.length !== 6) { setProblem(zhCN.pairPage.incomplete); return }
    setBusy(true); setProblem("");
    try { const result = await api.resolvePairCode(code, turnstileToken); window.location.assign(result.url) }
    catch (error) { if (error instanceof APIError && error.code === "challenge_required") { setChallenge(true); setProblem(zhCN.pairPage.challenge); } else setProblem(error instanceof Error ? error.message : zhCN.pairPage.invalid) }
    finally { setBusy(false) }
  };

  return <div className="pair-page"><AppHeader management={false} /><main className="pair-shell"><span className="pair-icon"><Hash size={30} /></span><h1>{zhCN.pairPage.title}</h1><p>{zhCN.pairPage.lead}</p><form onSubmit={submit}><label><span className="field-label">{zhCN.pairPage.code}</span><input ref={inputRef} className="pair-code-input" inputMode="numeric" autoComplete="one-time-code" maxLength={6} value={code} onChange={(event) => setCode(event.target.value.replace(/\D/gu, "").slice(0, 6))} onPaste={(event) => { const next = event.clipboardData.getData("text").replace(/\D/gu, "").slice(0, 6); if (next) { event.preventDefault(); setCode(next) } }} aria-label={zhCN.pairPage.code} /></label>{challenge && <div className="turnstile-box"><div ref={challengeRef} /></div>}{problem && <p className="error-text">{problem}</p>}<button className="primary-button pair-submit" disabled={busy || (challenge && !turnstileToken)}>{busy ? zhCN.pairPage.searching : zhCN.pairPage.receive}<ArrowRight size={18} /></button></form><div className="pair-security"><ShieldCheck size={18} /><span>{zhCN.pairPage.security}</span></div><a href="/">{zhCN.pairPage.back}</a></main></div>;
}
