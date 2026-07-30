import {
  Database,
  Download,
  Folder,
  Home,
  KeyRound,
  LogOut,
  Plus,
  Settings,
  Shield,
  ServerCog,
  Ticket,
  Trash2,
  MonitorSmartphone,
} from "lucide-react";
import { useEffect, useState } from "react";
import { APIError, api } from "../api";
import { Brand } from "../components/AppHeader";
import { zhCN } from "../i18n/zh-CN";
import type { AdminSession, AdministratorSecurity, Invite, Overview, RuntimeSettings, Share, StorageProfile, StorageProviderDefinition } from "../types";
import { formatBytes, remainingTime } from "../utils";
import { SecurityPanel } from "./admin/SecurityPanel";
import { SettingsPanel } from "./admin/SettingsPanel";
import { StoragePanel } from "./admin/StoragePanel";
import { TrustedDevicesPanel } from "./admin/TrustedDevicesPanel";
import "./AdminPage.css";

type AdminTab = "overview" | "shares" | "invites" | "devices" | "storage" | "settings" | "security";

export function AdminPage() {
  const [authenticated, setAuthenticated] = useState<boolean | null>(null);
  const [session, setSession] = useState<AdminSession | null>(null);
  const [password, setPassword] = useState("");
  const [tab, setTab] = useState<AdminTab>("overview");
  const [overview, setOverview] = useState<Overview | null>(null);
  const [shares, setShares] = useState<Share[]>([]);
  const [invites, setInvites] = useState<Invite[]>([]);
  const [settings, setSettings] = useState<RuntimeSettings | null>(null);
  const [security, setSecurity] = useState<AdministratorSecurity | null>(null);
  const [storageProfiles, setStorageProfiles] = useState<StorageProfile[]>([]);
  const [storageProviders, setStorageProviders] = useState<StorageProviderDefinition[]>([]);
  const [problem, setProblem] = useState("");
  const [notice, setNotice] = useState("");
  const [busy, setBusy] = useState(false);

  const load = async () => {
    try {
      const sessionData = await api.adminSession();
      setSession(sessionData);
      if (!sessionData.authenticated) {
        setAuthenticated(false);
        return;
      }
      if (!sessionData.local && tab === "security") {
        setTab("overview");
      }
      const [overviewData, shareData, inviteData, settingsData, securityData, profilesData, providersData] = await Promise.all([
        api.adminOverview(),
        api.adminShares(),
        api.adminInvites(),
        api.adminSettings(),
        sessionData.local ? api.adminSecurity() : Promise.resolve(null),
        api.storageProfiles(),
        api.storageProviders(),
      ]);
      setAuthenticated(true);
      setOverview(overviewData);
      setShares(shareData.items);
      setInvites(inviteData.items);
      setSettings(settingsData.limits);
      setSecurity(securityData);
      setStorageProfiles(profilesData.items);
      setStorageProviders(providersData.items);
      setProblem("");
    } catch (error) {
      if (error instanceof APIError && error.status === 401) {
        setAuthenticated(false);
      } else {
        setProblem(error instanceof Error ? error.message : zhCN.adminPanel.loadFailed);
      }
    }
  };

  useEffect(() => {
    void load();
  }, []);

  const login = async (event: React.FormEvent) => {
    event.preventDefault();
    setBusy(true);
    setProblem("");
    try {
      await api.adminLogin(password);
      setPassword("");
      await load();
    } catch (error) {
      setProblem(error instanceof Error ? error.message : zhCN.adminPanel.loginFailed);
    } finally {
      setBusy(false);
    }
  };

  if (authenticated === null) {
    return <div className="page-state">{zhCN.adminPanel.connecting}</div>;
  }
  if (!authenticated) {
    return (
      <main className="admin-login">
        <Brand />
        <div className="admin-login-panel">
          <KeyRound size={38} strokeWidth={1.5} />
          <h1>{zhCN.adminPanel.loginTitle}</h1>
          <p>{zhCN.adminPanel.loginLead}</p>
          {session?.email && <p className="admin-identity-hint">{zhCN.adminPanel.cloudflareIdentity(session.email)}</p>}
          {session?.passwordConfigured ? (
            <form onSubmit={login}>
              <input
                className="text-input"
                type="password"
                autoFocus
                value={password}
                placeholder={zhCN.adminPanel.passwordPlaceholder}
                onChange={(event) => setPassword(event.target.value)}
              />
              {problem && <p className="error-text">{problem}</p>}
              <button className="primary-button" type="submit" disabled={busy || !password}>
                {busy ? zhCN.adminPanel.loggingIn : zhCN.adminPanel.login}
              </button>
            </form>
          ) : (
            <p className="admin-login-note">{zhCN.adminPanel.passwordNotConfigured}</p>
          )}
          <div className="admin-login-divider"><span>{zhCN.adminPanel.loginOr}</span></div>
          <a className="secondary-button admin-email-login" href="/admin/email-login">
            {zhCN.adminPanel.emailLogin}
          </a>
        </div>
      </main>
    );
  }

  return (
    <div className="admin-layout">
      <aside className="admin-sidebar">
        <Brand />
        <p>{zhCN.admin}</p>
        <nav>
          <NavButton icon={Home} label={zhCN.overview} active={tab === "overview"} onClick={() => setTab("overview")} />
          <NavButton icon={Folder} label={zhCN.shares} active={tab === "shares"} onClick={() => setTab("shares")} />
          <NavButton icon={Ticket} label={zhCN.invites} active={tab === "invites"} onClick={() => setTab("invites")} />
          <NavButton icon={MonitorSmartphone} label={zhCN.adminPanel.trustedDevices} active={tab === "devices"} onClick={() => setTab("devices")} />
          <NavButton icon={ServerCog} label={zhCN.adminPanel.storageConnections} active={tab === "storage"} onClick={() => setTab("storage")} />
          <NavButton icon={Settings} label={zhCN.settings} active={tab === "settings"} onClick={() => setTab("settings")} />
          {session?.local && (
            <NavButton icon={Shield} label={zhCN.adminPanel.advancedSecurity} active={tab === "security"} onClick={() => setTab("security")} />
          )}
        </nav>
        {!session?.local && <button
          className="admin-logout"
          type="button"
          onClick={async () => {
            await api.adminLogout();
            setAuthenticated(false);
          }}
        >
          <LogOut size={18} />
          {zhCN.logout}
        </button>}
      </aside>
      <main className="admin-main">
        {problem && <p className="admin-alert error-text">{problem}</p>}
        {notice && <p className="admin-alert admin-success">{notice}</p>}
        {tab === "overview" && overview && (
          <OverviewPanel overview={overview} shares={shares} profiles={storageProfiles} onCreateInvite={() => setTab("invites")} />
        )}
        {tab === "shares" && (
          <SharesPanel
            shares={shares}
            profiles={storageProfiles}
            onRevoke={async (share) => {
              if (!window.confirm(zhCN.adminPanel.revokeShareConfirm(share.name))) return;
              try {
                await api.revokeAdminShare(share.id);
                setNotice(zhCN.adminPanel.shareRevoked);
                await load();
              } catch (error) {
                setProblem(error instanceof Error ? error.message : zhCN.adminPanel.revokeShareFailed);
              }
            }}
          />
        )}
        {tab === "invites" && (
          <InvitesPanel
            invites={invites}
            onChanged={async (message) => {
              setNotice(message);
              await load();
            }}
            onError={setProblem}
          />
        )}
        {tab === "storage" && (
          <StoragePanel profiles={storageProfiles} providers={storageProviders} cloud={!session?.local} onChanged={async (message) => { setNotice(message); await load(); }} onError={setProblem} />
        )}
        {tab === "devices" && <TrustedDevicesPanel localAdmin={Boolean(session?.local)} onNotice={setNotice} onError={setProblem} />}
        {tab === "settings" && settings && overview && (
          <SettingsPanel
            value={settings}
            cloud={!session?.local}
            onSaved={async (message) => {
              setNotice(message);
              await load();
            }}
            onError={setProblem}
          />
        )}
        {session?.local && tab === "security" && security && (
          <SecurityPanel value={security} onChanged={async (message) => { setNotice(message); await load(); }} onError={setProblem} />
        )}
      </main>
    </div>
  );
}

function NavButton({
  icon: Icon,
  label,
  active,
  onClick,
}: {
  icon: typeof Home;
  label: string;
  active: boolean;
  onClick: () => void;
}) {
  return (
    <button className={active ? "is-active" : ""} type="button" onClick={onClick}>
      <Icon size={20} strokeWidth={1.7} />
      {label}
    </button>
  );
}

function OverviewPanel({
  overview,
  shares,
  profiles,
  onCreateInvite,
}: {
  overview: Overview;
  shares: Share[];
  profiles: StorageProfile[];
  onCreateInvite: () => void;
}) {
  return (
    <>
      <header className="admin-heading">
        <h1>{zhCN.overview}</h1>
        <button className="primary-button" type="button" onClick={onCreateInvite}>
          <Plus size={18} />
          {zhCN.createInvite}
        </button>
      </header>
      <section className="metric-strip">
        <Metric label={zhCN.adminPanel.activeShares} value={String(overview.activeShares)} icon={Folder} />
        <Metric label={zhCN.adminPanel.storageUsed} value={formatBytes(overview.storageBytes)} icon={Database} />
        <Metric label={zhCN.adminPanel.downloadsToday} value={String(overview.downloadsToday)} icon={Download} />
        <Metric label={zhCN.adminPanel.pendingCleanup} value={String(overview.pendingCleanup)} icon={Trash2} />
      </section>
      <section className="admin-section">
        <h2>{zhCN.adminPanel.storageStatus}</h2>
        <div className="storage-status">
          {overview.storage.map((item) => (
            <div key={item.name}>
              <Database size={20} />
              <span>{item.name}</span>
              <strong className={item.healthy ? "healthy" : "unhealthy"}>
                <i /> {item.healthy ? zhCN.common.healthy : zhCN.common.unhealthy}
              </strong>
              {item.default && <small>{zhCN.common.currentDefault}</small>}
            </div>
          ))}
        </div>
      </section>
      <section className="admin-section">
        <h2>{zhCN.adminPanel.recentShares}</h2>
        <ShareTable shares={shares.slice(0, 10)} profiles={profiles} />
      </section>
    </>
  );
}

function Metric({
  label,
  value,
  icon: Icon,
}: {
  label: string;
  value: string;
  icon: typeof Home;
}) {
  return (
    <div>
      <span>{label}</span>
      <strong>{value}</strong>
      <Icon size={26} strokeWidth={1.55} />
    </div>
  );
}

function SharesPanel({ shares, profiles, onRevoke }: { shares: Share[]; profiles: StorageProfile[]; onRevoke: (share: Share) => void }) {
  return (
    <>
      <header className="admin-heading">
        <h1>{zhCN.shares}</h1>
      </header>
      <section className="admin-section">
        <ShareTable shares={shares} profiles={profiles} onRevoke={onRevoke} />
      </section>
    </>
  );
}

function ShareTable({ shares, profiles, onRevoke }: { shares: Share[]; profiles: StorageProfile[]; onRevoke?: (share: Share) => void }) {
  return (
    <div className="admin-table-wrap">
      <table className="admin-table">
        <thead>
          <tr>
            <th>{zhCN.adminPanel.columnName}</th>
            <th>{zhCN.adminPanel.columnStorage}</th>
            <th>{zhCN.adminPanel.columnSize}</th>
            <th>{zhCN.adminPanel.columnExpiry}</th>
            <th>{zhCN.adminPanel.columnDownloads}</th>
            <th>{zhCN.adminPanel.columnStatus}</th>
            {onRevoke && <th>{zhCN.adminPanel.columnActions}</th>}
          </tr>
        </thead>
        <tbody>
          {shares.map((share) => (
            <tr key={share.id}>
              <td>{share.name}</td>
              <td>{profiles.find((profile) => profile.id === share.storageProfileId)?.name ?? (share.storageBackend === "local" ? zhCN.common.local : zhCN.common.s3)}</td>
              <td>{formatBytes(share.totalBytes)}</td>
              <td>{remainingTime(share.expiresAt)}</td>
              <td>
                {share.downloadCount} / {share.downloadLimit ?? zhCN.common.infinity}
              </td>
              <td>
                <span className={"table-status status-" + share.status}>{zhCN.adminPanel.shareStatus(share.status)}</span>
              </td>
              {onRevoke && (
                <td>
                  {["uploading", "active", "expired", "cleanup_pending"].includes(share.status) ? (
                    <button
                      aria-label={zhCN.adminPanel.revokeNamed(share.name)}
                      className="table-action"
                      type="button"
                      onClick={() => onRevoke(share)}
                    >
                      {zhCN.adminPanel.revoke}
                    </button>
                  ) : (
                    <span className="table-action-unavailable">{zhCN.common.emDash}</span>
                  )}
                </td>
              )}
            </tr>
          ))}
          {shares.length === 0 && (
            <tr>
              <td colSpan={onRevoke ? 7 : 6} className="table-empty">
                {zhCN.adminPanel.noShares}
              </td>
            </tr>
          )}
        </tbody>
      </table>
    </div>
  );
}

export function InvitesPanel({
  invites,
  onChanged,
  onError,
}: {
  invites: Invite[];
  onChanged: (message: string) => Promise<void>;
  onError: (message: string) => void;
}) {
  const [label, setLabel] = useState("");
  const [maxUses, setMaxUses] = useState(10);
  const [expiresIn, setExpiresIn] = useState("168h");
  const [createdCode, setCreatedCode] = useState("");
  const [busy, setBusy] = useState(false);
  const [kind, setKind] = useState<"standard" | "trusted_device">("standard");
  const [deviceName, setDeviceName] = useState("");

  const create = async (event: React.FormEvent) => {
    event.preventDefault();
    setBusy(true);
    try {
      const inviteLabel = kind === "trusted_device" && !label.trim() ? deviceName.trim() : label.trim();
      const invite = await api.createInvite({ label: inviteLabel, maxUses: kind === "trusted_device" ? 1 : maxUses, expiresIn, kind, deviceName });
      setCreatedCode(invite.code ?? "");
      setLabel("");
      setDeviceName("");
      await onChanged(zhCN.adminPanel.inviteCreated);
    } catch (error) {
      onError(error instanceof Error ? error.message : zhCN.adminPanel.createInviteFailed);
    } finally {
      setBusy(false);
    }
  };

  return (
    <>
      <header className="admin-heading">
        <h1>{zhCN.invites}</h1>
      </header>
      <section className="invite-create admin-section">
        <h2>{zhCN.createInvite}</h2>
        <form onSubmit={create}>
          <select className="select-input" aria-label={zhCN.adminPanel.inviteKindAria} value={kind} onChange={(event) => setKind(event.target.value as "standard" | "trusted_device")}><option value="standard">{zhCN.adminPanel.standardInvite}</option><option value="trusted_device">{zhCN.adminPanel.trustedInvite}</option></select>
          {kind === "trusted_device" ? <input aria-label={zhCN.adminPanel.deviceNameAria} className="text-input" required value={deviceName} placeholder={zhCN.adminPanel.deviceNamePlaceholder} onChange={(event) => setDeviceName(event.target.value)} /> : <input
            aria-label={zhCN.adminPanel.inviteLabelAria}
            className="text-input"
            value={label}
            placeholder={zhCN.adminPanel.inviteLabelPlaceholder}
            onChange={(event) => setLabel(event.target.value)}
          />}
          <input
            aria-label={zhCN.adminPanel.inviteUsesAria}
            className="text-input"
            type="number"
            min={1}
            value={kind === "trusted_device" ? 1 : maxUses}
            disabled={kind === "trusted_device"}
            title={kind === "trusted_device" ? zhCN.adminPanel.trustedInviteOnce : undefined}
            onChange={(event) => setMaxUses(Number(event.target.value))}
          />
          <select
            aria-label={zhCN.adminPanel.inviteExpiryAria}
            className="select-input"
            value={expiresIn}
            onChange={(event) => setExpiresIn(event.target.value)}
          >
            <option value="24h">{zhCN.expiryLabel("24h")}</option>
            <option value="168h">{zhCN.expiryLabel("168h")}</option>
            <option value="720h">{zhCN.expiryLabel("720h")}</option>
          </select>
          <button className="primary-button invite-create-button" type="submit" disabled={busy}>
            <Plus size={18} />
            {zhCN.adminPanel.create}
          </button>
        </form>
        {createdCode && (
          <div className="created-invite">
            <code>{createdCode}</code>
            <button aria-label={zhCN.adminPanel.copyInviteAria} type="button" onClick={() => navigator.clipboard.writeText(createdCode)}>
              {zhCN.adminPanel.copy}
            </button>
          </div>
        )}
      </section>
      <section className="admin-section">
        <h2>{zhCN.adminPanel.inviteList}</h2>
        <div className="invite-list">
          {invites.map((invite) => (
            <div key={invite.id}>
              <Ticket size={19} />
              <span>
                <strong>{invite.label || invite.deviceName || zhCN.adminPanel.unnamedInvite}</strong>
                <small>
                  {invite.kind === "trusted_device" ? zhCN.adminPanel.trustedInviteSummary(invite.deviceName) : zhCN.adminPanel.standardInviteSummary}
                  {zhCN.adminPanel.inviteUsage(invite.uses, invite.maxUses ?? zhCN.common.infinity)} ·{" "}
                  {invite.revokedAt
                    ? zhCN.common.revoked
                    : invite.expiresAt
                      ? zhCN.adminPanel.inviteExpiredAt(new Date(invite.expiresAt).toLocaleString(zhCN.locale))
                      : zhCN.adminPanel.longLived}
                </small>
              </span>
              {!invite.revokedAt && (
                <button
                  className="ghost-button"
                  type="button"
                  onClick={async () => {
                    if (!window.confirm(zhCN.adminPanel.revokeInviteConfirm)) return;
                    await api.revokeInvite(invite.id);
                    await onChanged(zhCN.adminPanel.inviteRevoked);
                  }}
                >
                  {zhCN.adminPanel.revoke}
                </button>
              )}
            </div>
          ))}
        </div>
      </section>
    </>
  );
}
