import { isoTimestamp, isBefore } from "../../infra/time";
import { ValidationError } from "../../infra/errors";

/** Durable shape of one payment attempt. */
export interface Payment {
  readonly id: string;
  readonly tenantId: string;
  status: PaymentStatus;
  amountCents: number;
  reference: string;
  labels: readonly string[];
  history: readonly PaymentChange[];
  readonly createdAt: string;
  updatedAt: string;
}

export interface PaymentChange {
  readonly at: string;
  readonly from: PaymentStatus;
  readonly to: PaymentStatus;
}

export type PaymentStatus = "draft" | "active" | "settled" | "cancelled";

export const paymentStatuses: readonly PaymentStatus[] = ["draft", "active", "settled", "cancelled"];

/** Legal forward transitions for a payment attempt; anything else is rejected upstream. */
const transitions: Record<PaymentStatus, readonly PaymentStatus[]> = {
  draft: ["active", "cancelled"],
  active: ["settled", "cancelled"],
  settled: [],
  cancelled: [],
};

export function canPaymentTransition(from: PaymentStatus, to: PaymentStatus): boolean {
  return transitions[from].includes(to);
}

export function isPaymentTerminal(value: Payment): boolean {
  return transitions[value.status].length === 0;
}

export function newPayment(id: string, tenantId: string, reference: string): Payment {
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

export function touchPayment(value: Payment): Payment {
  return { ...value, updatedAt: isoTimestamp() };
}

/** Applies a transition and records it; callers must check legality first. */
export function applyPaymentTransition(value: Payment, to: PaymentStatus): Payment {
  const change: PaymentChange = { at: isoTimestamp(), from: value.status, to };
  return {
    ...value,
    status: to,
    history: [...value.history, change],
    updatedAt: change.at,
  };
}

export function withPaymentAmount(value: Payment, amountCents: number): Payment {
  if (!Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("payment amount must be a non-negative integer");
  }
  return touchPayment({ ...value, amountCents });
}

export function withPaymentLabel(value: Payment, label: string): Payment {
  if (label.trim().length === 0) {
    throw new ValidationError("payment label must not be blank");
  }
  if (value.labels.includes(label)) {
    return value;
  }
  return touchPayment({ ...value, labels: [...value.labels, label].sort() });
}

export function withoutPaymentLabel(value: Payment, label: string): Payment {
  if (!value.labels.includes(label)) {
    return value;
  }
  return touchPayment({ ...value, labels: value.labels.filter((item) => item !== label) });
}

export function validatePayment(value: Payment): void {
  if (value.id.length === 0 || value.tenantId.length === 0) {
    throw new ValidationError("payment requires both id and tenantId");
  }
  if (value.reference.trim().length === 0) {
    throw new ValidationError("payment reference must not be blank");
  }
  if (!Number.isInteger(value.amountCents) || value.amountCents < 0) {
    throw new ValidationError("payment amount must be a non-negative integer");
  }
  if (isBefore(value.updatedAt, value.createdAt)) {
    throw new ValidationError("payment updatedAt precedes createdAt");
  }
}

export function comparePayment(left: Payment, right: Payment): number {
  const byCreation = left.createdAt.localeCompare(right.createdAt);
  return byCreation !== 0 ? byCreation : left.id.localeCompare(right.id);
}

export function summarizePayment(value: Payment): string {
  return `${value.reference} (${value.status}, ${(value.amountCents / 100).toFixed(2)})`;
}

export function paymentStatusCounts(values: readonly Payment[]): Record<PaymentStatus, number> {
  const counts: Record<PaymentStatus, number> = { draft: 0, active: 0, settled: 0, cancelled: 0 };
  for (const value of values) {
    counts[value.status] += 1;
  }
  return counts;
}
