import { isoTimestamp, isBefore } from "../../infra/time";
import { ValidationError } from "../../infra/errors";

/** Durable shape of one settlement run. */
export interface Settlement {
  readonly id: string;
  readonly tenantId: string;
  status: SettlementStatus;
  amountCents: number;
  reference: string;
  labels: readonly string[];
  history: readonly SettlementChange[];
  readonly createdAt: string;
  updatedAt: string;
}

export interface SettlementChange {
  readonly at: string;
  readonly from: SettlementStatus;
  readonly to: SettlementStatus;
}

export type SettlementStatus = "draft" | "active" | "settled" | "cancelled";

export const settlementStatuses: readonly SettlementStatus[] = ["draft", "active", "settled", "cancelled"];

/** Legal forward transitions for a settlement run; anything else is rejected upstream. */
const transitions: Record<SettlementStatus, readonly SettlementStatus[]> = {
  draft: ["active", "cancelled"],
  active: ["settled", "cancelled"],
  settled: [],
  cancelled: [],
};

export function canSettlementTransition(from: SettlementStatus, to: SettlementStatus): boolean {
  return transitions[from].includes(to);
}

export function isSettlementTerminal(value: Settlement): boolean {
  return transitions[value.status].length === 0;
}

export function newSettlement(id: string, tenantId: string, reference: string): Settlement {
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

export function touchSettlement(value: Settlement): Settlement {
  return { ...value, updatedAt: isoTimestamp() };
}

/** Applies a transition and records it; callers must check legality first. */
export function applySettlementTransition(value: Settlement, to: SettlementStatus): Settlement {
  const change: SettlementChange = { at: isoTimestamp(), from: value.status, to };
  return {
    ...value,
    status: to,
    history: [...value.history, change],
    updatedAt: change.at,
  };
}

export function withSettlementAmount(value: Settlement, amountCents: number): Settlement {
  if (!Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("settlement amount must be a non-negative integer");
  }
  return touchSettlement({ ...value, amountCents });
}

export function withSettlementLabel(value: Settlement, label: string): Settlement {
  if (label.trim().length === 0) {
    throw new ValidationError("settlement label must not be blank");
  }
  if (value.labels.includes(label)) {
    return value;
  }
  return touchSettlement({ ...value, labels: [...value.labels, label].sort() });
}

export function withoutSettlementLabel(value: Settlement, label: string): Settlement {
  if (!value.labels.includes(label)) {
    return value;
  }
  return touchSettlement({ ...value, labels: value.labels.filter((item) => item !== label) });
}

export function validateSettlement(value: Settlement): void {
  if (value.id.length === 0 || value.tenantId.length === 0) {
    throw new ValidationError("settlement requires both id and tenantId");
  }
  if (value.reference.trim().length === 0) {
    throw new ValidationError("settlement reference must not be blank");
  }
  if (!Number.isInteger(value.amountCents) || value.amountCents < 0) {
    throw new ValidationError("settlement amount must be a non-negative integer");
  }
  if (isBefore(value.updatedAt, value.createdAt)) {
    throw new ValidationError("settlement updatedAt precedes createdAt");
  }
}

export function compareSettlement(left: Settlement, right: Settlement): number {
  const byCreation = left.createdAt.localeCompare(right.createdAt);
  return byCreation !== 0 ? byCreation : left.id.localeCompare(right.id);
}

export function summarizeSettlement(value: Settlement): string {
  return `${value.reference} (${value.status}, ${(value.amountCents / 100).toFixed(2)})`;
}

export function settlementStatusCounts(values: readonly Settlement[]): Record<SettlementStatus, number> {
  const counts: Record<SettlementStatus, number> = { draft: 0, active: 0, settled: 0, cancelled: 0 };
  for (const value of values) {
    counts[value.status] += 1;
  }
  return counts;
}
