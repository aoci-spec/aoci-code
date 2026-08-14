import { isoTimestamp, isBefore } from "../../infra/time";
import { ValidationError } from "../../infra/errors";

/** Durable shape of one consumption quota. */
export interface Quota {
  readonly id: string;
  readonly tenantId: string;
  status: QuotaStatus;
  amountCents: number;
  reference: string;
  labels: readonly string[];
  history: readonly QuotaChange[];
  readonly createdAt: string;
  updatedAt: string;
}

export interface QuotaChange {
  readonly at: string;
  readonly from: QuotaStatus;
  readonly to: QuotaStatus;
}

export type QuotaStatus = "draft" | "active" | "settled" | "cancelled";

export const quotaStatuses: readonly QuotaStatus[] = ["draft", "active", "settled", "cancelled"];

/** Legal forward transitions for a consumption quota; anything else is rejected upstream. */
const transitions: Record<QuotaStatus, readonly QuotaStatus[]> = {
  draft: ["active", "cancelled"],
  active: ["settled", "cancelled"],
  settled: [],
  cancelled: [],
};

export function canQuotaTransition(from: QuotaStatus, to: QuotaStatus): boolean {
  return transitions[from].includes(to);
}

export function isQuotaTerminal(value: Quota): boolean {
  return transitions[value.status].length === 0;
}

export function newQuota(id: string, tenantId: string, reference: string): Quota {
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

export function touchQuota(value: Quota): Quota {
  return { ...value, updatedAt: isoTimestamp() };
}

/** Applies a transition and records it; callers must check legality first. */
export function applyQuotaTransition(value: Quota, to: QuotaStatus): Quota {
  const change: QuotaChange = { at: isoTimestamp(), from: value.status, to };
  return {
    ...value,
    status: to,
    history: [...value.history, change],
    updatedAt: change.at,
  };
}

export function withQuotaAmount(value: Quota, amountCents: number): Quota {
  if (!Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("quota amount must be a non-negative integer");
  }
  return touchQuota({ ...value, amountCents });
}

export function withQuotaLabel(value: Quota, label: string): Quota {
  if (label.trim().length === 0) {
    throw new ValidationError("quota label must not be blank");
  }
  if (value.labels.includes(label)) {
    return value;
  }
  return touchQuota({ ...value, labels: [...value.labels, label].sort() });
}

export function withoutQuotaLabel(value: Quota, label: string): Quota {
  if (!value.labels.includes(label)) {
    return value;
  }
  return touchQuota({ ...value, labels: value.labels.filter((item) => item !== label) });
}

export function validateQuota(value: Quota): void {
  if (value.id.length === 0 || value.tenantId.length === 0) {
    throw new ValidationError("quota requires both id and tenantId");
  }
  if (value.reference.trim().length === 0) {
    throw new ValidationError("quota reference must not be blank");
  }
  if (!Number.isInteger(value.amountCents) || value.amountCents < 0) {
    throw new ValidationError("quota amount must be a non-negative integer");
  }
  if (isBefore(value.updatedAt, value.createdAt)) {
    throw new ValidationError("quota updatedAt precedes createdAt");
  }
}

export function compareQuota(left: Quota, right: Quota): number {
  const byCreation = left.createdAt.localeCompare(right.createdAt);
  return byCreation !== 0 ? byCreation : left.id.localeCompare(right.id);
}

export function summarizeQuota(value: Quota): string {
  return `${value.reference} (${value.status}, ${(value.amountCents / 100).toFixed(2)})`;
}

export function quotaStatusCounts(values: readonly Quota[]): Record<QuotaStatus, number> {
  const counts: Record<QuotaStatus, number> = { draft: 0, active: 0, settled: 0, cancelled: 0 };
  for (const value of values) {
    counts[value.status] += 1;
  }
  return counts;
}
