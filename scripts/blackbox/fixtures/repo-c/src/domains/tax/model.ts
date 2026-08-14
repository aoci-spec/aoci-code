import { isoTimestamp, isBefore } from "../../infra/time";
import { ValidationError } from "../../infra/errors";

/** Durable shape of one tax determination. */
export interface Tax {
  readonly id: string;
  readonly tenantId: string;
  status: TaxStatus;
  amountCents: number;
  reference: string;
  labels: readonly string[];
  history: readonly TaxChange[];
  readonly createdAt: string;
  updatedAt: string;
}

export interface TaxChange {
  readonly at: string;
  readonly from: TaxStatus;
  readonly to: TaxStatus;
}

export type TaxStatus = "draft" | "active" | "settled" | "cancelled";

export const taxStatuses: readonly TaxStatus[] = ["draft", "active", "settled", "cancelled"];

/** Legal forward transitions for a tax determination; anything else is rejected upstream. */
const transitions: Record<TaxStatus, readonly TaxStatus[]> = {
  draft: ["active", "cancelled"],
  active: ["settled", "cancelled"],
  settled: [],
  cancelled: [],
};

export function canTaxTransition(from: TaxStatus, to: TaxStatus): boolean {
  return transitions[from].includes(to);
}

export function isTaxTerminal(value: Tax): boolean {
  return transitions[value.status].length === 0;
}

export function newTax(id: string, tenantId: string, reference: string): Tax {
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

export function touchTax(value: Tax): Tax {
  return { ...value, updatedAt: isoTimestamp() };
}

/** Applies a transition and records it; callers must check legality first. */
export function applyTaxTransition(value: Tax, to: TaxStatus): Tax {
  const change: TaxChange = { at: isoTimestamp(), from: value.status, to };
  return {
    ...value,
    status: to,
    history: [...value.history, change],
    updatedAt: change.at,
  };
}

export function withTaxAmount(value: Tax, amountCents: number): Tax {
  if (!Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("tax amount must be a non-negative integer");
  }
  return touchTax({ ...value, amountCents });
}

export function withTaxLabel(value: Tax, label: string): Tax {
  if (label.trim().length === 0) {
    throw new ValidationError("tax label must not be blank");
  }
  if (value.labels.includes(label)) {
    return value;
  }
  return touchTax({ ...value, labels: [...value.labels, label].sort() });
}

export function withoutTaxLabel(value: Tax, label: string): Tax {
  if (!value.labels.includes(label)) {
    return value;
  }
  return touchTax({ ...value, labels: value.labels.filter((item) => item !== label) });
}

export function validateTax(value: Tax): void {
  if (value.id.length === 0 || value.tenantId.length === 0) {
    throw new ValidationError("tax requires both id and tenantId");
  }
  if (value.reference.trim().length === 0) {
    throw new ValidationError("tax reference must not be blank");
  }
  if (!Number.isInteger(value.amountCents) || value.amountCents < 0) {
    throw new ValidationError("tax amount must be a non-negative integer");
  }
  if (isBefore(value.updatedAt, value.createdAt)) {
    throw new ValidationError("tax updatedAt precedes createdAt");
  }
}

export function compareTax(left: Tax, right: Tax): number {
  const byCreation = left.createdAt.localeCompare(right.createdAt);
  return byCreation !== 0 ? byCreation : left.id.localeCompare(right.id);
}

export function summarizeTax(value: Tax): string {
  return `${value.reference} (${value.status}, ${(value.amountCents / 100).toFixed(2)})`;
}

export function taxStatusCounts(values: readonly Tax[]): Record<TaxStatus, number> {
  const counts: Record<TaxStatus, number> = { draft: 0, active: 0, settled: 0, cancelled: 0 };
  for (const value of values) {
    counts[value.status] += 1;
  }
  return counts;
}
