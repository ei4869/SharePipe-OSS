import { Link2, Settings } from "lucide-react";
import { zhCN } from "../i18n/zh-CN";
import "./AppHeader.css";

export function Brand() {
  return (
    <a className="brand" href="/" aria-label={zhCN.homeAria}>
      <span className="brand-mark" aria-hidden="true">
        <svg viewBox="0 0 40 40" role="img">
          <path d="M4.5 9.5a4 4 0 0 1 4-4h8l4 4h10a4 4 0 0 1 4 4v8" />
          <path d="M22.5 30.5H8.5a4 4 0 0 1-4-4v-17" />
          <path d="M20.5 27.5l4-4a5 5 0 0 1 7 7l-3 3a5 5 0 0 1-7 0" />
          <path d="M28.5 24.5l3-3a5 5 0 1 1 7 7l-4 4" />
        </svg>
      </span>
      <span>{zhCN.brand}</span>
    </a>
  );
}

export function AppHeader({ management = true }: { management?: boolean }) {
  return (
    <header className="app-header">
      <Brand />
      {management ? (
        <a className="header-action" href="/admin">
          <Settings size={19} strokeWidth={1.75} />
          <span>{zhCN.admin}</span>
        </a>
      ) : (
        <Link2 className="header-symbol" size={20} strokeWidth={1.75} />
      )}
    </header>
  );
}
