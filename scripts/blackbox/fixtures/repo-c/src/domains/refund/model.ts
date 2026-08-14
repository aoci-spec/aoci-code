import { isoTimestamp, isBefore } from "../../infra/time";
import { ValidationError } from "../../infra/errors";

/** Durable shape of one refund request. */
export interface Refund {
  readonly id: string;
  readonly tenantId: string;
  status: RefundStatus;
  amountCents: number;
  reference: string;
  labels: readonly string[];
  history: readonly RefundChange[];
  readonly createdAt: string;
  updatedAt: string;
}

export interface RefundChange {
  readonly at: string;
  readonly from: RefundStatus;
  readonly to: RefundStatus;
}

export type RefundStatus = "draft" | "active" | "settled" | "cancelled";

export const refundStatuses: readonly RefundStatus[] = ["draft", "active", "settled", "cancelled"];

/** Legal forward transitions for a refund request; anything else is rejected upstream. */
const transitions: Record<RefundStatus, readonly RefundStatus[]> = {
  draft: ["active", "cancelled"],
  active: ["settled", "cancelled"],
  settled: [],
  cancelled: [],
};

export function canRefundTransition(from: RefundStatus, to: RefundStatus): boolean {
  return transitions[from].includes(to);
}

export function isRefundTerminal(value: Refund): boolean {
  return transitions[value.status].length === 0;
}

export function newRefund(id: string, tenantId: string, reference: string): Refund {
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

export function touchRefund(value: Refund): Refund {
  return { ...value, updatedAt: isoTimestamp() };
}

/** Applies a transition and records it; callers must check legality first. */
export function applyRefundTransition(value: Refund, to: RefundStatus): Refund {
  const change: RefundChange = { at: isoTimestamp(), from: value.status, to };
  return {
    ...value,
    status: to,
    history: [...value.history, change],
    updatedAt: change.at,
  };
}

export function withRefundAmount(value: Refund, amountCents: number): Refund {
  if (!Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("refund amount must be a non-negative integer");
  }
  return touchRefund({ ...value, amountCents });
}

export function withRefundLabel(value: Refund, label: string): Refund {
  if (label.trim().length === 0) {
    throw new ValidationError("refund label must not be blank");
  }
  if (value.labels.includes(label)) {
    return value;
  }
  return touchRefund({ ...value, labels: [...value.labels, label].sort() });
}

export function withoutRefundLabel(value: Refund, label: string): Refund {
  if (!value.labels.includes(label)) {
    return value;
  }
  return touchRefund({ ...value, labels: value.labels.filter((item) => item !== label) });
}

export function validateRefund(value: Refund): void {
  if (value.id.length === 0 || value.tenantId.length === 0) {
    throw new ValidationError("refund requires both id and tenantId");
  }
  if (value.reference.trim().length === 0) {
    throw new ValidationError("refund reference must not be blank");
  }
  if (!Number.isInteger(value.amountCents) || value.amountCents < 0) {
    throw new ValidationError("refund amount must be a non-negative integer");
  }
  if (isBefore(value.updatedAt, value.createdAt)) {
    throw new ValidationError("refund updatedAt precedes createdAt");
  }
}

export function compareRefund(left: Refund, right: Refund): number {
  const byCreation = left.createdAt.localeCompare(right.createdAt);
  return byCreation !== 0 ? byCreation : left.id.localeCompare(right.id);
}

export function summarizeRefund(value: Refund): string {
  return `${value.reference} (${value.status}, ${(value.amountCents / 100).toFixed(2)})`;
}

export function refundStatusCounts(values: readonly Refund[]): Record<RefundStatus, number> {
  const counts: Record<RefundStatus, number> = { draft: 0, active: 0, settled: 0, cancelled: 0 };
  for (const value of values) {
    counts[value.status] += 1;
  }
  return counts;
}
