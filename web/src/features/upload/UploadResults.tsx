import { Check, Copy, Download, Link2, RefreshCw } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import QRCodeGenerator from "qrcode";
import { api } from "../../api";
import { AppHeader } from "../../components/AppHeader";
import { zhCN } from "../../i18n/zh-CN";
import type { PairingState, ShareCreation } from "../../types";

export function RecoverySummary({ creation, recovering }: { creation: ShareCreation; recovering: boolean }) {
  const completed = creation.uploads.filter((item) => item.status === "completed").length;
  return (
    <section className="recovery-summary" aria-live="polite">
      <RefreshCw size={22} className={recovering ? "is-spinning" : ""} />
      <div>
        <strong>{creation.share.name}</strong>
        <span>{zhCN.upload.recoveredParts(completed, creation.uploads.length)}</span>
      </div>
    </section>
  );
}

export function UploadSuccess({ creation }: { creation: ShareCreation }) {
  const [pairing, setPairing] = useState<PairingState | null>(null);
  const canvasRef = useRef<HTMLCanvasElement>(null);
  useEffect(() => {
    api.managedShare(creation.managementToken).then((value) => setPairing(value.pairing ?? null)).catch(() => undefined);
  }, [creation.managementToken]);
  useEffect(() => {
    if (!creation.share.qrEnabled || !canvasRef.current) return;
    void QRCodeGenerator.toCanvas(canvasRef.current, creation.publicUrl, { width: 220, margin: 1, errorCorrectionLevel: "M" });
  }, [creation.publicUrl, creation.share.qrEnabled]);

  const saveQR = () => {
    const href = canvasRef.current?.toDataURL("image/png");
    if (!href) return;
    const anchor = document.createElement("a");
    anchor.href = href;
    anchor.download = `sharepipe-${creation.share.id}.png`;
    anchor.click();
  };

  return (
    <div className="upload-page">
      <AppHeader management={false} />
      <main className="success-panel">
        <span className="success-icon"><Check size={34} strokeWidth={1.7} /></span>
        <h1>{zhCN.upload.successTitle}</h1>
        <p>{zhCN.upload.successLead}</p>
        <div className={"success-share-grid" + (creation.share.qrEnabled ? " has-qr" : "")}>
          <div>
            <LinkField label={zhCN.upload.publicLink} value={creation.publicUrl} />
            <LinkField label={zhCN.upload.managementLink} value={creation.managementUrl} />
            {creation.share.pairingEnabled && pairing?.code && (
              <div className="pair-code-card">
                <span>{zhCN.upload.pairCode}</span>
                <strong>{pairing.code}</strong>
                {pairing.expiresAt && <small>{zhCN.upload.pairExpires(new Date(pairing.expiresAt).toLocaleString(zhCN.locale))}</small>}
              </div>
            )}
          </div>
          {creation.share.qrEnabled && (
            <div className="qr-card">
              <canvas ref={canvasRef} aria-label={zhCN.upload.qrAria} />
              <button className="secondary-button" type="button" onClick={saveQR}><Download size={17} />{zhCN.upload.saveQR}</button>
            </div>
          )}
        </div>
        <p className="success-warning">{zhCN.upload.managementWarning}</p>
        <div className="success-actions">
          <a className="primary-button" href={creation.publicUrl}><Link2 size={18} />{zhCN.upload.openShare}</a>
          <a className="secondary-button" href="/">{zhCN.upload.createAnother}</a>
        </div>
      </main>
    </div>
  );
}

export function LinkField({ label, value, compact = false }: { label: string; value: string; compact?: boolean }) {
  const [copied, setCopied] = useState(false);
  return (
    <label className={"success-link" + (compact ? " compact" : "")}>
      <span className="field-label">{label}</span>
      <span>
        <input className="text-input" readOnly value={value} />
        <button type="button" aria-label={zhCN.upload.copyLabel(label)} onClick={async () => {
          await navigator.clipboard.writeText(value);
          setCopied(true);
          window.setTimeout(() => setCopied(false), 1800);
        }}>
          {copied ? <Check size={19} /> : <Copy size={19} />}
        </button>
      </span>
    </label>
  );
}
