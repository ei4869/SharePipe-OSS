import { Pause, Play, QrCode, RefreshCw, ShieldCheck, Upload } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { api } from "../api";
import { AppHeader } from "../components/AppHeader";
import { FilePicker } from "../features/upload/FilePicker";
import type { PersistentFileHandle } from "../features/upload/file-system-access";
import { ShareSettings, type ShareForm } from "../features/upload/ShareSettings";
import { LinkField, RecoverySummary, UploadSuccess } from "../features/upload/UploadResults";
import {
  fileKey,
  hasEveryOpenFile,
  isAbortError,
  matchRecoveredPath,
  materializeHandles,
  mergePickedFiles,
  restoredProgress,
  sha256Blob,
} from "../features/upload/upload-runtime";
import {
  latestPendingUpload,
  loadFileHandles,
  removePendingUpload,
  saveFileHandles,
  savePendingUpload,
} from "../features/upload/upload-task-store";
import type { PickedFile } from "../features/upload/types";
import { zhCN } from "../i18n/zh-CN";
import type { PublicConfig, ShareCreation, UploadAuthorization, UploadDescriptor, UploadPart } from "../types";
import { formatBytes, sleep } from "../utils";
import "./UploadPage.css";

const initialForm: ShareForm = {
  inviteCode: "",
  name: "",
  description: "",
  expiresIn: "168h",
  downloadLimit: 10,
  password: "",
  pairingEnabled: true,
  qrEnabled: true,
};

type UploadPhase = "idle" | "recovering" | "waiting" | "creating" | "uploading" | "paused" | "error" | "success";

interface UploadPageProps {
  recoveryToken?: string;
}

export function UploadPage({ recoveryToken = "" }: UploadPageProps) {
  const [config, setConfig] = useState<PublicConfig | null>(null);
  const [authorization, setAuthorization] = useState<UploadAuthorization | null>(null);
  const [files, setFiles] = useState<PickedFile[]>([]);
  const [form, setForm] = useState(initialForm);
  const [phase, setPhase] = useState<UploadPhase>(recoveryToken ? "recovering" : "idle");
  const [problem, setProblem] = useState("");
  const [creation, setCreation] = useState<ShareCreation | null>(null);
  const [activeRecoveryToken, setActiveRecoveryToken] = useState(recoveryToken);
  const [permissionHandles, setPermissionHandles] = useState<Map<string, PersistentFileHandle>>(new Map());
  const [paused, setPaused] = useState(false);
  const [controlBusy, setControlBusy] = useState(false);
  const pausedRef = useRef(false);
  const cancelledRef = useRef(false);
  const requestControllersRef = useRef(new Set<AbortController>());
  const recoveryGenerationRef = useRef(0);
  const uploadGenerationRef = useRef(0);
  const activeUploadRef = useRef("");

  useEffect(() => {
    api
      .publicConfig()
      .then((loaded) => {
        setConfig(loaded);
        setForm((current) => ({
          ...current,
          expiresIn: loaded.defaultExpiry,
          downloadLimit: loaded.defaultDownloadLimit,
        }));
      })
      .catch((error: Error) => setProblem(error.message));
  }, []);

  useEffect(() => { api.uploadAuthorization().then(setAuthorization).catch(() => setAuthorization({ trusted: false, source: "invite", deviceName: "" })); }, []);

  useEffect(() => {
    if (activeRecoveryToken) return;
    const pending = latestPendingUpload();
    if (!pending) return;
    window.history.replaceState({}, "", new URL(pending.recoveryUrl, window.location.origin).pathname);
    setActiveRecoveryToken(pending.recoveryToken);
  }, [activeRecoveryToken]);

  useEffect(() => {
    if (!activeRecoveryToken) return;
    const generation = ++recoveryGenerationRef.current;
    setPhase("recovering");
    setProblem("");
    void (async () => {
      try {
        const loaded = await api.recoverUpload(activeRecoveryToken);
        if (generation !== recoveryGenerationRef.current) return;
        setCreation(loaded);
        savePendingUpload({
          version: 1,
          shareId: loaded.share.id,
          recoveryToken: loaded.managementToken,
          recoveryUrl: loaded.recoveryUrl,
          name: loaded.share.name,
          createdAt: loaded.share.createdAt,
        });
        if (loaded.uploadTaskStatus === "completed" || loaded.share.status === "active" || loaded.uploads.every((item) => item.status === "completed")) {
          removePendingUpload(loaded.share.id);
          setPhase("success");
          return;
        }

        const handles = await loadFileHandles(loaded.share.id);
        const restored = await materializeHandles(loaded, handles);
        if (generation !== recoveryGenerationRef.current) return;
        setFiles(restored.files);
        setPermissionHandles(restored.pending);
        if (loaded.uploadTaskStatus === "paused") {
          pausedRef.current = true;
          setPaused(true);
          setPhase("paused");
        } else if (hasEveryOpenFile(loaded, restored.files)) {
          setPhase("idle");
          void runUpload(loaded, restored.files).catch(() => undefined);
        } else {
          setPhase("waiting");
        }
      } catch (error) {
        if (generation !== recoveryGenerationRef.current) return;
        setProblem(error instanceof Error ? error.message : zhCN.upload.recoveryFailed);
        setPhase("error");
      }
    })();
  }, [activeRecoveryToken]);

  const addFiles = (incoming: Array<{ file: File; path: string; handle?: PersistentFileHandle }>) => {
    setFiles((current) => {
      const keys = new Set(current.map((item) => fileKey(item.file, item.path)));
      const occupiedPaths = new Set(current.map((item) => item.path));
      const additions: PickedFile[] = [];
      for (const item of incoming) {
        let normalized = item.path.replaceAll("\\", "/").replace(/^\/+/, "");
        if (creation) normalized = matchRecoveredPath(creation, item.file, normalized, occupiedPaths);
        const key = fileKey(item.file, normalized);
        if (!normalized || keys.has(key) || occupiedPaths.has(normalized)) continue;
        keys.add(key);
        occupiedPaths.add(normalized);
        const descriptor = creation?.uploads.find((upload) => upload.path === normalized);
        additions.push({
          id: crypto.randomUUID(),
          file: item.file,
          path: normalized,
          progress: descriptor ? restoredProgress(descriptor) : 0,
          status: descriptor?.status === "completed" ? "completed" : "pending",
          handle: item.handle,
        });
      }
      const next = [...current, ...additions];
      if (creation) void saveFileHandles(creation.share.id, next).catch(() => undefined);
      return next;
    });
  };

  const validate = (selected: PickedFile[]): string => {
    if (creation) {
      for (const descriptor of creation.uploads) {
        if (descriptor.status === "completed") continue;
        const file = selected.find((item) => item.path === descriptor.path);
        if (!file) return zhCN.upload.chooseOriginalFiles;
        if (file.file.size !== descriptor.expectedSize) return zhCN.upload.originalFileMismatch(descriptor.path);
      }
      return "";
    }
    if (!config) return zhCN.upload.configLoadingProblem;
    if (selected.length === 0) return zhCN.upload.chooseAtLeastOne;
    if (!authorization?.trusted && !form.inviteCode.trim()) return zhCN.upload.enterInvite;
    if (!form.name.trim()) return zhCN.upload.enterName;
    if (form.password && form.password.length < 8) return zhCN.upload.passwordMinimum;
    if (selected.length > config.maxFilesPerShare) return zhCN.upload.tooManyFiles;
    const total = selected.reduce((sum, item) => sum + item.file.size, 0);
    if (total > config.maxShareBytes) return zhCN.upload.shareTooLarge(formatBytes(config.maxShareBytes));
    const oversized = selected.find((item) => item.file.size > config.maxFileBytes);
    if (oversized) return zhCN.upload.fileTooLarge(oversized.path);
    return "";
  };

  const start = async () => {
    const validation = validate(files);
    if (validation) {
      setProblem(validation);
      return;
    }
    setProblem("");
    try {
      let created = creation;
      if (!created) {
        setPhase("creating");
        created = await api.createShare({
          inviteCode: form.inviteCode.trim(),
          name: form.name.trim(),
          description: form.description.trim(),
          expiresIn: form.expiresIn,
          downloadLimit: form.downloadLimit,
          password: form.password,
          pairingEnabled: form.pairingEnabled,
          qrEnabled: form.qrEnabled,
          entries: files.map((item) => ({
            path: item.path,
            kind: "file" as const,
            size: item.file.size,
            mimeType: item.file.type || "application/octet-stream",
          })),
        });
        setCreation(created);
        savePendingUpload({
          version: 1,
          shareId: created.share.id,
          recoveryToken: created.managementToken,
          recoveryUrl: created.recoveryUrl,
          name: created.share.name,
          createdAt: created.share.createdAt,
        });
        await saveFileHandles(created.share.id, files).catch(() => undefined);
        window.history.replaceState({}, "", new URL(created.recoveryUrl, window.location.origin).pathname);
      }
      await runUpload(created, files);
    } catch (error) {
      setProblem(error instanceof Error ? error.message : zhCN.upload.failed);
      setPhase("error");
    }
  };

  async function runUpload(created: ShareCreation, selected: PickedFile[]) {
    if (activeUploadRef.current === created.share.id) return;
    if (created.uploadTaskStatus === "paused") return;
    activeUploadRef.current = created.share.id;
    const uploadGeneration = ++uploadGenerationRef.current;
    cancelledRef.current = false;
    pausedRef.current = false;
    setPaused(false);
    setProblem("");
    setPhase("uploading");
    try {
      let cursor = 0;
      const workers = Array.from({ length: Math.min(3, created.uploads.length) }, async () => {
        while (cursor < created.uploads.length) {
          const descriptor = created.uploads[cursor++];
          const item = selected.find((candidate) => candidate.path === descriptor.path);
          if (descriptor.status === "completed") {
            if (item) updateFile(item.id, { status: "completed", progress: 100 });
            continue;
          }
          if (!item) throw new Error(zhCN.upload.chooseOriginalFiles);
          await uploadOne(descriptor, item, uploadGeneration);
        }
      });
      const results = await Promise.allSettled(workers);
      const failed = results.find((result): result is PromiseRejectedResult => result.status === "rejected");
      if (failed) throw failed.reason;
      created.uploadTaskStatus = "completed";
      setPhase("success");
      removePendingUpload(created.share.id);
    } catch (error) {
      if (pausedRef.current || cancelledRef.current || isAbortError(error)) return;
      setProblem(error instanceof Error ? error.message : zhCN.upload.failed);
      setPhase("error");
      throw error;
    } finally {
      if (activeUploadRef.current === created.share.id) activeUploadRef.current = "";
    }
  }

  async function uploadOne(descriptor: UploadDescriptor, item: PickedFile, uploadGeneration: number) {
    updateFile(item.id, { status: "uploading", progress: restoredProgress(descriptor), error: undefined });
    const knownParts = new Map(descriptor.parts.map((part) => [part.partNumber, part]));
    let cursor = 1;
    const updateKnownProgress = () => {
      descriptor.parts = [...knownParts.values()].sort((left, right) => left.partNumber - right.partNumber);
      descriptor.receivedBytes = descriptor.parts.reduce((sum, part) => sum + part.size, 0);
      updateFile(item.id, { progress: item.file.size === 0 ? 100 : (descriptor.receivedBytes / item.file.size) * 100 });
    };
    try {
      const partWorkers = Array.from({ length: Math.min(3, descriptor.partCount) }, async () => {
        while (cursor <= descriptor.partCount) {
          const partNumber = cursor++;
          ensureUploadActive(uploadGeneration);
          if (cancelledRef.current) throw new Error(zhCN.upload.cancelled);
          const startAt = (partNumber - 1) * descriptor.chunkSize;
          const endAt = Math.min(item.file.size, startAt + descriptor.chunkSize);
          const blob = item.file.slice(startAt, endAt);
          const digest = await sha256Blob(blob);
          ensureUploadActive(uploadGeneration);
          if (cancelledRef.current) throw new DOMException(zhCN.upload.cancelled, "AbortError");
          const existing = knownParts.get(partNumber);
          if (existing && existing.size === blob.size && existing.sha256 === digest) continue;

          const controller = new AbortController();
          requestControllersRef.current.add(controller);
          try {
            ensureUploadActive(uploadGeneration);
            if (descriptor.backend === "local" || descriptor.backend === "worker") {
              const progress = await api.putPart(descriptor.sessionId, descriptor.token, partNumber, blob, digest, controller.signal);
              for (const part of progress.parts) knownParts.set(part.partNumber, part);
            } else {
              const signed = await api.presignPart(descriptor.sessionId, descriptor.token, partNumber, controller.signal);
              let response: Response;
              try {
                response = await fetch(signed.url, { method: "PUT", body: blob, signal: controller.signal });
              } catch (error) {
                if (controller.signal.aborted) throw error;
                throw new Error(zhCN.upload.s3NetworkFailed);
              }
              if (!response.ok) throw new Error(zhCN.upload.s3PartFailed);
              const etag = response.headers.get("ETag");
              if (!etag) throw new Error(zhCN.upload.s3ETagMissing);
              const acknowledged: UploadPart = { partNumber, size: blob.size, etag, sha256: digest };
              const progress = await api.acknowledgePart(descriptor.sessionId, descriptor.token, acknowledged, controller.signal);
              for (const part of progress.parts) knownParts.set(part.partNumber, part);
            }
          } finally {
            requestControllersRef.current.delete(controller);
          }
          updateKnownProgress();
        }
      });
      const results = await Promise.allSettled(partWorkers);
      const failed = results.find((result): result is PromiseRejectedResult => result.status === "rejected");
      if (failed) throw failed.reason;
      const parts = [...knownParts.values()].sort((left, right) => left.partNumber - right.partNumber);
      const controller = new AbortController();
      requestControllersRef.current.add(controller);
      try {
        ensureUploadActive(uploadGeneration);
        await api.completeUpload(descriptor.sessionId, descriptor.token, parts, controller.signal);
      } finally {
        requestControllersRef.current.delete(controller);
      }
      descriptor.status = "completed";
      descriptor.receivedBytes = descriptor.expectedSize;
      updateFile(item.id, { status: "completed", progress: 100 });
    } catch (error) {
      if (pausedRef.current || cancelledRef.current || isAbortError(error)) {
        updateFile(item.id, { status: "pending", error: undefined });
      } else {
        updateFile(item.id, { status: "error", error: error instanceof Error ? error.message : zhCN.upload.failed });
      }
      throw error;
    }
  }

  const ensureUploadActive = (uploadGeneration: number) => {
    if (uploadGeneration !== uploadGenerationRef.current || pausedRef.current || cancelledRef.current) {
      throw new DOMException(pausedRef.current ? zhCN.pauseUpload : zhCN.upload.cancelled, "AbortError");
    }
  };

  const updateFile = (id: string, patch: Partial<PickedFile>) => {
    setFiles((current) => current.map((item) => (item.id === id ? { ...item, ...patch } : item)));
  };

  const authorizeStoredFiles = async () => {
    if (!creation || permissionHandles.size === 0) return;
    setProblem("");
    try {
      await Promise.all(
        [...permissionHandles.values()].map((handle) =>
          handle.requestPermission ? handle.requestPermission({ mode: "read" }) : Promise.resolve("granted" as const),
        ),
      );
      const restored = await materializeHandles(creation, permissionHandles);
      const merged = mergePickedFiles(files, restored.files);
      setFiles(merged);
      setPermissionHandles(restored.pending);
      if (hasEveryOpenFile(creation, merged)) await runUpload(creation, merged);
      else setPhase("waiting");
    } catch (error) {
      setProblem(error instanceof Error ? error.message : zhCN.upload.permissionFailed);
      setPhase("waiting");
    }
  };

  const abortActiveRequests = () => {
    for (const controller of requestControllersRef.current) controller.abort();
    requestControllersRef.current.clear();
  };

  const togglePause = async () => {
    if (!creation || controlBusy) return;
    setControlBusy(true);
    setProblem("");
    try {
      if (!pausedRef.current) {
        const task = await api.pauseUploadTask(creation.share.id, creation.managementToken);
        creation.uploadTaskStatus = task.status;
        pausedRef.current = true;
        uploadGenerationRef.current += 1;
        abortActiveRequests();
        setPaused(true);
        setPhase("paused");
        setCreation({ ...creation });
        return;
      }
      const task = await api.resumeUploadTask(creation.share.id, creation.managementToken);
      pausedRef.current = false;
      setPaused(false);
      setPhase("uploading");
      creation.uploadTaskStatus = task.status;
      while (activeUploadRef.current === creation.share.id) await sleep(25);
      const refreshed = await api.recoverUpload(creation.managementToken);
      setCreation(refreshed);
      void runUpload(refreshed, files).catch(() => undefined);
    } catch (error) {
      setProblem(error instanceof Error ? error.message : (pausedRef.current ? zhCN.upload.resumeFailed : zhCN.upload.pauseFailed));
    } finally {
      setControlBusy(false);
    }
  };

  const cancelUpload = async () => {
    if (!creation || controlBusy || !window.confirm(zhCN.upload.cancelConfirm)) return;
    setControlBusy(true);
    setProblem("");
    try {
      await api.cancelUploadTask(creation.share.id, creation.managementToken);
      cancelledRef.current = true;
      pausedRef.current = false;
      uploadGenerationRef.current += 1;
      abortActiveRequests();
      removePendingUpload(creation.share.id);
      recoveryGenerationRef.current += 1;
      setActiveRecoveryToken("");
      setCreation(null);
      setPaused(false);
      setPhase("idle");
      window.history.replaceState({}, "", "/");
      setProblem(zhCN.upload.cancelled);
    } catch (error) {
      setProblem(error instanceof Error ? error.message : zhCN.upload.cancelFailed);
    } finally {
      setControlBusy(false);
    }
  };

  const busy = phase === "recovering" || phase === "creating" || phase === "uploading";
  const totalBytes = creation
    ? creation.uploads.reduce((sum, item) => sum + item.expectedSize, 0)
    : files.reduce((sum, item) => sum + item.file.size, 0);
  const uploadedBytes = creation
    ? creation.uploads.reduce((sum, item) => sum + (item.status === "completed" ? item.expectedSize : item.receivedBytes), 0)
    : files.reduce((sum, item) => sum + item.file.size * (item.progress / 100), 0);

  if (phase === "success" && creation) return <UploadSuccess creation={creation} />;

  return (
    <div className="upload-page">
      <AppHeader management={false} />
      <main className="upload-layout">
        <div className="upload-content">
          <div className="upload-intro">
            <div className="upload-title-row"><h1>{creation ? zhCN.upload.resumeTitle : zhCN.createTitle}</h1>{!creation && <a className="pair-entry" href="/pair"><QrCode size={18} />{zhCN.upload.pairEntry}</a>}</div>
            <p>{creation ? zhCN.upload.resumeLead : zhCN.createLead}</p>
          </div>
          {!creation && <a className="mobile-pair-entry" href="/pair"><QrCode size={18} /><span><strong>{zhCN.upload.pairEntry}</strong><small>{zhCN.upload.pairEntryHelp}</small></span></a>}
          {creation && <RecoverySummary creation={creation} recovering={phase === "recovering"} />}
          <FilePicker
            files={files}
            disabled={busy}
            onAdd={addFiles}
            onRemove={(id) => setFiles((current) => current.filter((item) => item.id !== id))}
          />
        </div>
        <aside className="settings-rail">
          {creation ? (
            <section className="recovery-controls">
              <ShieldCheck size={28} />
              <h2>{zhCN.upload.recoveryControls}</h2>
              <p>{zhCN.upload.recoverySecurity}</p>
              <LinkField label={zhCN.upload.recoveryLink} value={creation.recoveryUrl} compact />
              {permissionHandles.size > 0 && (
                <button className="secondary-button rail-button" type="button" disabled={busy} onClick={() => void authorizeStoredFiles()}>
                  <RefreshCw size={18} />
                  {zhCN.upload.authorizeAndResume}
                </button>
              )}
              {phase === "waiting" && !hasEveryOpenFile(creation, files) && (
                <p className="settings-note">{zhCN.upload.reselectFallback}</p>
              )}
            </section>
          ) : config ? (
            <ShareSettings value={form} config={config} disabled={busy} onChange={setForm} authorization={authorization} />
          ) : (
            <p className="page-state">{zhCN.upload.loadingSettings}</p>
          )}
          {problem && <p className="error-text settings-error">{problem}</p>}
          {(busy || creation) && (
            <div className="overall-progress">
              <span>{formatBytes(uploadedBytes)} / {formatBytes(totalBytes)}</span>
              <span className="progress-track">
                <span style={{ width: (totalBytes ? uploadedBytes / totalBytes : 0) * 100 + "%" }} />
              </span>
            </div>
          )}
          <div className="settings-actions">
            {phase === "uploading" || phase === "paused" ? (
              <>
                <button className="primary-button rail-button" type="button" disabled={controlBusy} onClick={() => void togglePause()}>
                  {paused ? <Play size={19} /> : <Pause size={19} />}
                  {paused ? zhCN.resumeUpload : zhCN.pauseUpload}
                </button>
                <button className="secondary-button rail-button" type="button" disabled={controlBusy} onClick={() => void cancelUpload()}>
                  {zhCN.cancel}
                </button>
              </>
            ) : (
              <button className="primary-button rail-button" type="button" disabled={busy} onClick={() => void start()}>
                <Upload size={19} strokeWidth={1.75} />
                {creation ? zhCN.upload.continueUpload : zhCN.startUpload}
              </button>
            )}
          </div>
        </aside>
      </main>
      <div className="mobile-upload-bar">
        <span>{zhCN.common.fileSummary(files.length, formatBytes(totalBytes))}</span>
        <button className="primary-button" type="button" disabled={busy || controlBusy} onClick={() => void (phase === "paused" ? togglePause() : start())}>
          <Upload size={20} />
          {phase === "paused" ? zhCN.resumeUpload : creation ? zhCN.upload.continueUpload : zhCN.startUpload}
        </button>
      </div>
    </div>
  );
}

export { UploadSuccess } from "../features/upload/UploadResults";
export { materializeHandles } from "../features/upload/upload-runtime";
