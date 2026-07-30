import { Ban, CalendarClock, Download, File, Hash, RefreshCw, Save, ShieldCheck } from "lucide-react";
import { useEffect, useState } from "react";
import { api } from "../api";
import { AppHeader } from "../components/AppHeader";
import { zhCN } from "../i18n/zh-CN";
import type { PublicShare } from "../types";
import { formatBytes, formatExpiry } from "../utils";
import "./ManagePage.css";

export function ManagePage({ token }: { token: string }) {
  const [data, setData] = useState<PublicShare | null>(null);
  const [expiresIn, setExpiresIn] = useState("");
  const [downloadLimit, setDownloadLimit] = useState(10);
  const [unlimited, setUnlimited] = useState(false);
  const [password, setPassword] = useState("");
  const [problem, setProblem] = useState("");
  const [notice, setNotice] = useState("");
  const [busy, setBusy] = useState(false);

  const load = async () => {
    try {
      const loaded = await api.managedShare(token);
      setData(loaded);
      setUnlimited(loaded.share.downloadLimit === null);
      setDownloadLimit(loaded.share.downloadLimit ?? 10);
    } catch (error) {
      setProblem(error instanceof Error ? error.message : zhCN.manage.cannotOpen);
    }
  };

  useEffect(() => {
    void load();
  }, [token]);

  const save = async () => {
    setBusy(true);
    setProblem("");
    setNotice("");
    try {
      const updated = await api.updateManagedShare(token, {
        expiresIn: expiresIn || undefined,
        downloadLimit: unlimited ? undefined : downloadLimit,
        unlimited,
        password: password || undefined,
      });
      setData(updated);
	  setExpiresIn("");
      setPassword("");
      setNotice(zhCN.manage.settingsSaved);
    } catch (error) {
      setProblem(error instanceof Error ? error.message : zhCN.manage.saveFailed);
    } finally {
      setBusy(false);
    }
  };

  const revoke = async () => {
    if (!window.confirm(zhCN.manage.revokeConfirm)) return;
    setBusy(true);
    try {
      await api.revokeManagedShare(token);
      await load();
      setNotice(zhCN.manage.revokedNotice);
    } catch (error) {
      setProblem(error instanceof Error ? error.message : zhCN.manage.revokeFailed);
    } finally {
      setBusy(false);
    }
  };

  if (!data) {
    return (
      <div>
        <AppHeader management={false} />
        <div className="page-state">{problem || zhCN.manage.loading}</div>
      </div>
    );
  }

  return (
    <div className="manage-page">
      <AppHeader management={false} />
      <main className="manage-shell">
        <header className="manage-heading">
          <div>
            <span className={"status-dot status-" + data.share.status} />
            <span>{zhCN.manage.status(data.share.status)}</span>
            <h1>{data.share.name}</h1>
            <p>{zhCN.manage.lead}</p>
          </div>
          <button className="danger-button" type="button" disabled={busy} onClick={revoke}>
            <Ban size={18} />
            {zhCN.manage.revoke}
          </button>
        </header>

        <section className="manage-stats">
          <div>
            <File size={22} />
            <span>{zhCN.files}</span>
            <strong>{data.share.fileCount}</strong>
            <small>{formatBytes(data.share.totalBytes)}</small>
          </div>
          <div>
            <Download size={22} />
            <span>{zhCN.manage.downloaded}</span>
            <strong>{data.share.downloadCount}</strong>
            <small>{data.share.downloadLimit === null ? zhCN.common.unlimited : zhCN.manage.limit(data.share.downloadLimit)}</small>
          </div>
          <div>
            <CalendarClock size={22} />
            <span>{zhCN.manage.expiresAt}</span>
            <strong>{new Date(data.share.expiresAt).toLocaleDateString(zhCN.locale)}</strong>
            <small>{new Date(data.share.expiresAt).toLocaleTimeString(zhCN.locale)}</small>
          </div>
        </section>

        <section className="manage-form">
          <h2>{zhCN.shareSettings}</h2>
          <div className="manage-form-grid">
            <label>
              <span className="field-label">{zhCN.expiry}</span>
              <select
                className="select-input"
                value={expiresIn}
                disabled={busy}
                onChange={(event) => setExpiresIn(event.target.value)}
              >
                <option value="">{zhCN.manage.keepExpiry}</option>
                {["1h", "24h", "168h", "720h"].map((value) => (
                  <option key={value} value={value}>
                    {formatExpiry(value)}
                  </option>
                ))}
              </select>
            </label>
            <label>
              <span className="field-label">{zhCN.downloadLimit}</span>
              <input
                className="text-input"
                type="number"
                min={Math.max(data.share.downloadCount, 1)}
                value={downloadLimit}
                disabled={busy || unlimited}
                onChange={(event) => setDownloadLimit(Number(event.target.value))}
              />
              <span className="checkbox-row">
                <input
                  type="checkbox"
                  checked={unlimited}
                  onChange={(event) => setUnlimited(event.target.checked)}
                />
                {zhCN.manage.unlimitedDownloads}
              </span>
            </label>
            <label className="manage-password">
              <span className="field-label">{zhCN.manage.newPassword}</span>
              <input
                className="text-input"
                type="password"
                minLength={8}
                value={password}
                disabled={busy}
                placeholder={zhCN.manage.passwordPlaceholder}
                onChange={(event) => setPassword(event.target.value)}
              />
            </label>
          </div>
          {problem && <p className="error-text">{problem}</p>}
          {notice && (
            <p className="manage-notice">
              <ShieldCheck size={17} />
              {notice}
            </p>
          )}
          <button className="primary-button" type="button" disabled={busy} onClick={save}>
            <Save size={18} />
            {busy ? zhCN.manage.saving : zhCN.manage.saveSettings}
          </button>
        </section>
        <section className="manage-form pairing-manage">
          <div><Hash size={22} /><div><h2>{zhCN.manage.pairingTitle}</h2><p>{zhCN.manage.pairingLead}</p></div></div>
          {data.pairing?.code ? <div className="managed-pair-code"><strong>{data.pairing.code}</strong><span>{data.pairing.expiresAt ? zhCN.manage.pairingExpires(new Date(data.pairing.expiresAt).toLocaleString(zhCN.locale)) : ""}</span></div> : <p>{data.pairing?.enabled ? zhCN.manage.pairingPending : zhCN.manage.pairingDisabled}</p>}
          <div className="pairing-actions"><button className="secondary-button" disabled={busy || data.share.status !== "active"} onClick={async () => { setBusy(true); try { await api.regeneratePairCode(token); await load(); setNotice(zhCN.manage.pairingGenerated) } catch (error) { setProblem(error instanceof Error ? error.message : zhCN.manage.pairingGenerateFailed) } finally { setBusy(false) } }}><RefreshCw size={17} />{data.pairing?.code ? zhCN.manage.pairingRegenerate : zhCN.manage.pairingGenerate}</button>{data.pairing?.enabled && <button className="ghost-button danger-text" disabled={busy} onClick={async () => { await api.disablePairCode(token); await load(); setNotice(zhCN.manage.pairingClosed) }}>{zhCN.manage.pairingClose}</button>}</div>
        </section>
      </main>
    </div>
  );
}
