import { isoTimestamp, isBefore } from "../../infra/time";
import { ValidationError } from "../../infra/errors";

/** Durable shape of one aggregated metric. */
export interface Metric {
  readonly id: string;
  readonly tenantId: string;
  status: MetricStatus;
  amountCents: number;
  reference: string;
  labels: readonly string[];
  history: readonly MetricChange[];
  readonly createdAt: string;
  updatedAt: string;
}

export interface MetricChange {
  readonly at: string;
  readonly from: MetricStatus;
  readonly to: MetricStatus;
}

export type MetricStatus = "draft" | "active" | "settled" | "cancelled";

export const metricStatuses: readonly MetricStatus[] = ["draft", "active", "settled", "cancelled"];

/** Legal forward transitions for a aggregated metric; anything else is rejected upstream. */
const transitions: Record<MetricStatus, readonly MetricStatus[]> = {
  draft: ["active", "cancelled"],
  active: ["settled", "cancelled"],
  settled: [],
  cancelled: [],
};

export function canMetricTransition(from: MetricStatus, to: MetricStatus): boolean {
  return transitions[from].includes(to);
}

export function isMetricTerminal(value: Metric): boolean {
  return transitions[value.status].length === 0;
}

export function newMetric(id: string, tenantId: string, reference: string): Metric {
  const now = isoTimestamp();
  return {
    id,
    tenantId,
    status: "draft",
    amountCents: 0,
    reference,
    labels: [],
    history: [],
    createdAt: now,
    updatedAt: now,
  };
}

export function touchMetric(value: Metric): Metric {
  return { ...value, updatedAt: isoTimestamp() };
}

/** Applies a transition and records it; callers must check legality first. */
export function applyMetricTransition(value: Metric, to: MetricStatus): Metric {
  const change: MetricChange = { at: isoTimestamp(), from: value.status, to };
  return {
    ...value,
    status: to,
    history: [...value.history, change],
    updatedAt: change.at,
  };
}

export function withMetricAmount(value: Metric, amountCents: number): Metric {
  if (!Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("metric amount must be a non-negative integer");
  }
  return touchMetric({ ...value, amountCents });
}

export function withMetricLabel(value: Metric, label: string): Metric {
  if (label.trim().length === 0) {
    throw new ValidationError("metric label must not be blank");
  }
  if (value.labels.includes(label)) {
    return value;
  }
  return touchMetric({ ...value, labels: [...value.labels, label].sort() });
}

export function withoutMetricLabel(value: Metric, label: string): Metric {
  if (!value.labels.includes(label)) {
    return value;
  }
  return touchMetric({ ...value, labels: value.labels.filter((item) => item !== label) });
}

export function validateMetric(value: Metric): void {
  if (value.id.length === 0 || value.tenantId.length === 0) {
    throw new ValidationError("metric requires both id and tenantId");
  }
  if (value.reference.trim().length === 0) {
    throw new ValidationError("metric reference must not be blank");
  }
  if (!Number.isInteger(value.amountCents) || value.amountCents < 0) {
    throw new ValidationError("metric amount must be a non-negative integer");
  }
  if (isBefore(value.updatedAt, value.createdAt)) {
    throw new ValidationError("metric updatedAt precedes createdAt");
  }
}

export function compareMetric(left: Metric, right: Metric): number {
  const byCreation = left.createdAt.localeCompare(right.createdAt);
  return byCreation !== 0 ? byCreation : left.id.localeCompare(right.id);
}

export function summarizeMetric(value: Metric): string {
  return `${value.reference} (${value.status}, ${(value.amountCents / 100).toFixed(2)})`;
}

export function metricStatusCounts(values: readonly Metric[]): Record<MetricStatus, number> {
  const counts: Record<MetricStatus, number> = { draft: 0, active: 0, settled: 0, cancelled: 0 };
  for (const value of values) {
    counts[value.status] += 1;
  }
  return counts;
}
