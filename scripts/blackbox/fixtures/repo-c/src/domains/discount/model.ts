import { isoTimestamp, isBefore } from "../../infra/time";
import { ValidationError } from "../../infra/errors";

/** Durable shape of one discount rule. */
export interface Discount {
  readonly id: string;
  readonly tenantId: string;
  status: DiscountStatus;
  amountCents: number;
  reference: string;
  labels: readonly string[];
  history: readonly DiscountChange[];
  readonly createdAt: string;
  updatedAt: string;
}

export interface DiscountChange {
  readonly at: string;
  readonly from: DiscountStatus;
  readonly to: DiscountStatus;
}

export type DiscountStatus = "draft" | "active" | "settled" | "cancelled";

export const discountStatuses: readonly DiscountStatus[] = ["draft", "active", "settled", "cancelled"];

/** Legal forward transitions for a discount rule; anything else is rejected upstream. */
const transitions: Record<DiscountStatus, readonly DiscountStatus[]> = {
  draft: ["active", "cancelled"],
  active: ["settled", "cancelled"],
  settled: [],
  cancelled: [],
};

export function canDiscountTransition(from: DiscountStatus, to: DiscountStatus): boolean {
  return transitions[from].includes(to);
}

export function isDiscountTerminal(value: Discount): boolean {
  return transitions[value.status].length === 0;
}

export function newDiscount(id: string, tenantId: string, reference: string): Discount {
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

export function touchDiscount(value: Discount): Discount {
  return { ...value, updatedAt: isoTimestamp() };
}

/** Applies a transition and records it; callers must check legality first. */
export function applyDiscountTransition(value: Discount, to: DiscountStatus): Discount {
  const change: DiscountChange = { at: isoTimestamp(), from: value.status, to };
  return {
    ...value,
    status: to,
    history: [...value.history, change],
    updatedAt: change.at,
  };
}

export function withDiscountAmount(value: Discount, amountCents: number): Discount {
  if (!Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("discount amount must be a non-negative integer");
  }
  return touchDiscount({ ...value, amountCents });
}

export function withDiscountLabel(value: Discount, label: string): Discount {
  if (label.trim().length === 0) {
    throw new ValidationError("discount label must not be blank");
  }
  if (value.labels.includes(label)) {
    return value;
  }
  return touchDiscount({ ...value, labels: [...value.labels, label].sort() });
}

export function withoutDiscountLabel(value: Discount, label: string): Discount {
  if (!value.labels.includes(label)) {
    return value;
  }
  return touchDiscount({ ...value, labels: value.labels.filter((item) => item !== label) });
}

export function validateDiscount(value: Discount): void {
  if (value.id.length === 0 || value.tenantId.length === 0) {
    throw new ValidationError("discount requires both id and tenantId");
  }
  if (value.reference.trim().length === 0) {
    throw new ValidationError("discount reference must not be blank");
  }
  if (!Number.isInteger(value.amountCents) || value.amountCents < 0) {
    throw new ValidationError("discount amount must be a non-negative integer");
  }
  if (isBefore(value.updatedAt, value.createdAt)) {
    throw new ValidationError("discount updatedAt precedes createdAt");
  }
}

export function compareDiscount(left: Discount, right: Discount): number {
  const byCreation = left.createdAt.localeCompare(right.createdAt);
  return byCreation !== 0 ? byCreation : left.id.localeCompare(right.id);
}

export function summarizeDiscount(value: Discount): string {
  return `${value.reference} (${value.status}, ${(value.amountCents / 100).toFixed(2)})`;
}

export function discountStatusCounts(values: readonly Discount[]): Record<DiscountStatus, number> {
  const counts: Record<DiscountStatus, number> = { draft: 0, active: 0, settled: 0, cancelled: 0 };
  for (const value of values) {
    counts[value.status] += 1;
  }
  return counts;
}
