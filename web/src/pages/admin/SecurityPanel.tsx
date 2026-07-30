import { KeyRound, Mail, Plus, ShieldCheck, Trash2 } from "lucide-react";
import { useState } from "react";
import { api } from "../../api";
import type { AdministratorSecurity } from "../../types";
import { zhCN } from "../../i18n/zh-CN";

export function SecurityPanel({
  value,
  onChanged,
  onError,
}: {
  value: AdministratorSecurity;
  onChanged: (message: string) => Promise<void>;
  onError: (message: string) => void;
}) {
  const [email, setEmail] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [busy, setBusy] = useState(false);

  const addEmail = async (event: React.FormEvent) => {
    event.preventDefault();
    setBusy(true);
    try {
      await api.addAdminEmail(email);
      setEmail("");
      await onChanged(zhCN.securityPanel.emailAdded);
    } catch (error) {
      onError(error instanceof Error ? error.message : zhCN.securityPanel.emailAddFailed);
    } finally {
      setBusy(false);
    }
  };

  const changePassword = async (event: React.FormEvent) => {
    event.preventDefault();
    if (newPassword !== confirmPassword) {
      onError(zhCN.securityPanel.passwordMismatch);
      return;
    }
    setBusy(true);
    try {
      await api.changeAdminPassword("", newPassword);
      setNewPassword("");
      setConfirmPassword("");
      await onChanged(zhCN.securityPanel.passwordChanged);
    } catch (error) {
      onError(error instanceof Error ? error.message : zhCN.securityPanel.passwordChangeFailed);
    } finally {
      setBusy(false);
    }
  };

  return (
    <>
      <header className="admin-heading">
        <div>
          <h1>{zhCN.securityPanel.title}</h1>
          <p className="heading-lead">{zhCN.securityPanel.lead}</p>
        </div>
      </header>

      <section className="security-mode-card admin-section">
        <ShieldCheck size={24} />
        <div>
          <strong>{zhCN.securityPanel.localMode}</strong>
          <p>{zhCN.securityPanel.localModeHelp}</p>
        </div>
      </section>

      <section className="admin-section settings-block">
        <div className="section-title-row">
          <div>
            <h2>{zhCN.securityPanel.emails}</h2>
            <p>{zhCN.securityPanel.emailsHelp}</p>
          </div>
        </div>
        <div className="email-allowlist">
          {value.emails.map((item) => (
            <div key={item}>
              <Mail size={18} />
              <span>{item}</span>
              <button
                className="icon-danger-button"
                type="button"
                disabled={value.emails.length <= 1 || busy}
                aria-label={zhCN.securityPanel.deleteEmailAria(item)}
                onClick={async () => {
                  if (!window.confirm(zhCN.securityPanel.deleteEmailConfirm(item))) return;
                  setBusy(true);
                  try {
                    await api.removeAdminEmail(item);
                    await onChanged(zhCN.securityPanel.emailDeleted);
                  } catch (error) {
                    onError(error instanceof Error ? error.message : zhCN.securityPanel.emailDeleteFailed);
                  } finally {
                    setBusy(false);
                  }
                }}
              >
                <Trash2 size={17} />
              </button>
            </div>
          ))}
        </div>
        <form className="inline-admin-form" onSubmit={addEmail}>
          <label>
            <span className="field-label">{zhCN.securityPanel.addEmail}</span>
            <input
              className="text-input"
              type="email"
              value={email}
              placeholder="admin@example.com"
              onChange={(event) => setEmail(event.target.value)}
            />
          </label>
          <button className="secondary-button" type="submit" disabled={busy || !email.trim()}>
            <Plus size={17} /> {zhCN.securityPanel.add}
          </button>
        </form>
      </section>

      <section className="admin-section settings-block">
        <h2>{zhCN.securityPanel.passwordTitle}</h2>
        <p className="section-help">
          {zhCN.securityPanel.passwordHelp}
        </p>
        <form className="password-change-form" onSubmit={changePassword}>
          <label>
            <span className="field-label">{zhCN.securityPanel.newPassword}</span>
            <input className="text-input" type="password" minLength={12} maxLength={256} autoComplete="new-password" value={newPassword} onChange={(event) => setNewPassword(event.target.value)} />
            <small>{zhCN.securityPanel.passwordRule}</small>
          </label>
          <label>
            <span className="field-label">{zhCN.securityPanel.confirmPassword}</span>
            <input className="text-input" type="password" minLength={12} maxLength={256} autoComplete="new-password" value={confirmPassword} onChange={(event) => setConfirmPassword(event.target.value)} />
          </label>
          <button className="primary-button" type="submit" disabled={busy || newPassword.length < 12 || newPassword !== confirmPassword}>
            <KeyRound size={17} /> {zhCN.securityPanel.setPassword}
          </button>
        </form>
      </section>
    </>
  );
}
