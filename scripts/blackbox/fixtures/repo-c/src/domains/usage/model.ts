import { isoTimestamp, isBefore } from "../../infra/time";
import { ValidationError } from "../../infra/errors";

/** Durable shape of one metered usage record. */
export interface Usage {
  readonly id: string;
  readonly tenantId: string;
  status: UsageStatus;
  amountCents: number;
  reference: string;
  labels: readonly string[];
  history: readonly UsageChange[];
  readonly createdAt: string;
  updatedAt: string;
}

export interface UsageChange {
  readonly at: string;
  readonly from: UsageStatus;
  readonly to: UsageStatus;
}

export type UsageStatus = "draft" | "active" | "settled" | "cancelled";

export const usageStatuses: readonly UsageStatus[] = ["draft", "active", "settled", "cancelled"];

/** Legal forward transitions for a metered usage record; anything else is rejected upstream. */
const transitions: Record<UsageStatus, readonly UsageStatus[]> = {
  draft: ["active", "cancelled"],
  active: ["settled", "cancelled"],
  settled: [],
  cancelled: [],
};

export function canUsageTransition(from: UsageStatus, to: UsageStatus): boolean {
  return transitions[from].includes(to);
}

export function isUsageTerminal(value: Usage): boolean {
  return transitions[value.status].length === 0;
}

export function newUsage(id: string, tenantId: string, reference: string): Usage {
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

export function touchUsage(value: Usage): Usage {
  return { ...value, updatedAt: isoTimestamp() };
}

/** Applies a transition and records it; callers must check legality first. */
export function applyUsageTransition(value: Usage, to: UsageStatus): Usage {
  const change: UsageChange = { at: isoTimestamp(), from: value.status, to };
  return {
    ...value,
    status: to,
    history: [...value.history, change],
    updatedAt: change.at,
  };
}

export function withUsageAmount(value: Usage, amountCents: number): Usage {
  if (!Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("usage amount must be a non-negative integer");
  }
  return touchUsage({ ...value, amountCents });
}

export function withUsageLabel(value: Usage, label: string): Usage {
  if (label.trim().length === 0) {
    throw new ValidationError("usage label must not be blank");
  }
  if (value.labels.includes(label)) {
    return value;
  }
  return touchUsage({ ...value, labels: [...value.labels, label].sort() });
}

export function withoutUsageLabel(value: Usage, label: string): Usage {
  if (!value.labels.includes(label)) {
    return value;
  }
  return touchUsage({ ...value, labels: value.labels.filter((item) => item !== label) });
}

export function validateUsage(value: Usage): void {
  if (value.id.length === 0 || value.tenantId.length === 0) {
    throw new ValidationError("usage requires both id and tenantId");
  }
  if (value.reference.trim().length === 0) {
    throw new ValidationError("usage reference must not be blank");
  }
  if (!Number.isInteger(value.amountCents) || value.amountCents < 0) {
    throw new ValidationError("usage amount must be a non-negative integer");
  }
  if (isBefore(value.updatedAt, value.createdAt)) {
    throw new ValidationError("usage updatedAt precedes createdAt");
  }
}

export function compareUsage(left: Usage, right: Usage): number {
  const byCreation = left.createdAt.localeCompare(right.createdAt);
  return byCreation !== 0 ? byCreation : left.id.localeCompare(right.id);
}

export function summarizeUsage(value: Usage): string {
  return `${value.reference} (${value.status}, ${(value.amountCents / 100).toFixed(2)})`;
}

export function usageStatusCounts(values: readonly Usage[]): Record<UsageStatus, number> {
  const counts: Record<UsageStatus, number> = { draft: 0, active: 0, settled: 0, cancelled: 0 };
  for (const value of values) {
    counts[value.status] += 1;
  }
  return counts;
}
