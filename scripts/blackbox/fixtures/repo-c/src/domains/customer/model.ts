import { isoTimestamp, isBefore } from "../../infra/time";
import { ValidationError } from "../../infra/errors";

/** Durable shape of one customer account. */
export interface Customer {
  readonly id: string;
  readonly tenantId: string;
  status: CustomerStatus;
  amountCents: number;
  reference: string;
  labels: readonly string[];
  history: readonly CustomerChange[];
  readonly createdAt: string;
  updatedAt: string;
}

export interface CustomerChange {
  readonly at: string;
  readonly from: CustomerStatus;
  readonly to: CustomerStatus;
}

export type CustomerStatus = "draft" | "active" | "settled" | "cancelled";

export const customerStatuses: readonly CustomerStatus[] = ["draft", "active", "settled", "cancelled"];

/** Legal forward transitions for a customer account; anything else is rejected upstream. */
const transitions: Record<CustomerStatus, readonly CustomerStatus[]> = {
  draft: ["active", "cancelled"],
  active: ["settled", "cancelled"],
  settled: [],
  cancelled: [],
};

export function canCustomerTransition(from: CustomerStatus, to: CustomerStatus): boolean {
  return transitions[from].includes(to);
}

export function isCustomerTerminal(value: Customer): boolean {
  return transitions[value.status].length === 0;
}

export function newCustomer(id: string, tenantId: string, reference: string): Customer {
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

export function touchCustomer(value: Customer): Customer {
  return { ...value, updatedAt: isoTimestamp() };
}

/** Applies a transition and records it; callers must check legality first. */
export function applyCustomerTransition(value: Customer, to: CustomerStatus): Customer {
  const change: CustomerChange = { at: isoTimestamp(), from: value.status, to };
  return {
    ...value,
    status: to,
    history: [...value.history, change],
    updatedAt: change.at,
  };
}

export function withCustomerAmount(value: Customer, amountCents: number): Customer {
  if (!Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("customer amount must be a non-negative integer");
  }
  return touchCustomer({ ...value, amountCents });
}

export function withCustomerLabel(value: Customer, label: string): Customer {
  if (label.trim().length === 0) {
    throw new ValidationError("customer label must not be blank");
  }
  if (value.labels.includes(label)) {
    return value;
  }
  return touchCustomer({ ...value, labels: [...value.labels, label].sort() });
}

export function withoutCustomerLabel(value: Customer, label: string): Customer {
  if (!value.labels.includes(label)) {
    return value;
  }
  return touchCustomer({ ...value, labels: value.labels.filter((item) => item !== label) });
}

export function validateCustomer(value: Customer): void {
  if (value.id.length === 0 || value.tenantId.length === 0) {
    throw new ValidationError("customer requires both id and tenantId");
  }
  if (value.reference.trim().length === 0) {
    throw new ValidationError("customer reference must not be blank");
  }
  if (!Number.isInteger(value.amountCents) || value.amountCents < 0) {
    throw new ValidationError("customer amount must be a non-negative integer");
  }
  if (isBefore(value.updatedAt, value.createdAt)) {
    throw new ValidationError("customer updatedAt precedes createdAt");
  }
}

export function compareCustomer(left: Customer, right: Customer): number {
  const byCreation = left.createdAt.localeCompare(right.createdAt);
  return byCreation !== 0 ? byCreation : left.id.localeCompare(right.id);
}

export function summarizeCustomer(value: Customer): string {
  return `${value.reference} (${value.status}, ${(value.amountCents / 100).toFixed(2)})`;
}

export function customerStatusCounts(values: readonly Customer[]): Record<CustomerStatus, number> {
  const counts: Record<CustomerStatus, number> = { draft: 0, active: 0, settled: 0, cancelled: 0 };
  for (const value of values) {
    counts[value.status] += 1;
  }
  return counts;
}
