import { isoTimestamp, isBefore } from "../../infra/time";
import { ValidationError } from "../../infra/errors";

/** Durable shape of one return case. */
export interface ReturnCase {
  readonly id: string;
  readonly tenantId: string;
  status: ReturnCaseStatus;
  amountCents: number;
  reference: string;
  labels: readonly string[];
  history: readonly ReturnCaseChange[];
  readonly createdAt: string;
  updatedAt: string;
}

export interface ReturnCaseChange {
  readonly at: string;
  readonly from: ReturnCaseStatus;
  readonly to: ReturnCaseStatus;
}

export type ReturnCaseStatus = "draft" | "active" | "settled" | "cancelled";

export const returnStatuses: readonly ReturnCaseStatus[] = ["draft", "active", "settled", "cancelled"];

/** Legal forward transitions for a return case; anything else is rejected upstream. */
const transitions: Record<ReturnCaseStatus, readonly ReturnCaseStatus[]> = {
  draft: ["active", "cancelled"],
  active: ["settled", "cancelled"],
  settled: [],
  cancelled: [],
};

export function canReturnCaseTransition(from: ReturnCaseStatus, to: ReturnCaseStatus): boolean {
  return transitions[from].includes(to);
}

export function isReturnCaseTerminal(value: ReturnCase): boolean {
  return transitions[value.status].length === 0;
}

export function newReturnCase(id: string, tenantId: string, reference: string): ReturnCase {
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

export function touchReturnCase(value: ReturnCase): ReturnCase {
  return { ...value, updatedAt: isoTimestamp() };
}

/** Applies a transition and records it; callers must check legality first. */
export function applyReturnCaseTransition(value: ReturnCase, to: ReturnCaseStatus): ReturnCase {
  const change: ReturnCaseChange = { at: isoTimestamp(), from: value.status, to };
  return {
    ...value,
    status: to,
    history: [...value.history, change],
    updatedAt: change.at,
  };
}

export function withReturnCaseAmount(value: ReturnCase, amountCents: number): ReturnCase {
  if (!Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("return amount must be a non-negative integer");
  }
  return touchReturnCase({ ...value, amountCents });
}

export function withReturnCaseLabel(value: ReturnCase, label: string): ReturnCase {
  if (label.trim().length === 0) {
    throw new ValidationError("return label must not be blank");
  }
  if (value.labels.includes(label)) {
    return value;
  }
  return touchReturnCase({ ...value, labels: [...value.labels, label].sort() });
}

export function withoutReturnCaseLabel(value: ReturnCase, label: string): ReturnCase {
  if (!value.labels.includes(label)) {
    return value;
  }
  return touchReturnCase({ ...value, labels: value.labels.filter((item) => item !== label) });
}

export function validateReturnCase(value: ReturnCase): void {
  if (value.id.length === 0 || value.tenantId.length === 0) {
    throw new ValidationError("return requires both id and tenantId");
  }
  if (value.reference.trim().length === 0) {
    throw new ValidationError("return reference must not be blank");
  }
  if (!Number.isInteger(value.amountCents) || value.amountCents < 0) {
    throw new ValidationError("return amount must be a non-negative integer");
  }
  if (isBefore(value.updatedAt, value.createdAt)) {
    throw new ValidationError("return updatedAt precedes createdAt");
  }
}

export function compareReturnCase(left: ReturnCase, right: ReturnCase): number {
  const byCreation = left.createdAt.localeCompare(right.createdAt);
  return byCreation !== 0 ? byCreation : left.id.localeCompare(right.id);
}

export function summarizeReturnCase(value: ReturnCase): string {
  return `${value.reference} (${value.status}, ${(value.amountCents / 100).toFixed(2)})`;
}

export function returnStatusCounts(values: readonly ReturnCase[]): Record<ReturnCaseStatus, number> {
  const counts: Record<ReturnCaseStatus, number> = { draft: 0, active: 0, settled: 0, cancelled: 0 };
  for (const value of values) {
    counts[value.status] += 1;
  }
  return counts;
}
