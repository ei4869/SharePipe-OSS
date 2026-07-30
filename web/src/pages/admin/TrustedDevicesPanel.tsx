import { Link2, MonitorSmartphone, Pencil } from "lucide-react";
import { useEffect, useState } from "react";
import { api } from "../../api";
import { zhCN } from "../../i18n/zh-CN";
import type { TrustedDevice } from "../../types";

export function TrustedDevicesPanel({
  localAdmin,
  onNotice,
  onError,
}: {
  localAdmin: boolean;
  onNotice: (message: string) => void;
  onError: (message: string) => void;
}) {
  const [items, setItems] = useState<TrustedDevice[]>([]);
  const [scope, setScope] = useState<"local" | "cloud">(localAdmin ? "local" : "cloud");
  const [cloudAvailable, setCloudAvailable] = useState(!localAdmin);
  const [bootstrapName, setBootstrapName] = useState<string>(zhCN.adminPanel.myBrowser);

  const loadDevices = async (nextScope = scope) => {
    try {
      const result = await api.trustedDevices(nextScope);
      setItems(result.items);
      setCloudAvailable(Boolean(result.cloudAvailable) || result.scope === "cloud");
      setScope(result.scope === "cloud" ? "cloud" : "local");
    } catch (error) {
      onError(error instanceof Error ? error.message : zhCN.adminPanel.devicesLoadFailed);
    }
  };

  useEffect(() => {
    void loadDevices();
  }, []);

  const switchScope = (next: "local" | "cloud") => {
    setScope(next);
    void loadDevices(next);
  };

  return (
    <>
      <header className="admin-heading">
        <div>
          <h1>{zhCN.adminPanel.devicesTitle}</h1>
          <p>{zhCN.adminPanel.devicesLead}</p>
        </div>
      </header>
      {localAdmin && cloudAvailable && (
        <div className="scope-switch">
          <button className={scope === "local" ? "is-active" : ""} onClick={() => switchScope("local")}>{zhCN.adminPanel.localService}</button>
          <button className={scope === "cloud" ? "is-active" : ""} onClick={() => switchScope("cloud")}>{zhCN.adminPanel.cloudDevices}</button>
        </div>
      )}
      {localAdmin && cloudAvailable && scope === "cloud" && (
        <section className="admin-section device-bootstrap">
          <h2>{zhCN.adminPanel.bootstrapTitle}</h2>
          <p>{zhCN.adminPanel.bootstrapLead}</p>
          <div>
            <input className="text-input" value={bootstrapName} onChange={(event) => setBootstrapName(event.target.value)} />
            <button
              className="primary-button"
              onClick={async () => {
                try {
                  const result = await api.createTrustedDeviceBootstrap(bootstrapName);
                  window.location.assign(result.url);
                } catch (error) {
                  onError(error instanceof Error ? error.message : zhCN.adminPanel.bootstrapFailed);
                }
              }}
            >
              <Link2 size={17} />
              {zhCN.adminPanel.bootstrapAction}
            </button>
          </div>
        </section>
      )}
      <section className="admin-section">
        <div className="trusted-device-list">
          {items.map((item) => (
            <article key={item.id} className={item.revokedAt ? "is-revoked" : ""}>
              <MonitorSmartphone size={22} />
              <div>
                <strong>{item.name}{item.system && <em>{zhCN.adminPanel.localDevice}</em>}</strong>
                <span>{zhCN.adminPanel.deviceLastSeen(item.platform || zhCN.adminPanel.unknownPlatform, new Date(item.lastSeenAt).toLocaleString(zhCN.locale))}</span>
                <small>{item.userAgent || zhCN.adminPanel.builtInDevice}</small>
              </div>
              <div className="device-actions">
                {!item.system && !item.revokedAt && (
                  <>
                    <button
                      className="ghost-button"
                      onClick={async () => {
                        const name = window.prompt(zhCN.adminPanel.renameDevicePrompt, item.name);
                        if (!name) return;
                        try {
                          await api.renameTrustedDevice(item.id, name, scope);
                          onNotice(zhCN.adminPanel.deviceRenamed);
                          await loadDevices();
                        } catch (error) {
                          onError(error instanceof Error ? error.message : zhCN.adminPanel.deviceRenameFailed);
                        }
                      }}
                    >
                      <Pencil size={16} />
                      {zhCN.adminPanel.rename}
                    </button>
                    <button
                      className="ghost-button danger-text"
                      onClick={async () => {
                        if (!window.confirm(zhCN.adminPanel.removeDeviceConfirm(item.name))) return;
                        try {
                          await api.revokeTrustedDevice(item.id, scope);
                          onNotice(zhCN.adminPanel.deviceRemoved);
                          await loadDevices();
                        } catch (error) {
                          onError(error instanceof Error ? error.message : zhCN.adminPanel.deviceRemoveFailed);
                        }
                      }}
                    >
                      {zhCN.adminPanel.remove}
                    </button>
                  </>
                )}
              </div>
            </article>
          ))}
          {items.length === 0 && <p className="table-empty">{zhCN.adminPanel.noDevices}</p>}
        </div>
      </section>
    </>
  );
}
