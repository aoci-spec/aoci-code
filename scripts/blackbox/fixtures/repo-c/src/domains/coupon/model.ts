import { isoTimestamp, isBefore } from "../../infra/time";
import { ValidationError } from "../../infra/errors";

/** Durable shape of one coupon grant. */
export interface Coupon {
  readonly id: string;
  readonly tenantId: string;
  status: CouponStatus;
  amountCents: number;
  reference: string;
  labels: readonly string[];
  history: readonly CouponChange[];
  readonly createdAt: string;
  updatedAt: string;
}

export interface CouponChange {
  readonly at: string;
  readonly from: CouponStatus;
  readonly to: CouponStatus;
}

export type CouponStatus = "draft" | "active" | "settled" | "cancelled";

export const couponStatuses: readonly CouponStatus[] = ["draft", "active", "settled", "cancelled"];

/** Legal forward transitions for a coupon grant; anything else is rejected upstream. */
const transitions: Record<CouponStatus, readonly CouponStatus[]> = {
  draft: ["active", "cancelled"],
  active: ["settled", "cancelled"],
  settled: [],
  cancelled: [],
};

export function canCouponTransition(from: CouponStatus, to: CouponStatus): boolean {
  return transitions[from].includes(to);
}

export function isCouponTerminal(value: Coupon): boolean {
  return transitions[value.status].length === 0;
}

export function newCoupon(id: string, tenantId: string, reference: string): Coupon {
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

export function touchCoupon(value: Coupon): Coupon {
  return { ...value, updatedAt: isoTimestamp() };
}

/** Applies a transition and records it; callers must check legality first. */
export function applyCouponTransition(value: Coupon, to: CouponStatus): Coupon {
  const change: CouponChange = { at: isoTimestamp(), from: value.status, to };
  return {
    ...value,
    status: to,
    history: [...value.history, change],
    updatedAt: change.at,
  };
}

export function withCouponAmount(value: Coupon, amountCents: number): Coupon {
  if (!Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("coupon amount must be a non-negative integer");
  }
  return touchCoupon({ ...value, amountCents });
}

export function withCouponLabel(value: Coupon, label: string): Coupon {
  if (label.trim().length === 0) {
    throw new ValidationError("coupon label must not be blank");
  }
  if (value.labels.includes(label)) {
    return value;
  }
  return touchCoupon({ ...value, labels: [...value.labels, label].sort() });
}

export function withoutCouponLabel(value: Coupon, label: string): Coupon {
  if (!value.labels.includes(label)) {
    return value;
  }
  return touchCoupon({ ...value, labels: value.labels.filter((item) => item !== label) });
}

export function validateCoupon(value: Coupon): void {
  if (value.id.length === 0 || value.tenantId.length === 0) {
    throw new ValidationError("coupon requires both id and tenantId");
  }
  if (value.reference.trim().length === 0) {
    throw new ValidationError("coupon reference must not be blank");
  }
  if (!Number.isInteger(value.amountCents) || value.amountCents < 0) {
    throw new ValidationError("coupon amount must be a non-negative integer");
  }
  if (isBefore(value.updatedAt, value.createdAt)) {
    throw new ValidationError("coupon updatedAt precedes createdAt");
  }
}

export function compareCoupon(left: Coupon, right: Coupon): number {
  const byCreation = left.createdAt.localeCompare(right.createdAt);
  return byCreation !== 0 ? byCreation : left.id.localeCompare(right.id);
}

export function summarizeCoupon(value: Coupon): string {
  return `${value.reference} (${value.status}, ${(value.amountCents / 100).toFixed(2)})`;
}

export function couponStatusCounts(values: readonly Coupon[]): Record<CouponStatus, number> {
  const counts: Record<CouponStatus, number> = { draft: 0, active: 0, settled: 0, cancelled: 0 };
  for (const value of values) {
    counts[value.status] += 1;
  }
  return counts;
}
