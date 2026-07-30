import type {
  DownloadGrant,
  Invite,
  Overview,
  PublicConfig,
  PublicShare,
  RuntimeSettings,
  AdminSession,
  AdministratorSecurity,
  StorageHealth,
  StorageProfile,
  StorageProfileInput,
  StorageProviderDefinition,
  StorageUsage,
  Share,
  ShareCreation,
  UploadPart,
  UploadAuthorization,
  PairingState,
  TrustedDevice,
  UploadTask,
} from "./types";
import { zhCN } from "./i18n/zh-CN";

export class APIError extends Error {
  constructor(
    public readonly status: number,
    public readonly code: string,
    message: string,
  ) {
    super(message);
  }
}

async function request<T>(url: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers);
  if (init.body && !(init.body instanceof Blob) && !(init.body instanceof FormData)) {
    headers.set("Content-Type", "application/json");
  }
  const response = await fetch(url, { ...init, headers, credentials: "same-origin" });
  if (!response.ok) {
    let body: { error?: { code?: string; message?: string } } = {};
    try {
      body = await response.json();
    } catch {
      // Keep the HTTP fallback below.
    }
    throw new APIError(
      response.status,
      body.error?.code ?? "request_failed",
      body.error?.message ?? zhCN.api.requestFailed(response.status),
    );
  }
  if (response.status === 204) {
    return undefined as T;
  }
  return response.json() as Promise<T>;
}

const uploadBase = (sessionId: string) =>
  "/api/v1/uploads/" + encodeURIComponent(sessionId);
const publicShareBase = (token: string) =>
  "/api/v1/public/shares/" + encodeURIComponent(token);

export const api = {
  publicConfig: () => request<PublicConfig>("/api/v1/public/config"),
  uploadAuthorization: () => request<UploadAuthorization>("/api/v1/upload-authorization"),
  resolvePairCode: (code: string, turnstileToken = "") => request<{ url: string; expiresAt: string }>("/api/v1/pairing/resolve", { method: "POST", body: JSON.stringify({ code, turnstileToken }) }),
  redeemTrustedDeviceBootstrap: (token: string) => request<UploadAuthorization>("/api/v1/trusted-devices/bootstrap", { method: "POST", body: JSON.stringify({ token }) }),
  validateInvite: (code: string) =>
    request<{ valid: boolean }>("/api/v1/invites/validate", {
      method: "POST",
      body: JSON.stringify({ code }),
    }),
  createShare: (input: {
    inviteCode?: string;
    name: string;
    description: string;
    expiresIn: string;
    downloadLimit: number;
    password: string;
    entries: Array<{ path: string; kind: "file"; size: number; mimeType: string }>;
    pairingEnabled: boolean;
    qrEnabled: boolean;
  }) =>
    request<ShareCreation>("/api/v1/shares", {
      method: "POST",
      body: JSON.stringify(input),
    }),
  recoverUpload: (managementToken: string) =>
    request<ShareCreation>("/api/v1/upload-recovery", {
      headers: { Authorization: "Bearer " + managementToken },
    }),
  uploadTask: (shareId: string, token: string) => request<UploadTask>("/api/v1/upload-tasks/" + encodeURIComponent(shareId), { headers: { Authorization: "Bearer " + token } }),
  pauseUploadTask: (shareId: string, token: string) => request<UploadTask>("/api/v1/upload-tasks/" + encodeURIComponent(shareId) + "/pause", { method: "POST", headers: { Authorization: "Bearer " + token }, body: "{}" }),
  resumeUploadTask: (shareId: string, token: string) => request<UploadTask>("/api/v1/upload-tasks/" + encodeURIComponent(shareId) + "/resume", { method: "POST", headers: { Authorization: "Bearer " + token }, body: "{}" }),
  cancelUploadTask: (shareId: string, token: string) => request<UploadTask>("/api/v1/upload-tasks/" + encodeURIComponent(shareId), { method: "DELETE", headers: { Authorization: "Bearer " + token } }),
  putPart: (sessionId: string, token: string, partNumber: number, body: Blob, sha256: string, signal?: AbortSignal) =>
    request<{ parts: UploadPart[] }>(uploadBase(sessionId) + "/parts/" + partNumber, {
      method: "PUT",
      headers: { Authorization: "Bearer " + token, "X-SharePipe-Part-SHA256": sha256 },
      body,
      signal,
    }),
  presignPart: (sessionId: string, token: string, partNumber: number, signal?: AbortSignal) =>
    request<{ url: string }>(uploadBase(sessionId) + "/s3-parts/" + partNumber, {
      method: "POST",
      headers: { Authorization: "Bearer " + token },
      body: "{}",
      signal,
    }),
  acknowledgePart: (sessionId: string, token: string, part: UploadPart, signal?: AbortSignal) =>
    request<{ parts: UploadPart[] }>(uploadBase(sessionId) + "/s3-parts/" + part.partNumber + "/ack", {
      method: "POST",
      headers: { Authorization: "Bearer " + token },
      body: JSON.stringify({ etag: part.etag, sha256: part.sha256 }),
      signal,
    }),
  completeUpload: (
    sessionId: string,
    token: string,
    parts: Array<{ partNumber: number; etag: string }> = [],
    signal?: AbortSignal,
  ) =>
    request(uploadBase(sessionId) + "/complete", {
      method: "POST",
      headers: { Authorization: "Bearer " + token },
      body: JSON.stringify({ parts }),
      signal,
    }),
  publicShare: (token: string) => request<PublicShare>(publicShareBase(token)),
  unlockShare: (token: string, password: string) =>
    request(publicShareBase(token) + "/unlock", {
      method: "POST",
      body: JSON.stringify({ password }),
    }),
  createDownload: (token: string, kind: "file" | "zip", entryId = "") =>
    request<DownloadGrant>(publicShareBase(token) + "/downloads", {
      method: "POST",
      body: JSON.stringify({ kind, entryId }),
    }),
  downloadStatus: (token: string) =>
    request<DownloadGrant>("/api/v1/downloads/" + encodeURIComponent(token) + "/status"),
  managedShare: (token: string) =>
    request<PublicShare>("/api/v1/manage/" + encodeURIComponent(token)),
  updateManagedShare: (
    token: string,
    input: { expiresIn?: string; downloadLimit?: number; unlimited?: boolean; password?: string },
  ) =>
    request<PublicShare>("/api/v1/manage/" + encodeURIComponent(token), {
      method: "PATCH",
      body: JSON.stringify(input),
    }),
  revokeManagedShare: (token: string) =>
    request<void>("/api/v1/manage/" + encodeURIComponent(token), { method: "DELETE" }),
  regeneratePairCode: (token: string) => request<PairingState>("/api/v1/manage/" + encodeURIComponent(token) + "/pair-code", { method: "POST", body: "{}" }),
  disablePairCode: (token: string) => request<void>("/api/v1/manage/" + encodeURIComponent(token) + "/pair-code", { method: "DELETE" }),

  adminLogin: (password: string) =>
    request("/api/v1/admin/login", { method: "POST", body: JSON.stringify({ password }) }),
  adminLogout: () => request<void>("/api/v1/admin/logout", { method: "POST", body: "{}" }),
  adminSession: () => request<AdminSession>("/api/v1/admin/session"),
  adminOverview: () => request<Overview>("/api/v1/admin/overview"),
  adminShares: () => request<{ items: Share[] }>("/api/v1/admin/shares?limit=100"),
  revokeAdminShare: (id: string) =>
    request<void>("/api/v1/admin/shares/" + encodeURIComponent(id), { method: "DELETE" }),
  adminInvites: () => request<{ items: Invite[] }>("/api/v1/admin/invites"),
  createInvite: (input: { label: string; maxUses: number | null; expiresIn: string; kind: "standard" | "trusted_device"; deviceName: string }) =>
    request<Invite>("/api/v1/admin/invites", { method: "POST", body: JSON.stringify(input) }),
  revokeInvite: (id: string) =>
    request<void>("/api/v1/admin/invites/" + encodeURIComponent(id), { method: "DELETE" }),
  trustedDevices: (scope: "local" | "cloud" = "local") => request<{ items: TrustedDevice[]; scope: string; cloudAvailable?: boolean }>("/api/v1/admin/trusted-devices?scope=" + scope),
  renameTrustedDevice: (id: string, name: string, scope: "local" | "cloud") => request<TrustedDevice>("/api/v1/admin/trusted-devices/" + encodeURIComponent(id) + "?scope=" + scope, { method: "PATCH", body: JSON.stringify({ name }) }),
  revokeTrustedDevice: (id: string, scope: "local" | "cloud") => request<void>("/api/v1/admin/trusted-devices/" + encodeURIComponent(id) + "?scope=" + scope, { method: "DELETE" }),
  createTrustedDeviceBootstrap: (name: string) => request<{ url: string; expiresAt: string }>("/api/v1/admin/trusted-devices/bootstrap", { method: "POST", body: JSON.stringify({ name }) }),
  adminSettings: () =>
    request<{ limits: RuntimeSettings; defaultStorage: string }>(
      "/api/v1/admin/settings",
    ),
  updateSettings: (input: Partial<RuntimeSettings>) =>
    request<RuntimeSettings>("/api/v1/admin/settings", {
      method: "PUT",
      body: JSON.stringify(input),
    }),
  setDefaultStorage: (backend: string) =>
    request("/api/v1/admin/settings/default-storage", {
      method: "PUT",
      body: JSON.stringify({ backend }),
    }),
  enableDiagnostics: () =>
    request<{ enabled: boolean; until: string }>("/api/v1/admin/diagnostics", {
      method: "POST",
      body: "{}",
    }),
  runMaintenance: () =>
    request("/api/v1/admin/maintenance", { method: "POST", body: "{}" }),
  adminSecurity: () => request<AdministratorSecurity>("/api/v1/admin/security"),
  addAdminEmail: (email: string) =>
    request<{ email: string }>("/api/v1/admin/security/emails", {
      method: "POST",
      body: JSON.stringify({ email }),
    }),
  removeAdminEmail: (email: string) =>
    request<void>("/api/v1/admin/security/emails/" + encodeURIComponent(email), {
      method: "DELETE",
    }),
  changeAdminPassword: (currentPassword: string, newPassword: string) =>
    request<{ changed: boolean }>("/api/v1/admin/security/password", {
      method: "PUT",
      body: JSON.stringify({ currentPassword, newPassword }),
    }),
  storageProviders: () =>
    request<{ items: StorageProviderDefinition[] }>("/api/v1/admin/storage/providers"),
  storageProfiles: () =>
    request<{ items: StorageProfile[] }>("/api/v1/admin/storage/profiles"),
  createStorageProfile: (input: StorageProfileInput) =>
    request<StorageProfile>("/api/v1/admin/storage/profiles", {
      method: "POST",
      body: JSON.stringify(input),
    }),
  updateStorageProfile: (id: string, input: StorageProfileInput) =>
    request<StorageProfile>("/api/v1/admin/storage/profiles/" + encodeURIComponent(id), {
      method: "PUT",
      body: JSON.stringify(input),
    }),
  deleteStorageProfile: (id: string) =>
    request<void>("/api/v1/admin/storage/profiles/" + encodeURIComponent(id), {
      method: "DELETE",
    }),
  testStorageProfile: (id: string) =>
    request<StorageHealth>("/api/v1/admin/storage/profiles/" + encodeURIComponent(id) + "/test", {
      method: "POST",
      body: "{}",
    }),
  testStorageInput: (input: StorageProfileInput) =>
    request<StorageHealth>("/api/v1/admin/storage/test", {
      method: "POST",
      body: JSON.stringify(input),
    }),
  storageUsage: (id: string, range: "24h" | "7d" | "31d", refresh = false) =>
    request<StorageUsage>(`/api/v1/admin/storage/profiles/${encodeURIComponent(id)}/usage?range=${range}${refresh ? "&refresh=1" : ""}`),
};
