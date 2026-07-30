import { Component, lazy, Suspense, type ErrorInfo, type ReactNode } from "react";
import { zhCN } from "./i18n/zh-CN";
import { ManagePage } from "./pages/ManagePage";
import { SharePage } from "./pages/SharePage";
import { UploadPage } from "./pages/UploadPage";
import { PairPage } from "./pages/PairPage";
import { DeviceLinkPage } from "./pages/DeviceLinkPage";

const AdminPage = lazy(() =>
  import("./pages/AdminPage").then((module) => ({ default: module.AdminPage })),
);

export function App() {
  const path = window.location.pathname;
  if (path === "/admin" || path.startsWith("/admin/")) {
    return (
      <Suspense fallback={<div className="page-state">{zhCN.adminPanel.connecting}</div>}>
        <AdminPage />
      </Suspense>
    );
  }
  if (path.startsWith("/s/")) {
    return <SharePage token={decodePathToken(path, "/s/")} />;
  }
  if (path === "/pair") return <PairPage />;
  if (path === "/device-link") return <DeviceLinkPage />;
  if (path.startsWith("/m/")) {
    return <ManagePage token={decodePathToken(path, "/m/")} />;
  }
  if (path.startsWith("/resume/")) {
    return <UploadPage recoveryToken={decodePathToken(path, "/resume/")} />;
  }
  return <UploadPage />;
}

function decodePathToken(path: string, prefix: string) {
  const value = path.slice(prefix.length).split("/")[0];
  try {
    return decodeURIComponent(value);
  } catch {
    return value;
  }
}

interface ErrorBoundaryState {
  error: Error | null;
}

export class ErrorBoundary extends Component<{ children: ReactNode }, ErrorBoundaryState> {
  state: ErrorBoundaryState = { error: null };

  static getDerivedStateFromError(error: Error): ErrorBoundaryState {
    return { error };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error("SharePipe UI error", error, info);
  }

  render() {
    if (this.state.error) {
      return (
        <main className="page-state">
          <div>
            <h1>{zhCN.app.errorTitle}</h1>
            <p>{this.state.error.message}</p>
            <a className="primary-button" href="/">
              {zhCN.app.backHome}
            </a>
          </div>
        </main>
      );
    }
    return this.props.children;
  }
}
