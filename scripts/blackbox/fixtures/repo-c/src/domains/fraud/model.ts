import { isoTimestamp, isBefore } from "../../infra/time";
import { ValidationError } from "../../infra/errors";

/** Durable shape of one fraud signal. */
export interface Fraud {
  readonly id: string;
  readonly tenantId: string;
  status: FraudStatus;
  amountCents: number;
  reference: string;
  labels: readonly string[];
  history: readonly FraudChange[];
  readonly createdAt: string;
  updatedAt: string;
}

export interface FraudChange {
  readonly at: string;
  readonly from: FraudStatus;
  readonly to: FraudStatus;
}

export type FraudStatus = "draft" | "active" | "settled" | "cancelled";

export const fraudStatuses: readonly FraudStatus[] = ["draft", "active", "settled", "cancelled"];

/** Legal forward transitions for a fraud signal; anything else is rejected upstream. */
const transitions: Record<FraudStatus, readonly FraudStatus[]> = {
  draft: ["active", "cancelled"],
  active: ["settled", "cancelled"],
  settled: [],
  cancelled: [],
};

export function canFraudTransition(from: FraudStatus, to: FraudStatus): boolean {
  return transitions[from].includes(to);
}

export function isFraudTerminal(value: Fraud): boolean {
  return transitions[value.status].length === 0;
}

export function newFraud(id: string, tenantId: string, reference: string): Fraud {
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

export function touchFraud(value: Fraud): Fraud {
  return { ...value, updatedAt: isoTimestamp() };
}

/** Applies a transition and records it; callers must check legality first. */
export function applyFraudTransition(value: Fraud, to: FraudStatus): Fraud {
  const change: FraudChange = { at: isoTimestamp(), from: value.status, to };
  return {
    ...value,
    status: to,
    history: [...value.history, change],
    updatedAt: change.at,
  };
}

export function withFraudAmount(value: Fraud, amountCents: number): Fraud {
  if (!Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("fraud amount must be a non-negative integer");
  }
  return touchFraud({ ...value, amountCents });
}

export function withFraudLabel(value: Fraud, label: string): Fraud {
  if (label.trim().length === 0) {
    throw new ValidationError("fraud label must not be blank");
  }
  if (value.labels.includes(label)) {
    return value;
  }
  return touchFraud({ ...value, labels: [...value.labels, label].sort() });
}

export function withoutFraudLabel(value: Fraud, label: string): Fraud {
  if (!value.labels.includes(label)) {
    return value;
  }
  return touchFraud({ ...value, labels: value.labels.filter((item) => item !== label) });
}

export function validateFraud(value: Fraud): void {
  if (value.id.length === 0 || value.tenantId.length === 0) {
    throw new ValidationError("fraud requires both id and tenantId");
  }
  if (value.reference.trim().length === 0) {
    throw new ValidationError("fraud reference must not be blank");
  }
  if (!Number.isInteger(value.amountCents) || value.amountCents < 0) {
    throw new ValidationError("fraud amount must be a non-negative integer");
  }
  if (isBefore(value.updatedAt, value.createdAt)) {
    throw new ValidationError("fraud updatedAt precedes createdAt");
  }
}

export function compareFraud(left: Fraud, right: Fraud): number {
  const byCreation = left.createdAt.localeCompare(right.createdAt);
  return byCreation !== 0 ? byCreation : left.id.localeCompare(right.id);
}

export function summarizeFraud(value: Fraud): string {
  return `${value.reference} (${value.status}, ${(value.amountCents / 100).toFixed(2)})`;
}

export function fraudStatusCounts(values: readonly Fraud[]): Record<FraudStatus, number> {
  const counts: Record<FraudStatus, number> = { draft: 0, active: 0, settled: 0, cancelled: 0 };
  for (const value of values) {
    counts[value.status] += 1;
  }
  return counts;
}
