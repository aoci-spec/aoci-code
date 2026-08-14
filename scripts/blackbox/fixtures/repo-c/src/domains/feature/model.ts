import { isoTimestamp, isBefore } from "../../infra/time";
import { ValidationError } from "../../infra/errors";

/** Durable shape of one feature flag. */
export interface Feature {
  readonly id: string;
  readonly tenantId: string;
  status: FeatureStatus;
  amountCents: number;
  reference: string;
  labels: readonly string[];
  history: readonly FeatureChange[];
  readonly createdAt: string;
  updatedAt: string;
}

export interface FeatureChange {
  readonly at: string;
  readonly from: FeatureStatus;
  readonly to: FeatureStatus;
}

export type FeatureStatus = "draft" | "active" | "settled" | "cancelled";

export const featureStatuses: readonly FeatureStatus[] = ["draft", "active", "settled", "cancelled"];

/** Legal forward transitions for a feature flag; anything else is rejected upstream. */
const transitions: Record<FeatureStatus, readonly FeatureStatus[]> = {
  draft: ["active", "cancelled"],
  active: ["settled", "cancelled"],
  settled: [],
  cancelled: [],
};

export function canFeatureTransition(from: FeatureStatus, to: FeatureStatus): boolean {
  return transitions[from].includes(to);
}

export function isFeatureTerminal(value: Feature): boolean {
  return transitions[value.status].length === 0;
}

export function newFeature(id: string, tenantId: string, reference: string): Feature {
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

export function touchFeature(value: Feature): Feature {
  return { ...value, updatedAt: isoTimestamp() };
}

/** Applies a transition and records it; callers must check legality first. */
export function applyFeatureTransition(value: Feature, to: FeatureStatus): Feature {
  const change: FeatureChange = { at: isoTimestamp(), from: value.status, to };
  return {
    ...value,
    status: to,
    history: [...value.history, change],
    updatedAt: change.at,
  };
}

export function withFeatureAmount(value: Feature, amountCents: number): Feature {
  if (!Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("feature amount must be a non-negative integer");
  }
  return touchFeature({ ...value, amountCents });
}

export function withFeatureLabel(value: Feature, label: string): Feature {
  if (label.trim().length === 0) {
    throw new ValidationError("feature label must not be blank");
  }
  if (value.labels.includes(label)) {
    return value;
  }
  return touchFeature({ ...value, labels: [...value.labels, label].sort() });
}

export function withoutFeatureLabel(value: Feature, label: string): Feature {
  if (!value.labels.includes(label)) {
    return value;
  }
  return touchFeature({ ...value, labels: value.labels.filter((item) => item !== label) });
}

export function validateFeature(value: Feature): void {
  if (value.id.length === 0 || value.tenantId.length === 0) {
    throw new ValidationError("feature requires both id and tenantId");
  }
  if (value.reference.trim().length === 0) {
    throw new ValidationError("feature reference must not be blank");
  }
  if (!Number.isInteger(value.amountCents) || value.amountCents < 0) {
    throw new ValidationError("feature amount must be a non-negative integer");
  }
  if (isBefore(value.updatedAt, value.createdAt)) {
    throw new ValidationError("feature updatedAt precedes createdAt");
  }
}

export function compareFeature(left: Feature, right: Feature): number {
  const byCreation = left.createdAt.localeCompare(right.createdAt);
  return byCreation !== 0 ? byCreation : left.id.localeCompare(right.id);
}

export function summarizeFeature(value: Feature): string {
  return `${value.reference} (${value.status}, ${(value.amountCents / 100).toFixed(2)})`;
}

export function featureStatusCounts(values: readonly Feature[]): Record<FeatureStatus, number> {
  const counts: Record<FeatureStatus, number> = { draft: 0, active: 0, settled: 0, cancelled: 0 };
  for (const value of values) {
    counts[value.status] += 1;
  }
  return counts;
}
