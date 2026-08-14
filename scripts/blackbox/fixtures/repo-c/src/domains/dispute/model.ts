import { isoTimestamp, isBefore } from "../../infra/time";
import { ValidationError } from "../../infra/errors";

/** Durable shape of one payment dispute. */
export interface Dispute {
  readonly id: string;
  readonly tenantId: string;
  status: DisputeStatus;
  amountCents: number;
  reference: string;
  labels: readonly string[];
  history: readonly DisputeChange[];
  readonly createdAt: string;
  updatedAt: string;
}

export interface DisputeChange {
  readonly at: string;
  readonly from: DisputeStatus;
  readonly to: DisputeStatus;
}

export type DisputeStatus = "draft" | "active" | "settled" | "cancelled";

export const disputeStatuses: readonly DisputeStatus[] = ["draft", "active", "settled", "cancelled"];

/** Legal forward transitions for a payment dispute; anything else is rejected upstream. */
const transitions: Record<DisputeStatus, readonly DisputeStatus[]> = {
  draft: ["active", "cancelled"],
  active: ["settled", "cancelled"],
  settled: [],
  cancelled: [],
};

export function canDisputeTransition(from: DisputeStatus, to: DisputeStatus): boolean {
  return transitions[from].includes(to);
}

export function isDisputeTerminal(value: Dispute): boolean {
  return transitions[value.status].length === 0;
}

export function newDispute(id: string, tenantId: string, reference: string): Dispute {
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

export function touchDispute(value: Dispute): Dispute {
  return { ...value, updatedAt: isoTimestamp() };
}

/** Applies a transition and records it; callers must check legality first. */
export function applyDisputeTransition(value: Dispute, to: DisputeStatus): Dispute {
  const change: DisputeChange = { at: isoTimestamp(), from: value.status, to };
  return {
    ...value,
    status: to,
    history: [...value.history, change],
    updatedAt: change.at,
  };
}

export function withDisputeAmount(value: Dispute, amountCents: number): Dispute {
  if (!Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("dispute amount must be a non-negative integer");
  }
  return touchDispute({ ...value, amountCents });
}

export function withDisputeLabel(value: Dispute, label: string): Dispute {
  if (label.trim().length === 0) {
    throw new ValidationError("dispute label must not be blank");
  }
  if (value.labels.includes(label)) {
    return value;
  }
  return touchDispute({ ...value, labels: [...value.labels, label].sort() });
}

export function withoutDisputeLabel(value: Dispute, label: string): Dispute {
  if (!value.labels.includes(label)) {
    return value;
  }
  return touchDispute({ ...value, labels: value.labels.filter((item) => item !== label) });
}

export function validateDispute(value: Dispute): void {
  if (value.id.length === 0 || value.tenantId.length === 0) {
    throw new ValidationError("dispute requires both id and tenantId");
  }
  if (value.reference.trim().length === 0) {
    throw new ValidationError("dispute reference must not be blank");
  }
  if (!Number.isInteger(value.amountCents) || value.amountCents < 0) {
    throw new ValidationError("dispute amount must be a non-negative integer");
  }
  if (isBefore(value.updatedAt, value.createdAt)) {
    throw new ValidationError("dispute updatedAt precedes createdAt");
  }
}

export function compareDispute(left: Dispute, right: Dispute): number {
  const byCreation = left.createdAt.localeCompare(right.createdAt);
  return byCreation !== 0 ? byCreation : left.id.localeCompare(right.id);
}

export function summarizeDispute(value: Dispute): string {
  return `${value.reference} (${value.status}, ${(value.amountCents / 100).toFixed(2)})`;
}

export function disputeStatusCounts(values: readonly Dispute[]): Record<DisputeStatus, number> {
  const counts: Record<DisputeStatus, number> = { draft: 0, active: 0, settled: 0, cancelled: 0 };
  for (const value of values) {
    counts[value.status] += 1;
  }
  return counts;
}
