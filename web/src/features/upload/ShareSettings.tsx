import { Eye, EyeOff } from "lucide-react";
import { useState } from "react";
import { zhCN } from "../../i18n/zh-CN";
import { formatExpiry } from "../../utils";
import type { PublicConfig, UploadAuthorization } from "../../types";

export interface ShareForm {
  inviteCode: string;
  name: string;
  description: string;
  expiresIn: string;
  downloadLimit: number;
  password: string;
  pairingEnabled: boolean;
  qrEnabled: boolean;
}

interface ShareSettingsProps {
  value: ShareForm;
  config: PublicConfig;
  disabled: boolean;
  onChange: (next: ShareForm) => void;
  authorization: UploadAuthorization | null;
}

export function ShareSettings({ value, config, disabled, onChange, authorization }: ShareSettingsProps) {
  const [showPassword, setShowPassword] = useState(false);
  const set = <K extends keyof ShareForm>(key: K, next: ShareForm[K]) =>
    onChange({ ...value, [key]: next });

  return (
    <section className="share-settings" aria-labelledby="settings-heading">
      <h2 id="settings-heading">{zhCN.shareSettings}</h2>
      {authorization?.trusted ? (
        <div className="trusted-device-status"><strong>{zhCN.uploadSettings.trustedDevice}</strong><span>{authorization.deviceName}</span></div>
      ) : (
        <label>
          <span className="field-label">{zhCN.inviteCode}</span>
          <input className="text-input" value={value.inviteCode} disabled={disabled} autoComplete="one-time-code" placeholder={zhCN.uploadSettings.invitePlaceholder} onChange={(event) => set("inviteCode", event.target.value)} />
        </label>
      )}
      <label>
        <span className="field-label">{zhCN.shareName}</span>
        <input
          className="text-input"
          value={value.name}
          disabled={disabled}
          placeholder={zhCN.uploadSettings.namePlaceholder}
          onChange={(event) => set("name", event.target.value)}
        />
      </label>
      <div className="share-option-list">
        <label className="checkbox-row"><input type="checkbox" checked={value.pairingEnabled} disabled={disabled} onChange={(event) => set("pairingEnabled", event.target.checked)} /><span><strong>{zhCN.uploadSettings.pairing}</strong><small>{zhCN.uploadSettings.pairingHelp}</small></span></label>
        <label className="checkbox-row"><input type="checkbox" checked={value.qrEnabled} disabled={disabled} onChange={(event) => set("qrEnabled", event.target.checked)} /><span><strong>{zhCN.uploadSettings.qr}</strong><small>{zhCN.uploadSettings.qrHelp}</small></span></label>
      </div>
      <label>
        <span className="field-label">{zhCN.description}</span>
        <textarea
          className="textarea"
          value={value.description}
          disabled={disabled}
          placeholder={zhCN.uploadSettings.descriptionPlaceholder}
          onChange={(event) => set("description", event.target.value)}
        />
      </label>
      <label>
        <span className="field-label">{zhCN.expiry}</span>
        <select
          className="select-input"
          value={value.expiresIn}
          disabled={disabled}
          onChange={(event) => set("expiresIn", event.target.value)}
        >
          {config.expiryPresets.map((item) => (
            <option key={item} value={item}>
              {formatExpiry(item)}
            </option>
          ))}
        </select>
      </label>
      <label>
        <span className="field-label">{zhCN.downloadLimit}</span>
        <select
          className="select-input"
          value={value.downloadLimit}
          disabled={disabled}
          onChange={(event) => set("downloadLimit", Number(event.target.value))}
        >
          {[1, 5, 10, 20, 50].map((count) => (
            <option key={count} value={count}>
              {zhCN.common.countTimes(count)}
            </option>
          ))}
          <option value={0}>{zhCN.common.unlimited}</option>
        </select>
      </label>
      <label>
        <span className="field-label">{zhCN.password}</span>
        <span className="password-field">
          <input
            className="text-input"
            type={showPassword ? "text" : "password"}
            value={value.password}
            disabled={disabled}
            minLength={8}
            autoComplete="new-password"
            placeholder={zhCN.uploadSettings.passwordPlaceholder}
            onChange={(event) => set("password", event.target.value)}
          />
          <button
            type="button"
            aria-label={showPassword ? zhCN.uploadSettings.hidePassword : zhCN.uploadSettings.showPassword}
            onClick={() => setShowPassword((shown) => !shown)}
          >
            {showPassword ? <EyeOff size={19} /> : <Eye size={19} />}
          </button>
        </span>
      </label>
      {disabled && <p className="settings-note">{zhCN.uploadSettings.lockedNotice}</p>}
    </section>
  );
}
