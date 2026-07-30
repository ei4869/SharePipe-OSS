import {
  CalendarClock,
  ChevronRight,
  Download,
  File,
  FileArchive,
  FileText,
  Folder,
  Home,
  LockKeyhole,
  Search,
  X,
} from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { api } from "../api";
import { AppHeader } from "../components/AppHeader";
import { previewKind } from "../features/share/preview-policy";
import { zhCN } from "../i18n/zh-CN";
import type { DownloadGrant, Entry, PublicShare } from "../types";
import { basename, formatBytes, parentPath, remainingTime, sleep } from "../utils";
import "./SharePage.css";

export function SharePage({ token }: { token: string }) {
  const [data, setData] = useState<PublicShare | null>(null);
  const [problem, setProblem] = useState("");
  const [password, setPassword] = useState("");
  const [unlocking, setUnlocking] = useState(false);
  const [query, setQuery] = useState("");
  const [currentPath, setCurrentPath] = useState("");
  const [selected, setSelected] = useState<Entry | null>(null);
  const [downloading, setDownloading] = useState("");
  const [collapsedPaths, setCollapsedPaths] = useState<Set<string>>(() => new Set());

  const load = async () => {
    try {
      setProblem("");
      setData(await api.publicShare(token));
    } catch (error) {
      setProblem(error instanceof Error ? error.message : zhCN.share.cannotOpen);
    }
  };

  useEffect(() => {
    setCollapsedPaths(new Set());
    void load();
  }, [token]);

  useEffect(() => {
    if (!data?.entries?.length || selected) return;
    if (!window.matchMedia("(min-width: 821px)").matches) return;
    const first = data.entries.find((entry) => entry.kind === "file" && entry.status === "ready");
    if (first) setSelected(first);
  }, [data, selected]);

  const visibleEntries = useMemo(() => {
    if (!data) return [];
    if (query.trim()) {
      const lowered = query.trim().toLocaleLowerCase();
      return data.entries.filter((entry) => entry.path.toLocaleLowerCase().includes(lowered));
    }
    return data.entries.filter((entry) => parentPath(entry.path) === currentPath);
  }, [currentPath, data, query]);

  const treeEntries = useMemo(() => {
    if (!data) return [];
    const lowered = query.trim().toLocaleLowerCase();
    if (lowered) {
      const visiblePaths = new Set<string>();
      for (const entry of data.entries) {
        if (!entry.path.toLocaleLowerCase().includes(lowered)) continue;
        visiblePaths.add(entry.path);
        for (const parent of ancestorPaths(entry.path)) visiblePaths.add(parent);
      }
      return data.entries.filter((entry) => visiblePaths.has(entry.path));
    }
    return data.entries.filter((entry) => ancestorPaths(entry.path).every((parent) => !collapsedPaths.has(parent)));
  }, [collapsedPaths, data, query]);

  const unlock = async (event: React.FormEvent) => {
    event.preventDefault();
    setUnlocking(true);
    setProblem("");
    try {
      await api.unlockShare(token, password);
      await load();
    } catch (error) {
      setProblem(error instanceof Error ? error.message : zhCN.share.wrongPassword);
    } finally {
      setUnlocking(false);
    }
  };

  const beginDownload = async (kind: "file" | "zip", entryId = "") => {
    const key = kind === "zip" ? "zip" : entryId;
    setDownloading(key);
    setProblem("");
    try {
      let grant = await api.createDownload(token, kind, entryId);
      grant = await waitForGrant(grant);
      window.location.assign(grant.url);
    } catch (error) {
      setProblem(error instanceof Error ? error.message : zhCN.share.downloadPrepareFailed);
    } finally {
      setDownloading("");
    }
  };

  if (problem && !data) {
    return (
      <div className="share-page">
        <AppHeader management={false} />
        <div className="page-state">
          <div>
            <FileArchive size={44} strokeWidth={1.5} />
            <h1>{zhCN.share.cannotOpen}</h1>
            <p>{problem}</p>
          </div>
        </div>
      </div>
    );
  }
  if (!data) {
    return (
      <div className="share-page">
        <AppHeader management={false} />
        <div className="page-state">{zhCN.share.loading}</div>
      </div>
    );
  }
  if (data.locked) {
    return (
      <div className="share-page">
        <AppHeader management={false} />
        <main className="unlock-panel">
          <LockKeyhole size={42} strokeWidth={1.5} />
          <h1>{zhCN.share.passwordTitle}</h1>
          <p>{zhCN.share.passwordLead}</p>
          <form onSubmit={unlock}>
            <label>
              <span className="field-label">{zhCN.share.accessPassword}</span>
              <input
                className="text-input"
                type="password"
                autoFocus
                value={password}
                onChange={(event) => setPassword(event.target.value)}
              />
            </label>
            {problem && <p className="error-text">{problem}</p>}
            <button className="primary-button" type="submit" disabled={unlocking || !password}>
              {unlocking ? zhCN.share.verifying : zhCN.share.open}
            </button>
          </form>
        </main>
      </div>
    );
  }

  const share = data.share;
  const crumbs = currentPath ? currentPath.split("/") : [];

  return (
    <div className="share-page">
      <AppHeader management={false} />
      <section className="share-summary">
        <div>
          <h1>{share.name}</h1>
          {share.description && <p>{share.description}</p>}
          <div className="share-meta">
            <span>
              <File size={18} />
              {zhCN.common.fileSummary(share.fileCount, formatBytes(share.totalBytes))}
            </span>
            <span>
              <CalendarClock size={18} />
              {remainingTime(share.expiresAt)}
            </span>
            <span>
              <Download size={18} />
              {share.downloadLimit === null
                ? zhCN.share.unlimitedDownloads
                : zhCN.share.downloadsRemaining(Math.max(share.downloadLimit - share.downloadCount, 0))}
            </span>
          </div>
        </div>
        <button
          className="primary-button summary-download"
          type="button"
          disabled={downloading === "zip"}
          onClick={() => void beginDownload("zip")}
        >
          <Download size={19} />
          {downloading === "zip" ? zhCN.share.preparing : zhCN.downloadAll}
        </button>
      </section>

      {problem && <div className="share-notice error-text">{problem}</div>}

      <main className="recipient-layout">
        <aside className="file-tree-panel">
          <h2>{zhCN.files}</h2>
          <label className="search-field">
            <Search size={19} strokeWidth={1.7} />
            <input
              aria-label={zhCN.searchFiles}
              placeholder={zhCN.searchFiles}
              value={query}
              onChange={(event) => setQuery(event.target.value)}
            />
          </label>
          <div className="desktop-tree">
            {treeEntries.map((entry) => (
              <TreeRow
                key={entry.id}
                entry={entry}
                selected={selected?.id === entry.id}
                expanded={entry.kind === "directory" && !collapsedPaths.has(entry.path)}
                onClick={() => {
                  if (entry.kind === "directory") {
                    setCurrentPath(entry.path);
                    setCollapsedPaths((current) => {
                      const next = new Set(current);
                      if (next.has(entry.path)) next.delete(entry.path);
                      else next.add(entry.path);
                      return next;
                    });
                  } else setSelected(entry);
                }}
              />
            ))}
          </div>
        </aside>

        <section className="file-browser">
          <nav className="breadcrumbs" aria-label={zhCN.share.breadcrumbsAria}>
            <button type="button" onClick={() => setCurrentPath("")}>
              <Home size={18} />
            </button>
            {crumbs.map((crumb, index) => {
              const destination = crumbs.slice(0, index + 1).join("/");
              return (
                <span key={destination}>
                  <ChevronRight size={17} />
                  <button type="button" onClick={() => setCurrentPath(destination)}>
                    {crumb}
                  </button>
                </span>
              );
            })}
          </nav>
          <div className="mobile-entry-list">
            {visibleEntries.map((entry) => (
              <MobileEntryRow
                key={entry.id}
                entry={entry}
                busy={downloading === entry.id}
                onOpen={() => {
                  if (entry.kind === "directory") {
                    setCurrentPath(entry.path);
                  } else {
                    setSelected(entry);
                  }
                }}
                onDownload={() => void beginDownload("file", entry.id)}
              />
            ))}
          </div>
          {selected ? (
            <PreviewPane
              token={token}
              entry={selected}
              busy={downloading === selected.id}
              onClose={() => setSelected(null)}
              onDownload={() => void beginDownload("file", selected.id)}
            />
          ) : (
            <div className="preview-empty">{zhCN.share.previewHint}</div>
          )}
        </section>
      </main>

      <footer className="share-footer">
        <span>
          <CalendarClock size={18} />
          {zhCN.share.expiresAt(new Date(share.expiresAt).toLocaleString(zhCN.locale))}
        </span>
        <button
          className="primary-button"
          type="button"
          disabled={downloading === "zip"}
          onClick={() => void beginDownload("zip")}
        >
          <Download size={20} />
          {downloading === "zip" ? zhCN.share.preparing : zhCN.downloadAll}
        </button>
      </footer>
    </div>
  );
}

function TreeRow({
  entry,
  selected,
  expanded,
  onClick,
}: {
  entry: Entry;
  selected: boolean;
  expanded: boolean;
  onClick: () => void;
}) {
  const depth = entry.path.split("/").length - 1;
  return (
    <button
      className={"tree-row" + (selected ? " is-selected" : "")}
      style={{ paddingLeft: 14 + depth * 22 }}
      type="button"
      aria-expanded={entry.kind === "directory" ? expanded : undefined}
      title={entry.path}
      onClick={onClick}
    >
      {entry.kind === "directory" ? (
        <ChevronRight className={"tree-disclosure" + (expanded ? " is-expanded" : "")} size={16} strokeWidth={2} />
      ) : (
        <span className="tree-disclosure-spacer" aria-hidden="true" />
      )}
      {entry.kind === "directory" ? (
        <Folder size={20} strokeWidth={1.7} />
      ) : (
        <FileText size={20} strokeWidth={1.7} />
      )}
      <span>{basename(entry.path)}</span>
      {entry.kind === "file" && <small>{formatBytes(entry.size)}</small>}
    </button>
  );
}

function ancestorPaths(path: string): string[] {
  const segments = path.split("/");
  segments.pop();
  const result: string[] = [];
  let current = "";
  for (const segment of segments) {
    current = current ? `${current}/${segment}` : segment;
    result.push(current);
  }
  return result;
}

function MobileEntryRow({
  entry,
  busy,
  onOpen,
  onDownload,
}: {
  entry: Entry;
  busy: boolean;
  onOpen: () => void;
  onDownload: () => void;
}) {
  return (
    <div className="mobile-entry-row">
      <button className="entry-main" type="button" onClick={onOpen}>
        {entry.kind === "directory" ? (
          <Folder size={34} strokeWidth={1.6} />
        ) : (
          <FileText size={34} strokeWidth={1.6} />
        )}
        <span>
          <strong>{basename(entry.path)}</strong>
          <small>{entry.kind === "directory" ? zhCN.share.folder : formatBytes(entry.size)}</small>
        </span>
      </button>
      {entry.kind === "directory" ? (
        <ChevronRight size={21} className="entry-chevron" />
      ) : (
        <>
          <button className="preview-link" type="button" onClick={onOpen}>
            {zhCN.preview}
          </button>
          <button
            className="entry-download"
            type="button"
            disabled={busy}
            aria-label={zhCN.share.downloadFile(basename(entry.path))}
            onClick={onDownload}
          >
            <Download size={23} strokeWidth={1.7} />
          </button>
        </>
      )}
    </div>
  );
}

function PreviewPane({
  token,
  entry,
  busy,
  onClose,
  onDownload,
}: {
  token: string;
  entry: Entry;
  busy: boolean;
  onClose: () => void;
  onDownload: () => void;
}) {
  const url =
    "/api/v1/public/shares/" +
    encodeURIComponent(token) +
    "/entries/" +
    encodeURIComponent(entry.id) +
    "/preview";
  return (
    <div className="preview-pane">
      <header>
        <span>{entry.path}</span>
        <span>{formatBytes(entry.size)}</span>
        <button className="secondary-button" type="button" disabled={busy} onClick={onDownload}>
          <Download size={18} />
          {busy ? zhCN.share.preparingShort : zhCN.download}
        </button>
        <button className="preview-close" type="button" aria-label={zhCN.share.closePreview} onClick={onClose}>
          <X size={20} />
        </button>
      </header>
      <PreviewContent entry={entry} url={url} />
    </div>
  );
}

function PreviewContent({ entry, url }: { entry: Entry; url: string }) {
  const kind = previewKind(entry);
  const [text, setText] = useState("");
  const textPreview = kind === "text";

  useEffect(() => {
    if (!textPreview) return;
    const controller = new AbortController();
    fetch(url, { credentials: "same-origin", signal: controller.signal })
      .then((response) => {
        if (!response.ok) throw new Error(zhCN.share.textPreviewFailed);
        return response.text();
      })
      .then((value) => setText(value.slice(0, 2_000_000)))
      .catch(() => setText(zhCN.share.textPreviewUnavailable));
    return () => controller.abort();
  }, [textPreview, url]);

  if (kind === "image") {
    return <img className="image-preview" src={url} alt={basename(entry.path)} />;
  }
  if (kind === "pdf") {
    return <iframe className="document-preview" src={url} title={basename(entry.path)} />;
  }
  if (kind === "video") {
    return <video className="media-preview" src={url} controls />;
  }
  if (kind === "audio") {
    return <audio className="audio-preview" src={url} controls />;
  }
  if (textPreview) {
    return <pre className="text-preview">{text || zhCN.share.textPreviewLoading}</pre>;
  }
  return (
    <div className="preview-empty">
      <FileArchive size={48} strokeWidth={1.4} />
      <p>{zhCN.share.unsupportedPreview}</p>
    </div>
  );
}

async function waitForGrant(grant: DownloadGrant): Promise<DownloadGrant> {
  let current = grant;
  for (let attempt = 0; current.status === "preparing" && attempt < 180; attempt++) {
    await sleep(2000);
    current = await api.downloadStatus(current.token);
  }
  if (current.status !== "ready") throw new Error(zhCN.share.zipPrepareFailed);
  return current;
}
