import { Metric, MetricStatus, summarizeMetric } from "./model";
import { MetricPage } from "./repository";
import { MetricSummary } from "./service";

export interface MetricPayload {
  id: string;
  status: string;
  amount: string;
  reference: string;
  labels: readonly string[];
  summary: string;
  updatedAt: string;
}

export interface MetricPagePayload {
  items: readonly MetricPayload[];
  total: number;
  offset: number;
}

export interface MetricSummaryPayload {
  total: number;
  outstanding: number;
  amount: string;
  byStatus: Record<MetricStatus, number>;
}

function money(amountCents: number): string {
  return (amountCents / 100).toFixed(2);
}

/** Wire representation of a aggregated metric; tenant identity never leaves the service. */
export function toMetricPayload(value: Metric): MetricPayload {
  return {
    id: value.id,
    status: value.status,
    amount: money(value.amountCents),
    reference: value.reference,
    labels: value.labels,
    summary: summarizeMetric(value),
    updatedAt: value.updatedAt,
  };
}

export function toMetricPayloads(values: readonly Metric[]): MetricPayload[] {
  return values.map(toMetricPayload);
}

export function toMetricPagePayload(page: MetricPage): MetricPagePayload {
  return { items: toMetricPayloads(page.items), total: page.total, offset: page.offset };
}

export function toMetricSummaryPayload(summary: MetricSummary): MetricSummaryPayload {
  return {
    total: summary.total,
    outstanding: summary.outstanding,
    amount: money(summary.amountCents),
    byStatus: summary.byStatus,
  };
}

export function totalAmountCents(values: readonly Metric[]): number {
  return values.reduce((sum, value) => sum + value.amountCents, 0);
}

export function toMetricCsvRow(value: Metric): string {
  return [value.id, value.status, money(value.amountCents), value.reference].join(",");
}
