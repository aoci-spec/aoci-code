import { isoTimestamp, isBefore } from "../../infra/time";
import { ValidationError } from "../../infra/errors";

/** Durable shape of one merchant payout. */
export interface Payout {
  readonly id: string;
  readonly tenantId: string;
  status: PayoutStatus;
  amountCents: number;
  reference: string;
  labels: readonly string[];
  history: readonly PayoutChange[];
  readonly createdAt: string;
  updatedAt: string;
}

export interface PayoutChange {
  readonly at: string;
  readonly from: PayoutStatus;
  readonly to: PayoutStatus;
}

export type PayoutStatus = "draft" | "active" | "settled" | "cancelled";

export const payoutStatuses: readonly PayoutStatus[] = ["draft", "active", "settled", "cancelled"];

/** Legal forward transitions for a merchant payout; anything else is rejected upstream. */
const transitions: Record<PayoutStatus, readonly PayoutStatus[]> = {
  draft: ["active", "cancelled"],
  active: ["settled", "cancelled"],
  settled: [],
  cancelled: [],
};

export function canPayoutTransition(from: PayoutStatus, to: PayoutStatus): boolean {
  return transitions[from].includes(to);
}

export function isPayoutTerminal(value: Payout): boolean {
  return transitions[value.status].length === 0;
}

export function newPayout(id: string, tenantId: string, reference: string): Payout {
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

export function touchPayout(value: Payout): Payout {
  return { ...value, updatedAt: isoTimestamp() };
}

/** Applies a transition and records it; callers must check legality first. */
export function applyPayoutTransition(value: Payout, to: PayoutStatus): Payout {
  const change: PayoutChange = { at: isoTimestamp(), from: value.status, to };
  return {
    ...value,
    status: to,
    history: [...value.history, change],
    updatedAt: change.at,
  };
}

export function withPayoutAmount(value: Payout, amountCents: number): Payout {
  if (!Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("payout amount must be a non-negative integer");
  }
  return touchPayout({ ...value, amountCents });
}

export function withPayoutLabel(value: Payout, label: string): Payout {
  if (label.trim().length === 0) {
    throw new ValidationError("payout label must not be blank");
  }
  if (value.labels.includes(label)) {
    return value;
  }
  return touchPayout({ ...value, labels: [...value.labels, label].sort() });
}

export function withoutPayoutLabel(value: Payout, label: string): Payout {
  if (!value.labels.includes(label)) {
    return value;
  }
  return touchPayout({ ...value, labels: value.labels.filter((item) => item !== label) });
}

export function validatePayout(value: Payout): void {
  if (value.id.length === 0 || value.tenantId.length === 0) {
    throw new ValidationError("payout requires both id and tenantId");
  }
  if (value.reference.trim().length === 0) {
    throw new ValidationError("payout reference must not be blank");
  }
  if (!Number.isInteger(value.amountCents) || value.amountCents < 0) {
    throw new ValidationError("payout amount must be a non-negative integer");
  }
  if (isBefore(value.updatedAt, value.createdAt)) {
    throw new ValidationError("payout updatedAt precedes createdAt");
  }
}

export function comparePayout(left: Payout, right: Payout): number {
  const byCreation = left.createdAt.localeCompare(right.createdAt);
  return byCreation !== 0 ? byCreation : left.id.localeCompare(right.id);
}

export function summarizePayout(value: Payout): string {
  return `${value.reference} (${value.status}, ${(value.amountCents / 100).toFixed(2)})`;
}

export function payoutStatusCounts(values: readonly Payout[]): Record<PayoutStatus, number> {
  const counts: Record<PayoutStatus, number> = { draft: 0, active: 0, settled: 0, cancelled: 0 };
  for (const value of values) {
    counts[value.status] += 1;
  }
  return counts;
}
