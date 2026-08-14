import { isoTimestamp, isBefore } from "../../infra/time";
import { ValidationError } from "../../infra/errors";

/** Durable shape of one approval decision. */
export interface Approval {
  readonly id: string;
  readonly tenantId: string;
  status: ApprovalStatus;
  amountCents: number;
  reference: string;
  labels: readonly string[];
  history: readonly ApprovalChange[];
  readonly createdAt: string;
  updatedAt: string;
}

export interface ApprovalChange {
  readonly at: string;
  readonly from: ApprovalStatus;
  readonly to: ApprovalStatus;
}

export type ApprovalStatus = "draft" | "active" | "settled" | "cancelled";

export const approvalStatuses: readonly ApprovalStatus[] = ["draft", "active", "settled", "cancelled"];

/** Legal forward transitions for a approval decision; anything else is rejected upstream. */
const transitions: Record<ApprovalStatus, readonly ApprovalStatus[]> = {
  draft: ["active", "cancelled"],
  active: ["settled", "cancelled"],
  settled: [],
  cancelled: [],
};

export function canApprovalTransition(from: ApprovalStatus, to: ApprovalStatus): boolean {
  return transitions[from].includes(to);
}

export function isApprovalTerminal(value: Approval): boolean {
  return transitions[value.status].length === 0;
}

export function newApproval(id: string, tenantId: string, reference: string): Approval {
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

export function touchApproval(value: Approval): Approval {
  return { ...value, updatedAt: isoTimestamp() };
}

/** Applies a transition and records it; callers must check legality first. */
export function applyApprovalTransition(value: Approval, to: ApprovalStatus): Approval {
  const change: ApprovalChange = { at: isoTimestamp(), from: value.status, to };
  return {
    ...value,
    status: to,
    history: [...value.history, change],
    updatedAt: change.at,
  };
}

export function withApprovalAmount(value: Approval, amountCents: number): Approval {
  if (!Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("approval amount must be a non-negative integer");
  }
  return touchApproval({ ...value, amountCents });
}

export function withApprovalLabel(value: Approval, label: string): Approval {
  if (label.trim().length === 0) {
    throw new ValidationError("approval label must not be blank");
  }
  if (value.labels.includes(label)) {
    return value;
  }
  return touchApproval({ ...value, labels: [...value.labels, label].sort() });
}

export function withoutApprovalLabel(value: Approval, label: string): Approval {
  if (!value.labels.includes(label)) {
    return value;
  }
  return touchApproval({ ...value, labels: value.labels.filter((item) => item !== label) });
}

export function validateApproval(value: Approval): void {
  if (value.id.length === 0 || value.tenantId.length === 0) {
    throw new ValidationError("approval requires both id and tenantId");
  }
  if (value.reference.trim().length === 0) {
    throw new ValidationError("approval reference must not be blank");
  }
  if (!Number.isInteger(value.amountCents) || value.amountCents < 0) {
    throw new ValidationError("approval amount must be a non-negative integer");
  }
  if (isBefore(value.updatedAt, value.createdAt)) {
    throw new ValidationError("approval updatedAt precedes createdAt");
  }
}

export function compareApproval(left: Approval, right: Approval): number {
  const byCreation = left.createdAt.localeCompare(right.createdAt);
  return byCreation !== 0 ? byCreation : left.id.localeCompare(right.id);
}

export function summarizeApproval(value: Approval): string {
  return `${value.reference} (${value.status}, ${(value.amountCents / 100).toFixed(2)})`;
}

export function approvalStatusCounts(values: readonly Approval[]): Record<ApprovalStatus, number> {
  const counts: Record<ApprovalStatus, number> = { draft: 0, active: 0, settled: 0, cancelled: 0 };
  for (const value of values) {
    counts[value.status] += 1;
  }
  return counts;
}
