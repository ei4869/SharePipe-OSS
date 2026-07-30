import { Activity, Gauge, Save } from "lucide-react";
import { useState } from "react";
import { api } from "../../api";
import { zhCN } from "../../i18n/zh-CN";
import type { RuntimeSettings } from "../../types";

export function SettingsPanel({
  value,
  cloud,
  onSaved,
  onError,
}: {
  value: RuntimeSettings;
  cloud: boolean;
  onSaved: (message: string) => Promise<void>;
  onError: (message: string) => void;
}) {
  const [form, setForm] = useState(value);
  const [busy, setBusy] = useState(false);
  const set = <K extends keyof RuntimeSettings>(key: K, next: RuntimeSettings[K]) =>
    setForm((current) => ({ ...current, [key]: next }));

  const save = async () => {
    setBusy(true);
    try {
      await api.updateSettings(form);
      await onSaved(zhCN.adminPanel.settingsSaved);
    } catch (error) {
      onError(error instanceof Error ? error.message : zhCN.adminPanel.settingsSaveFailed);
    } finally {
      setBusy(false);
    }
  };

  return (
    <>
      <header className="admin-heading">
        <h1>{zhCN.settings}</h1>
      </header>
      <section className="settings-grid admin-section">
        <label>
          <span className="field-label">{zhCN.adminPanel.maxFileGB}</span>
          <input className="text-input" type="number" min={1} value={bytesToGB(form.maxFileBytes)} onChange={(event) => set("maxFileBytes", gbToBytes(event.target.value))} />
        </label>
        <label>
          <span className="field-label">{zhCN.adminPanel.pairCodeTTLMinutes}</span>
          <input className="text-input" type="number" min={5} max={1440} value={Math.round(form.pairCodeTTLSeconds / 60)} onChange={(event) => set("pairCodeTTLSeconds", Number(event.target.value) * 60)} />
        </label>
        <label>
          <span className="field-label">{zhCN.adminPanel.maxShareGB}</span>
          <input className="text-input" type="number" min={1} value={bytesToGB(form.maxShareBytes)} onChange={(event) => set("maxShareBytes", gbToBytes(event.target.value))} />
        </label>
        <label>
          <span className="field-label">{zhCN.adminPanel.maxFiles}</span>
          <input className="text-input" type="number" min={1} value={form.maxFilesPerShare} onChange={(event) => set("maxFilesPerShare", Number(event.target.value))} />
        </label>
        <label>
          <span className="field-label">{zhCN.adminPanel.defaultDownloads}</span>
          <input className="text-input" type="number" min={0} value={form.defaultDownloadLimit} onChange={(event) => set("defaultDownloadLimit", Number(event.target.value))} />
        </label>
        <label>
          <span className="field-label">{zhCN.adminPanel.defaultExpiry}</span>
          <select className="select-input" value={form.defaultExpiry} onChange={(event) => set("defaultExpiry", event.target.value)}>
            <option value="1h">{zhCN.expiryLabel("1h")}</option>
            <option value="24h">{zhCN.expiryLabel("24h")}</option>
            <option value="168h">{zhCN.expiryLabel("168h")}</option>
            <option value="720h">{zhCN.expiryLabel("720h")}</option>
          </select>
        </label>
        {cloud ? (
          <>
            <div className="settings-readonly-card">
              <span className="field-label">{zhCN.adminPanel.workerZipMode}</span>
              <strong>{zhCN.adminPanel.workerZipStreaming}</strong>
              <small>{zhCN.adminPanel.workerZipStreamingHelp}</small>
            </div>
            <label>
              <span className="field-label">{zhCN.adminPanel.r2BudgetStorage}</span>
              <input className="text-input" type="number" min={0.01} step={0.01} value={form.r2BudgetStorageGBMonth} onChange={(event) => set("r2BudgetStorageGBMonth", Number(event.target.value))} />
              <small>{zhCN.adminPanel.r2BudgetHelp}</small>
            </label>
            <label>
              <span className="field-label">{zhCN.adminPanel.r2BudgetClassA}</span>
              <input className="text-input" type="number" min={1} step={1} value={form.r2BudgetClassA} onChange={(event) => set("r2BudgetClassA", Number(event.target.value))} />
            </label>
            <label>
              <span className="field-label">{zhCN.adminPanel.r2BudgetClassB}</span>
              <input className="text-input" type="number" min={1} step={1} value={form.r2BudgetClassB} onChange={(event) => set("r2BudgetClassB", Number(event.target.value))} />
            </label>
          </>
        ) : (
          <label>
            <span className="field-label">{zhCN.adminPanel.asyncZipGB}</span>
            <input className="text-input" type="number" min={0.0625} step={0.0625} value={bytesToGB(form.zipAsyncThresholdBytes)} onChange={(event) => set("zipAsyncThresholdBytes", gbToBytes(event.target.value))} />
          </label>
        )}
      </section>
      <div className="settings-command-row">
        <button className="primary-button" type="button" disabled={busy} onClick={save}>
          <Save size={18} />
          {zhCN.adminPanel.saveSettings}
        </button>
        <button
          className="secondary-button"
          type="button"
          onClick={async () => {
            const result = await api.enableDiagnostics();
            await onSaved(zhCN.adminPanel.diagnosticsUntil(new Date(result.until).toLocaleString(zhCN.locale)));
          }}
        >
          <Activity size={18} />
          {zhCN.adminPanel.enableDiagnostics}
        </button>
        <button
          className="secondary-button"
          type="button"
          onClick={async () => {
            await api.runMaintenance();
            await onSaved(zhCN.adminPanel.maintenanceCompleted);
          }}
        >
          <Gauge size={18} />
          {zhCN.adminPanel.runMaintenance}
        </button>
      </div>
    </>
  );
}

function bytesToGB(bytes: number) {
  return Number((bytes / 1024 ** 3).toFixed(4));
}

function gbToBytes(value: string) {
  return Math.round(Number(value) * 1024 ** 3);
}
