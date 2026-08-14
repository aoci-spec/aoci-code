import { isoTimestamp, isBefore } from "../../infra/time";
import { ValidationError } from "../../infra/errors";

/** Durable shape of one warranty claim. */
export interface Warranty {
  readonly id: string;
  readonly tenantId: string;
  status: WarrantyStatus;
  amountCents: number;
  reference: string;
  labels: readonly string[];
  history: readonly WarrantyChange[];
  readonly createdAt: string;
  updatedAt: string;
}

export interface WarrantyChange {
  readonly at: string;
  readonly from: WarrantyStatus;
  readonly to: WarrantyStatus;
}

export type WarrantyStatus = "draft" | "active" | "settled" | "cancelled";

export const warrantyStatuses: readonly WarrantyStatus[] = ["draft", "active", "settled", "cancelled"];

/** Legal forward transitions for a warranty claim; anything else is rejected upstream. */
const transitions: Record<WarrantyStatus, readonly WarrantyStatus[]> = {
  draft: ["active", "cancelled"],
  active: ["settled", "cancelled"],
  settled: [],
  cancelled: [],
};

export function canWarrantyTransition(from: WarrantyStatus, to: WarrantyStatus): boolean {
  return transitions[from].includes(to);
}

export function isWarrantyTerminal(value: Warranty): boolean {
  return transitions[value.status].length === 0;
}

export function newWarranty(id: string, tenantId: string, reference: string): Warranty {
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

export function touchWarranty(value: Warranty): Warranty {
  return { ...value, updatedAt: isoTimestamp() };
}

/** Applies a transition and records it; callers must check legality first. */
export function applyWarrantyTransition(value: Warranty, to: WarrantyStatus): Warranty {
  const change: WarrantyChange = { at: isoTimestamp(), from: value.status, to };
  return {
    ...value,
    status: to,
    history: [...value.history, change],
    updatedAt: change.at,
  };
}

export function withWarrantyAmount(value: Warranty, amountCents: number): Warranty {
  if (!Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("warranty amount must be a non-negative integer");
  }
  return touchWarranty({ ...value, amountCents });
}

export function withWarrantyLabel(value: Warranty, label: string): Warranty {
  if (label.trim().length === 0) {
    throw new ValidationError("warranty label must not be blank");
  }
  if (value.labels.includes(label)) {
    return value;
  }
  return touchWarranty({ ...value, labels: [...value.labels, label].sort() });
}

export function withoutWarrantyLabel(value: Warranty, label: string): Warranty {
  if (!value.labels.includes(label)) {
    return value;
  }
  return touchWarranty({ ...value, labels: value.labels.filter((item) => item !== label) });
}

export function validateWarranty(value: Warranty): void {
  if (value.id.length === 0 || value.tenantId.length === 0) {
    throw new ValidationError("warranty requires both id and tenantId");
  }
  if (value.reference.trim().length === 0) {
    throw new ValidationError("warranty reference must not be blank");
  }
  if (!Number.isInteger(value.amountCents) || value.amountCents < 0) {
    throw new ValidationError("warranty amount must be a non-negative integer");
  }
  if (isBefore(value.updatedAt, value.createdAt)) {
    throw new ValidationError("warranty updatedAt precedes createdAt");
  }
}

export function compareWarranty(left: Warranty, right: Warranty): number {
  const byCreation = left.createdAt.localeCompare(right.createdAt);
  return byCreation !== 0 ? byCreation : left.id.localeCompare(right.id);
}

export function summarizeWarranty(value: Warranty): string {
  return `${value.reference} (${value.status}, ${(value.amountCents / 100).toFixed(2)})`;
}

export function warrantyStatusCounts(values: readonly Warranty[]): Record<WarrantyStatus, number> {
  const counts: Record<WarrantyStatus, number> = { draft: 0, active: 0, settled: 0, cancelled: 0 };
  for (const value of values) {
    counts[value.status] += 1;
  }
  return counts;
}
