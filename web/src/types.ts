export interface ManifestEntry {
  path: string;
  kind: "file" | "directory";
  size: number;
  mimeType: string;
}

export interface Share {
  id: string;
  name: string;
  description: string;
  storageBackend: "local" | "s3";
  storageProfileId?: string;
  status: string;
  expiresAt: string;
  downloadLimit: number | null;
  downloadCount: number;
  fileCount: number;
  totalBytes: number;
  passwordProtected: boolean;
  contentDeletedAt?: string;
  metadataDeleteAfter?: string;
  createdAt: string;
  updatedAt: string;
  pairingEnabled: boolean;
  qrEnabled: boolean;
}

export interface Entry {
  id: string;
  path: string;
  kind: "file" | "directory";
  size: number;
  mimeType: string;
  sha256?: string;
  status: string;
  createdAt: string;
}

export interface PublicShare {
  share: Share;
  entries: Entry[];
  locked: boolean;
  pairing?: PairingState;
}

export interface UploadDescriptor {
  sessionId: string;
  entryId: string;
  path: string;
  token: string;
  backend: "local" | "s3" | "worker";
  chunkSize: number;
  partCount: number;
  expectedSize: number;
  receivedBytes: number;
  status: "open" | "assembling" | "completed" | "aborted" | "expired";
  parts: UploadPart[];
}

export type UploadTaskStatus = "uploading" | "paused" | "completing" | "completed" | "cancel_requested" | "aborted" | "expired";

export interface UploadPart {
  partNumber: number;
  size: number;
  etag: string;
  sha256: string;
}

export interface ShareCreation {
  share: Share;
  publicToken: string;
  managementToken: string;
  publicUrl: string;
  managementUrl: string;
  recoveryUrl: string;
  uploadTaskStatus: UploadTaskStatus;
  uploads: UploadDescriptor[];
}

export interface UploadTask {
  shareId: string;
  status: UploadTaskStatus;
  updatedAt: string;
}

export interface PublicConfig {
  expiryPresets: string[];
  defaultExpiry: string;
  defaultDownloadLimit: number;
  chunkSize: number;
  maxFileBytes: number;
  maxShareBytes: number;
  maxFilesPerShare: number;
  storageBackend: "local" | "s3";
  turnstileSiteKey: string;
}

export interface DownloadGrant {
  token: string;
  url: string;
  status: "ready" | "preparing" | "failed";
  expiresAt: string;
}

export interface Invite {
  id: string;
  code?: string;
  label: string;
  maxUses: number | null;
  uses: number;
  expiresAt?: string;
  revokedAt?: string;
  createdAt: string;
  kind: "standard" | "trusted_device";
  deviceName: string;
}

export interface StorageHealth {
  id?: string;
  name: string;
  kind?: "local" | "s3";
  provider?: string;
  healthy: boolean;
  error?: string;
  default: boolean;
}

export interface AdminSession {
  local: boolean;
  email: string;
  authenticated: boolean;
  passwordConfigured: boolean;
  authenticationMethod?: "email" | "password" | "";
}

export interface AdministratorSecurity {
  emails: string[];
  passwordConfigured: boolean;
  local: boolean;
  currentEmail: string;
}

export interface StorageProfileConfig {
  rootPath?: string;
  accountId?: string;
  jurisdiction?: string;
  endpoint?: string;
  region?: string;
  bucket?: string;
  prefix?: string;
  pathStyle?: boolean;
  serverSideEncryption?: "none" | "AES256" | "aws:kms";
}

export interface StorageCredentials {
  accessKeyId?: string;
  secretAccessKey?: string;
  sessionToken?: string;
  clearSessionToken?: boolean;
}

export interface StorageProfileInput {
  name: string;
  provider: string;
  config: StorageProfileConfig;
  credentials: StorageCredentials;
}

export interface StorageProfile {
  id: string;
  name: string;
  provider: string;
  config: StorageProfileConfig;
  kind: "local" | "s3";
  enabled: boolean;
  managed?: boolean;
  hasCredentials: boolean;
  usesDefaultCredentials: boolean;
  hasSessionToken: boolean;
  default: boolean;
  inUse: number;
  lastCheckedAt?: string;
  lastError?: string;
  createdAt: string;
  updatedAt: string;
}

export interface StorageProviderDefinition {
  id: string;
  label: string;
  kind: "local" | "s3";
  documentationUrl: string;
  validation: "built-in" | "mounted-filesystem" | "live-tested" | "official-spec" | "contract-only";
  available?: boolean;
  creatable?: boolean;
  unavailableReason?: string;
}

export interface StorageUsagePoint {
  date: string;
  payloadBytes: number;
  metadataBytes: number;
  objectCount: number;
  uploadCount: number;
}

export interface OperationUsagePoint {
  date: string;
  classA: number;
  classB: number;
  free: number;
  unclassified: number;
}

export interface StorageUsage {
  supported: boolean;
  available: boolean;
  provider: string;
  profile: { id: string; name: string; bucket: string; metricBucket: string; prefix: string };
  range: "24h" | "7d" | "31d";
  current: { payloadBytes: number; metadataBytes: number; objectCount: number; uploadCount: number } | null;
  storageSeries: StorageUsagePoint[];
  operationSeries: OperationUsagePoint[];
  monthToDate: { classA: number; classB: number; free: number; unclassified: number; estimatedStorageGBMonth: number };
  budget: { storageGBMonth: number; classA: number; classB: number; referenceDate: string; referenceKind: string };
  estimated: boolean;
  observedAt: string | null;
  stale: boolean;
  cached: boolean;
  refreshLimited: boolean;
  reason?: "unsupported_provider" | "missing_analytics_token" | "analytics_unavailable" | "missing_account_id";
  message?: string;
}

export interface Overview {
  activeShares: number;
  storageBytes: number;
  downloadsToday: number;
  pendingCleanup: number;
  storage: StorageHealth[];
}

export interface RuntimeSettings {
  maxFileBytes: number;
  maxShareBytes: number;
  maxFilesPerShare: number;
  defaultExpiry: string;
  defaultDownloadLimit: number;
  zipAsyncThresholdBytes: number;
  pairCodeTTLSeconds: number;
  r2BudgetStorageGBMonth: number;
  r2BudgetClassA: number;
  r2BudgetClassB: number;
}

export interface UploadAuthorization { trusted: boolean; source: "invite" | "trusted_device" | "local_system"; deviceName: string }
export interface PairingState { enabled: boolean; code?: string; expiresAt?: string; expired?: boolean }
export interface TrustedDevice { id: string; name: string; platform: string; userAgent: string; expiresAt: string; lastSeenAt: string; revokedAt?: string; createdAt: string; system: boolean; scope: "local" | "cloud" }
