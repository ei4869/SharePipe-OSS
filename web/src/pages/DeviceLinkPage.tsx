import { Check, LoaderCircle, ShieldAlert } from "lucide-react";
import { useEffect, useState } from "react";
import { api } from "../api";
import { AppHeader } from "../components/AppHeader";
import { zhCN } from "../i18n/zh-CN";
import "./PairPage.css";

export function DeviceLinkPage() {
  const [state, setState] = useState<"loading" | "done" | "error">("loading");
  const [message, setMessage] = useState<string>(zhCN.deviceLink.registering);
  useEffect(() => { const token = window.location.hash.slice(1); window.history.replaceState({}, "", "/device-link"); if (!token) { setState("error"); setMessage(zhCN.deviceLink.invalid); return } void api.redeemTrustedDeviceBootstrap(token).then((result) => { setState("done"); setMessage(zhCN.deviceLink.completed(result.deviceName)) }).catch((error: Error) => { setState("error"); setMessage(error.message) }) }, []);
  return <div className="pair-page"><AppHeader management={false} /><main className="pair-shell">{state === "loading" ? <LoaderCircle className="spin" size={36} /> : state === "done" ? <Check size={36} /> : <ShieldAlert size={36} />}<h1>{state === "done" ? zhCN.deviceLink.completedTitle : state === "error" ? zhCN.deviceLink.failedTitle : zhCN.deviceLink.title}</h1><p>{message}</p><a className="primary-button" href="/">{zhCN.deviceLink.back}</a></main></div>;
}
