import { CheckCircle2, Database, ExternalLink, Pencil, Plus, RotateCw, Save, Server, Trash2, X } from "lucide-react";
import { useMemo, useState } from "react";
import { api } from "../../api";
import { zhCN } from "../../i18n/zh-CN";
import type {
  StorageProfile,
  StorageProfileInput,
  StorageProviderDefinition,
} from "../../types";
import { StorageUsagePanel } from "./StorageUsagePanel";

const providerHints: Record<string, { region?: string; credentials: string; extra?: string }> = {
  "cloudflare-r2": {
    credentials: zhCN.storagePanel.providerHints.cloudflareR2Credentials,
    extra: zhCN.storagePanel.providerHints.cloudflareR2Extra,
  },
  "aws-s3": {
    region: zhCN.storagePanel.providerHints.awsRegion,
    credentials: zhCN.storagePanel.providerHints.awsCredentials,
  },
  "backblaze-b2": {
    region: zhCN.storagePanel.providerHints.b2Region,
    credentials: zhCN.storagePanel.providerHints.b2Credentials,
  },
  wasabi: {
    region: zhCN.storagePanel.providerHints.wasabiRegion,
    credentials: zhCN.storagePanel.providerHints.wasabiCredentials,
  },
  "digitalocean-spaces": {
    region: zhCN.storagePanel.providerHints.spacesRegion,
    credentials: zhCN.storagePanel.providerHints.spacesCredentials,
  },
  minio: {
    region: zhCN.storagePanel.providerHints.minioRegion,
    credentials: zhCN.storagePanel.providerHints.minioCredentials,
    extra: zhCN.storagePanel.providerHints.minioExtra,
  },
  "generic-s3": {
    region: zhCN.storagePanel.providerHints.genericRegion,
    credentials: zhCN.storagePanel.providerHints.genericCredentials,
    extra: zhCN.storagePanel.providerHints.genericExtra,
  },
};

function blankProfile(provider = "cloudflare-r2"): StorageProfileInput {
  return {
    name: provider === "cloudflare-r2" ? "Cloudflare R2" : "",
    provider,
    config: {
      prefix: "sharepipe",
      pathStyle: provider === "cloudflare-r2" || provider === "backblaze-b2" || provider === "minio" || provider === "generic-s3",
      serverSideEncryption: provider === "aws-s3" || provider === "wasabi" ? "AES256" : "none",
      jurisdiction: "",
      region: provider === "cloudflare-r2" ? "auto" : provider === "minio" ? "us-east-1" : "",
    },
    credentials: {},
  };
}

export function StoragePanel({
  profiles,
  providers,
  onChanged,
  onError,
  cloud = true,
}: {
  profiles: StorageProfile[];
  providers: StorageProviderDefinition[];
  onChanged: (message: string) => Promise<void>;
  onError: (message: string) => void;
  cloud?: boolean;
}) {
  const [editingID, setEditingID] = useState<string | null>(null);
  const [form, setForm] = useState<StorageProfileInput | null>(null);
  const [busy, setBusy] = useState(false);
  const [testResult, setTestResult] = useState("");
  const provider = useMemo(() => providers.find((item) => item.id === form?.provider), [providers, form?.provider]);
  const editingProfile = editingID ? profiles.find((item) => item.id === editingID) : undefined;
  const encryptionModes = form ? serverSideEncryptionOptions(form.provider) : [];
  const runtimeOnlyProviders = providers.filter(
    (item) => item.available === false && !profiles.some((profile) => profile.provider === item.id),
  );
  const firstCreatableProvider = providers.find((item) => item.available !== false && item.creatable !== false)?.id ?? "cloudflare-r2";

  const edit = (profile: StorageProfile) => {
    setEditingID(profile.id);
    setForm({ name: profile.name, provider: profile.provider, config: { ...profile.config }, credentials: {} });
    setTestResult("");
  };

  const closeEditor = () => {
    setEditingID(null);
    setForm(null);
    setTestResult("");
  };

  const save = async (event: React.FormEvent) => {
    event.preventDefault();
    if (!form) return;
    setBusy(true);
    try {
      if (editingID) await api.updateStorageProfile(editingID, form);
      else await api.createStorageProfile(form);
      closeEditor();
      await onChanged(editingID ? zhCN.storagePanel.updated : zhCN.storagePanel.created);
    } catch (error) {
      onError(error instanceof Error ? error.message : zhCN.storagePanel.saveFailed);
    } finally {
      setBusy(false);
    }
  };

  return (
    <>
      <header className="admin-heading">
        <div>
          <h1>{zhCN.storagePanel.title}</h1>
          <p className="heading-lead">{zhCN.storagePanel.lead}</p>
        </div>
        <button className="primary-button" type="button" disabled={!providers.some((item) => item.available !== false && item.creatable !== false)} onClick={() => { setEditingID(null); setForm(blankProfile(firstCreatableProvider)); }}>
          <Plus size={18} /> {zhCN.storagePanel.add}
        </button>
      </header>

      <section className="storage-profile-grid admin-section">
        {runtimeOnlyProviders.map((item) => (
          <article className="storage-profile-card" key={`capability-${item.id}`}>
            <div className="storage-card-icon"><Server size={21} /></div>
            <div className="storage-card-copy">
              <div className="storage-card-title"><strong>{item.label}</strong><span className="status-pill">{zhCN.storagePanel.cloudUnavailable}</span></div>
              <p>{item.unavailableReason ?? zhCN.storagePanel.runtimeUnavailable}</p>
              <small>{zhCN.storagePanel.localAvailable}</small>
            </div>
          </article>
        ))}
        {profiles.map((profile) => (
          <article className="storage-profile-card" key={profile.id}>
            <div className="storage-card-icon">{profile.kind === "local" ? <Server size={21} /> : <Database size={21} />}</div>
            <div className="storage-card-copy">
              <div className="storage-card-title">
                <strong>{profile.name}</strong>
                {profile.default && <span className="status-pill">{zhCN.common.currentDefault}</span>}
              </div>
              <p>{profile.kind === "local" ? profile.config.rootPath : `${profile.config.bucket} · ${profile.config.region}`}</p>
              <small>{providerLabel(providers, profile.provider)} · {zhCN.storagePanel.historyUses(profile.inUse)}</small>
              {profile.kind === "s3" && <small>{profile.managed ? zhCN.storagePanel.managedCredentials : profile.hasCredentials ? zhCN.storagePanel.savedCredentials(profile.hasSessionToken) : zhCN.storagePanel.missingCredentials}</small>}
              {profile.lastError && <small className="error-text">{zhCN.storagePanel.lastCheck(profile.lastError)}</small>}
            </div>
            <div className="storage-card-actions">
              <button className="ghost-button" type="button" disabled={busy} onClick={async () => {
                setBusy(true);
                try {
                  const health = await api.testStorageProfile(profile.id);
                  if (!health.healthy) throw new Error(health.error || zhCN.storagePanel.connectionFailed);
                  await onChanged(zhCN.storagePanel.connectionPassed(profile.name));
                } catch (error) {
                  onError(error instanceof Error ? error.message : zhCN.storagePanel.connectionFailed);
                } finally { setBusy(false); }
              }}><RotateCw size={16} /> {zhCN.storagePanel.check}</button>
              {!profile.default && <button className="ghost-button" type="button" disabled={busy} onClick={async () => {
                setBusy(true);
                try { await api.setDefaultStorage(profile.id); await onChanged(zhCN.storagePanel.defaultChanged(profile.name)); }
                catch (error) { onError(error instanceof Error ? error.message : zhCN.storagePanel.defaultChangeFailed); }
                finally { setBusy(false); }
              }}><CheckCircle2 size={16} /> {zhCN.storagePanel.setDefault}</button>}
              {!profile.managed && <button className="ghost-button" type="button" onClick={() => edit(profile)}><Pencil size={16} /> {zhCN.storagePanel.edit}</button>}
              {!profile.managed && profile.id !== "local" && !profile.default && profile.inUse === 0 && (
                <button className="ghost-button danger-button" type="button" onClick={async () => {
                  if (!window.confirm(zhCN.storagePanel.deleteConfirm(profile.name))) return;
                  try { await api.deleteStorageProfile(profile.id); await onChanged(zhCN.storagePanel.deleted); }
                  catch (error) { onError(error instanceof Error ? error.message : zhCN.storagePanel.deleteFailed); }
                }}><Trash2 size={16} /> {zhCN.storagePanel.delete}</button>
              )}
            </div>
          </article>
        ))}
      </section>

      <StorageUsagePanel profile={profiles.find((profile) => profile.default)} cloud={cloud} />

      {form && (
        <section className="storage-editor admin-section">
          <div className="section-title-row">
            <div><h2>{editingID ? zhCN.storagePanel.editTitle : zhCN.storagePanel.addTitle}</h2><p>{zhCN.storagePanel.secretHelp}</p></div>
            <button className="icon-button" type="button" aria-label={zhCN.storagePanel.close} onClick={closeEditor}><X size={19} /></button>
          </div>
          <form onSubmit={save}>
            <div className="storage-form-grid">
              <Field label={zhCN.storagePanel.displayName}><input className="text-input" value={form.name} onChange={(event) => setForm({ ...form, name: event.target.value })} /></Field>
              <Field label={zhCN.storagePanel.service}>
                <select className="select-input" value={form.provider} disabled={Boolean(editingID && (editingID === "local" || editingProfile?.inUse))} onChange={(event) => setForm(blankProfile(event.target.value))}>
                  {providers.map((item) => <option value={item.id} key={item.id} disabled={(item.available === false || item.creatable === false) && item.id !== editingProfile?.provider}>{item.label}{item.available === false ? zhCN.storagePanel.unavailableSuffix : item.creatable === false ? zhCN.storagePanel.managedSuffix : ""}</option>)}
                </select>
                {editingID === "local" ? <small>{zhCN.storagePanel.localProfileHelp}</small> : editingID && editingProfile?.inUse ? <small>{zhCN.storagePanel.inUseHelp}</small> : null}
              </Field>
              {provider?.kind === "local" ? (
                <Field label={form.provider === "nas" ? zhCN.storagePanel.mountPath : zhCN.storagePanel.rootPath} wide>
                  <input className="text-input" value={form.config.rootPath ?? ""} placeholder={form.provider === "nas" ? zhCN.storagePanel.nasPathPlaceholder : zhCN.storagePanel.localPathPlaceholder} onChange={(event) => updateConfig(form, setForm, "rootPath", event.target.value)} />
                  <small>{form.provider === "nas" ? zhCN.storagePanel.nasHelp : zhCN.storagePanel.localHelp}</small>
                </Field>
              ) : (
                <>
                  {form.provider === "cloudflare-r2" && <>
                    <Field label="Cloudflare Account ID"><input className="text-input" value={form.config.accountId ?? ""} onChange={(event) => updateConfig(form, setForm, "accountId", event.target.value)} /><small>{providerHints[form.provider]?.extra}</small></Field>
                    <Field label={zhCN.storagePanel.jurisdiction}><select className="select-input" value={form.config.jurisdiction ?? ""} onChange={(event) => updateConfig(form, setForm, "jurisdiction", event.target.value)}><option value="">{zhCN.storagePanel.defaultJurisdiction}</option><option value="eu">EU</option><option value="fedramp">FedRAMP</option></select></Field>
                  </>}
                  {(form.provider === "minio" || form.provider === "generic-s3") && <Field label="S3 Endpoint" wide><input className="text-input" type="url" placeholder="https://s3.example.com" value={form.config.endpoint ?? ""} onChange={(event) => updateConfig(form, setForm, "endpoint", event.target.value)} /><small>{providerHints[form.provider]?.extra}</small></Field>}
                  {form.provider !== "cloudflare-r2" && <Field label="Region"><input className="text-input" value={form.config.region ?? ""} placeholder="ap-southeast-1" onChange={(event) => updateConfig(form, setForm, "region", event.target.value)} /><small>{providerHints[form.provider]?.region}</small></Field>}
                  <Field label="Bucket"><input className="text-input" value={form.config.bucket ?? ""} onChange={(event) => updateConfig(form, setForm, "bucket", event.target.value)} /></Field>
                  <Field label={zhCN.storagePanel.prefix}><input className="text-input" value={form.config.prefix ?? ""} placeholder="sharepipe" onChange={(event) => updateConfig(form, setForm, "prefix", event.target.value)} /><small>{zhCN.storagePanel.prefixHelp}</small></Field>
                  <Field label={zhCN.storagePanel.encryption}><select className="select-input" value={form.config.serverSideEncryption ?? "none"} onChange={(event) => updateConfig(form, setForm, "serverSideEncryption", event.target.value as "none" | "AES256" | "aws:kms")}>{encryptionModes.map((mode) => <option value={mode} key={mode}>{serverSideEncryptionLabel(mode)}</option>)}</select><small>{serverSideEncryptionHelp(form.provider)}</small></Field>
                  {(form.provider === "minio" || form.provider === "generic-s3") && <label className="checkbox-field"><input type="checkbox" checked={Boolean(form.config.pathStyle)} onChange={(event) => updateConfig(form, setForm, "pathStyle", event.target.checked)} /><span>{zhCN.storagePanel.pathStyle}</span></label>}
                  <Field label="Access Key ID"><input className="text-input" autoComplete="off" value={form.credentials.accessKeyId ?? ""} placeholder={editingID ? zhCN.storagePanel.keepSecret : ""} onChange={(event) => setForm({ ...form, credentials: { ...form.credentials, accessKeyId: event.target.value } })} /></Field>
                  <Field label="Secret Access Key"><input className="text-input" type="password" autoComplete="new-password" value={form.credentials.secretAccessKey ?? ""} placeholder={editingID ? zhCN.storagePanel.keepSecret : ""} onChange={(event) => setForm({ ...form, credentials: { ...form.credentials, secretAccessKey: event.target.value } })} /><small>{providerHints[form.provider]?.credentials}</small></Field>
                  {(form.provider === "aws-s3" || form.provider === "generic-s3") && <Field label={zhCN.storagePanel.sessionToken}><input className="text-input" type="password" autoComplete="off" value={form.credentials.sessionToken ?? ""} placeholder={editingID ? zhCN.storagePanel.keepSecret : ""} onChange={(event) => setForm({ ...form, credentials: { ...form.credentials, sessionToken: event.target.value, clearSessionToken: event.target.value ? false : form.credentials.clearSessionToken } })} /></Field>}
                  {editingID && (form.provider === "aws-s3" || form.provider === "generic-s3") && <label className="checkbox-field"><input type="checkbox" checked={Boolean(form.credentials.clearSessionToken)} disabled={Boolean(form.credentials.sessionToken) || !editingProfile?.hasSessionToken} onChange={(event) => setForm({ ...form, credentials: { ...form.credentials, clearSessionToken: event.target.checked } })} /><span>{editingProfile?.hasSessionToken ? zhCN.storagePanel.clearSession : zhCN.storagePanel.noSession}</span></label>}
                </>
              )}
            </div>
            {provider?.kind === "s3" && <CorsHelp />}
            <div className="settings-command-row">
              {!editingID && <button className="secondary-button" type="button" disabled={busy} onClick={async () => {
                setBusy(true); setTestResult("");
                try { const health = await api.testStorageInput(form); if (!health.healthy) throw new Error(health.error || zhCN.storagePanel.connectionFailed); setTestResult(zhCN.storagePanel.connected); }
                catch (error) { onError(error instanceof Error ? error.message : zhCN.storagePanel.connectionFailed); }
                finally { setBusy(false); }
              }}><RotateCw size={17} /> {zhCN.storagePanel.savePreflight}</button>}
              <button className="primary-button" type="submit" disabled={busy}><Save size={17} /> {zhCN.storagePanel.save}</button>
              {testResult && <span className="inline-success"><CheckCircle2 size={16} /> {testResult}</span>}
            </div>
            {provider && <p className="provider-reference">{zhCN.storagePanel.compatibility}<a href={provider.documentationUrl} target="_blank" rel="noreferrer">{provider.label} {zhCN.storagePanel.officialDocs} <ExternalLink size={13} /></a> · {validationLabel(provider.validation)}</p>}
          </form>
        </section>
      )}
    </>
  );
}

function Field({ label, wide = false, children }: { label: string; wide?: boolean; children: React.ReactNode }) {
  return <label className={wide ? "field-wide" : ""}><span className="field-label">{label}</span>{children}</label>;
}

function updateConfig<K extends keyof StorageProfileInput["config"]>(form: StorageProfileInput, setForm: (value: StorageProfileInput) => void, key: K, value: StorageProfileInput["config"][K]) {
  setForm({ ...form, config: { ...form.config, [key]: value } });
}

function providerLabel(providers: StorageProviderDefinition[], id: string) {
  return providers.find((item) => item.id === id)?.label ?? id;
}

function validationLabel(value: StorageProviderDefinition["validation"]) {
  if (value === "live-tested") return zhCN.storagePanel.validationLive;
  if (value === "built-in") return zhCN.storagePanel.validationBuiltIn;
  if (value === "mounted-filesystem") return zhCN.storagePanel.validationMounted;
  if (value === "official-spec") return zhCN.storagePanel.validationOfficial;
  return zhCN.storagePanel.validationContract;
}

function serverSideEncryptionOptions(provider: string): Array<"none" | "AES256" | "aws:kms"> {
  if (provider === "cloudflare-r2" || provider === "digitalocean-spaces") return ["none"];
  if (provider === "backblaze-b2" || provider === "wasabi") return ["none", "AES256"];
  return ["none", "AES256", "aws:kms"];
}

function serverSideEncryptionLabel(mode: "none" | "AES256" | "aws:kms") {
  if (mode === "AES256") return zhCN.storagePanel.encryptionAES;
  if (mode === "aws:kms") return "SSE-KMS (aws:kms)";
  return zhCN.storagePanel.encryptionDefault;
}

function serverSideEncryptionHelp(provider: string) {
  if (provider === "cloudflare-r2") return zhCN.storagePanel.r2EncryptionHelp;
  if (provider === "digitalocean-spaces") return zhCN.storagePanel.spacesEncryptionHelp;
  if (provider === "minio") return zhCN.storagePanel.minioEncryptionHelp;
  return zhCN.storagePanel.encryptionHelp;
}

function CorsHelp() {
  return <details className="cors-help"><summary>{zhCN.storagePanel.corsTitle}</summary><p>{zhCN.storagePanel.corsHelp}</p><pre>{`AllowedOrigins: ["${window.location.origin}"]\nAllowedMethods: ["PUT", "GET", "HEAD"]\nAllowedHeaders: ["*"]\nExposeHeaders: ["ETag"]`}</pre></details>;
}
