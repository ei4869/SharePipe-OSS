import { zhCN } from "./i18n/zh-CN";

export function formatBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes < 0) return zhCN.common.emDash;
  const units = zhCN.byteUnits;
  if (bytes === 0) return "0 " + units[0];
  const index = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1);
  const value = bytes / 1024 ** index;
  const shown = value >= 10 || index === 0 ? value.toFixed(0) : value.toFixed(1);
  return shown + " " + units[index];
}

export function formatExpiry(value: string): string {
  return zhCN.expiryLabel(value);
}

export function remainingTime(expiresAt: string): string {
  const milliseconds = new Date(expiresAt).getTime() - Date.now();
  if (milliseconds <= 0) return zhCN.common.expired;
  const hours = Math.ceil(milliseconds / 3_600_000);
  if (hours < 24) return zhCN.common.remainingHours(hours);
  return zhCN.common.remainingDays(Math.ceil(hours / 24));
}

export function parentPath(value: string): string {
  const index = value.lastIndexOf("/");
  return index === -1 ? "" : value.slice(0, index);
}

export function basename(value: string): string {
  const index = value.lastIndexOf("/");
  return index === -1 ? value : value.slice(index + 1);
}

export function sleep(milliseconds: number): Promise<void> {
  return new Promise((resolve) => window.setTimeout(resolve, milliseconds));
}
