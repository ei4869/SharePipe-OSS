import { AlertCircle, BarChart3, RefreshCw } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { api } from "../../api";
import { zhCN } from "../../i18n/zh-CN";
import type { OperationUsagePoint, StorageProfile, StorageUsage, StorageUsagePoint } from "../../types";
import { formatBytes } from "../../utils";

const number = new Intl.NumberFormat(zhCN.locale, { maximumFractionDigits: 2 });
const integer = new Intl.NumberFormat(zhCN.locale, { maximumFractionDigits: 0 });

export function StorageUsagePanel({ profile, cloud }: { profile?: StorageProfile; cloud: boolean }) {
  const [range, setRange] = useState<"24h" | "7d" | "31d">("31d");
  const [usage, setUsage] = useState<StorageUsage | null>(null);
  const [problem, setProblem] = useState("");
  const [busy, setBusy] = useState(false);
  const isR2 = profile?.provider === "cloudflare-r2" || profile?.provider === "cloudflare-r2-binding";

  const load = useCallback(async (refresh = false) => {
    if (!profile || !isR2 || !cloud) return;
    setBusy(true);
    setProblem("");
    try {
      setUsage(await api.storageUsage(profile.id, range, refresh));
    } catch (error) {
      setProblem(error instanceof Error ? error.message : zhCN.storageUsage.loadFailed);
    } finally {
      setBusy(false);
    }
  }, [cloud, isR2, profile, range]);

  useEffect(() => {
    void load();
    if (!profile || !isR2 || !cloud) return;
    const timer = window.setInterval(() => void load(), 10 * 60 * 1000);
    return () => window.clearInterval(timer);
  }, [cloud, isR2, load, profile]);

  if (!profile) return null;
  if (!cloud || !isR2) {
    return (
      <section className="admin-section storage-usage-empty">
        <BarChart3 size={22} />
        <div><h2>{zhCN.storageUsage.title}</h2><p>{zhCN.storageUsage.unsupported}</p></div>
      </section>
    );
  }

  return (
    <section className="admin-section storage-usage-panel">
      <div className="section-title-row storage-usage-heading">
        <div><h2>{zhCN.storageUsage.title}</h2><p>{zhCN.storageUsage.lead}</p></div>
        <button className="secondary-button" type="button" disabled={busy} onClick={() => void load(true)}>
          <RefreshCw size={16} className={busy ? "is-spinning" : ""} />
          {busy ? zhCN.storageUsage.refreshing : zhCN.storageUsage.refresh}
        </button>
      </div>
      <div className="scope-switch storage-range-switch" role="group" aria-label={zhCN.storageUsage.rangeLabel}>
        {(["24h", "7d", "31d"] as const).map((value) => (
          <button key={value} type="button" className={range === value ? "is-active" : ""} onClick={() => setRange(value)}>
            {value === "24h" ? zhCN.storageUsage.range24h : value === "7d" ? zhCN.storageUsage.range7d : zhCN.storageUsage.range31d}
          </button>
        ))}
      </div>
      {problem && <div className="usage-message is-error"><AlertCircle size={18} /><span>{problem}</span></div>}
      {usage && !usage.available && <div className="usage-message"><AlertCircle size={18} /><span>{usage.message || zhCN.storageUsage.unavailable}</span></div>}
      {usage?.available && <UsageContent usage={usage} />}
    </section>
  );
}

function UsageContent({ usage }: { usage: StorageUsage }) {
  const status = [
    usage.cached ? zhCN.storageUsage.cached : "",
    usage.stale ? zhCN.storageUsage.stale : "",
    usage.refreshLimited ? zhCN.storageUsage.refreshLimited : "",
  ].filter(Boolean).join(" · ");
  return (
    <>
      <div className="usage-summary-grid">
        <UsageMetric label={zhCN.storageUsage.currentStorage} value={formatBytes((usage.current?.payloadBytes ?? 0) + (usage.current?.metadataBytes ?? 0))} detail={`${zhCN.storageUsage.metadata} ${formatBytes(usage.current?.metadataBytes ?? 0)}`} />
        <UsageMetric label={zhCN.storageUsage.objects} value={integer.format(usage.current?.objectCount ?? 0)} detail={`${zhCN.storageUsage.incompleteUploads} ${integer.format(usage.current?.uploadCount ?? 0)}`} />
        <UsageMetric label={zhCN.storageUsage.classAMonth} value={integer.format(usage.monthToDate.classA)} detail={`${zhCN.storageUsage.free} ${integer.format(usage.monthToDate.free)}`} />
        <UsageMetric label={zhCN.storageUsage.classBMonth} value={integer.format(usage.monthToDate.classB)} detail={`${zhCN.storageUsage.unclassified} ${integer.format(usage.monthToDate.unclassified)}`} />
      </div>
      <p className="usage-observed">{zhCN.storageUsage.lastObserved}：{usage.observedAt ? new Date(usage.observedAt).toLocaleString(zhCN.locale) : zhCN.storageUsage.noObservation}{status ? ` · ${status}` : ""}</p>
      <div className="usage-budget-grid">
        <BudgetProgress label={zhCN.storageUsage.storageBudget} used={usage.monthToDate.estimatedStorageGBMonth} budget={usage.budget.storageGBMonth} formatter={(value) => `${number.format(value)} GB-month`} />
        <BudgetProgress label={zhCN.storageUsage.classABudget} used={usage.monthToDate.classA} budget={usage.budget.classA} formatter={(value) => integer.format(value)} />
        <BudgetProgress label={zhCN.storageUsage.classBBudget} used={usage.monthToDate.classB} budget={usage.budget.classB} formatter={(value) => integer.format(value)} />
      </div>
      <div className="usage-chart-grid">
        <StorageAreaChart points={usage.storageSeries} />
        <OperationsChart points={usage.operationSeries} />
      </div>
      <details className="usage-data-table">
        <summary>{zhCN.storageUsage.accessibleData}</summary>
        <div className="table-shell"><table><thead><tr><th>{zhCN.storageUsage.date}</th><th>{zhCN.storageUsage.payload}</th><th>{zhCN.storageUsage.metadata}</th><th>{zhCN.storageUsage.objects}</th><th>{zhCN.storageUsage.classA}</th><th>{zhCN.storageUsage.classB}</th><th>{zhCN.storageUsage.free}</th><th>{zhCN.storageUsage.unclassified}</th></tr></thead><tbody>
          {mergeRows(usage.storageSeries, usage.operationSeries).map((row) => <tr key={row.date}><td>{row.date}</td><td>{formatBytes(row.payloadBytes)}</td><td>{formatBytes(row.metadataBytes)}</td><td>{integer.format(row.objectCount)}</td><td>{integer.format(row.classA)}</td><td>{integer.format(row.classB)}</td><td>{integer.format(row.free)}</td><td>{integer.format(row.unclassified)}</td></tr>)}
        </tbody></table></div>
      </details>
      <div className="usage-notices">
        <p>{zhCN.storageUsage.estimateNotice(usage.budget.referenceDate)}</p>
        <p>{zhCN.storageUsage.scopeNotice(usage.profile.bucket, usage.profile.prefix)}</p>
      </div>
    </>
  );
}

function UsageMetric({ label, value, detail }: { label: string; value: string; detail: string }) {
  return <div><span>{label}</span><strong>{value}</strong><small>{detail}</small></div>;
}

function BudgetProgress({ label, used, budget, formatter }: { label: string; used: number; budget: number; formatter: (value: number) => string }) {
  const ratio = budget > 0 ? used / budget : 0;
  const percent = Math.min(100, Math.max(0, ratio * 100));
  const level = ratio >= .95 ? "critical" : ratio >= .85 ? "danger" : ratio >= .7 ? "warning" : "normal";
  const message = level === "critical" ? zhCN.storageUsage.warning95 : level === "danger" ? zhCN.storageUsage.warning85 : level === "warning" ? zhCN.storageUsage.warning70 : zhCN.storageUsage.withinBudget;
  return <div className={`usage-budget is-${level}`}><div><strong>{label}</strong><span>{zhCN.storageUsage.budgetStatus(formatter(used), formatter(budget))}</span></div><div className="usage-progress" role="progressbar" aria-label={label} aria-valuemin={0} aria-valuemax={100} aria-valuenow={Math.round(percent)}><span style={{ width: `${percent}%` }} /></div><small>{message}</small></div>;
}

function StorageAreaChart({ points }: { points: StorageUsagePoint[] }) {
  const chart = useMemo(() => areaPath(points), [points]);
  return <figure className="usage-chart"><figcaption>{zhCN.storageUsage.storageTrend}</figcaption>{points.length === 0 ? <p>{zhCN.storageUsage.noData}</p> : <svg viewBox="0 0 600 180" role="img" aria-label={zhCN.storageUsage.storageTrend}><path className="usage-area" d={chart.area} /><path className="usage-line" d={chart.line} />{chart.points.map((point) => <circle key={point.date} cx={point.x} cy={point.y} r="4" tabIndex={0}><title>{point.date}：{formatBytes(point.value)}</title></circle>)}</svg>}</figure>;
}

function OperationsChart({ points }: { points: OperationUsagePoint[] }) {
  const maximum = Math.max(1, ...points.map((point) => point.classA + point.classB + point.free + point.unclassified));
  return <figure className="usage-chart"><figcaption>{zhCN.storageUsage.operationTrend}</figcaption>{points.length === 0 ? <p>{zhCN.storageUsage.noData}</p> : <><div className="usage-chart-legend" aria-label={zhCN.storageUsage.operationLegend}><span className="is-class-a">{zhCN.storageUsage.classA}</span><span className="is-class-b">{zhCN.storageUsage.classB}</span><span className="is-free">{zhCN.storageUsage.free}</span><span className="is-unclassified">{zhCN.storageUsage.unclassified}</span></div><div className="usage-operation-bars">{points.map((point) => { const total = point.classA + point.classB + point.free + point.unclassified; return <div className="usage-operation-day" key={point.date} tabIndex={0} aria-label={`${point.date}：${integer.format(total)}`}><span className="is-class-a" style={{ height: `${point.classA / maximum * 100}%` }} /><span className="is-class-b" style={{ height: `${point.classB / maximum * 100}%` }} /><span className="is-free" style={{ height: `${point.free / maximum * 100}%` }} /><span className="is-unclassified" style={{ height: `${point.unclassified / maximum * 100}%` }} /><small>{point.date.slice(5)}</small><div className="usage-tooltip">{zhCN.storageUsage.classA} {integer.format(point.classA)} · {zhCN.storageUsage.classB} {integer.format(point.classB)} · {zhCN.storageUsage.free} {integer.format(point.free)} · {zhCN.storageUsage.unclassified} {integer.format(point.unclassified)}</div></div>; })}</div></>}</figure>;
}

function areaPath(points: StorageUsagePoint[]) {
  const values = points.map((point) => point.payloadBytes + point.metadataBytes);
  const maximum = Math.max(1, ...values);
  const left = 28, right = 572, top = 18, bottom = 150;
  const mapped = points.map((point, index) => ({ date: point.date, value: values[index] ?? 0, x: points.length === 1 ? (left + right) / 2 : left + index / (points.length - 1) * (right - left), y: bottom - (values[index] ?? 0) / maximum * (bottom - top) }));
  const line = mapped.map((point, index) => `${index === 0 ? "M" : "L"}${point.x},${point.y}`).join(" ");
  const area = mapped.length ? `M${mapped[0]?.x},${bottom} ${line} L${mapped.at(-1)?.x},${bottom} Z` : "";
  return { points: mapped, line, area };
}

function mergeRows(storage: StorageUsagePoint[], operations: OperationUsagePoint[]) {
  const rows = new Map<string, StorageUsagePoint & OperationUsagePoint>();
  for (const point of storage) rows.set(point.date, { ...point, classA: 0, classB: 0, free: 0, unclassified: 0 });
  for (const point of operations) rows.set(point.date, { payloadBytes: 0, metadataBytes: 0, objectCount: 0, uploadCount: 0, ...(rows.get(point.date) ?? {}), ...point });
  return [...rows.values()].sort((left, right) => left.date.localeCompare(right.date));
}
