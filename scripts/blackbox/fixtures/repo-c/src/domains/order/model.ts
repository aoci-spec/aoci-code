import { isoTimestamp, isBefore } from "../../infra/time";
import { ValidationError } from "../../infra/errors";

/** Durable shape of one purchase order. */
export interface Order {
  readonly id: string;
  readonly tenantId: string;
  status: OrderStatus;
  amountCents: number;
  reference: string;
  labels: readonly string[];
  history: readonly OrderChange[];
  readonly createdAt: string;
  updatedAt: string;
}

export interface OrderChange {
  readonly at: string;
  readonly from: OrderStatus;
  readonly to: OrderStatus;
}

export type OrderStatus = "draft" | "active" | "settled" | "cancelled";

export const orderStatuses: readonly OrderStatus[] = ["draft", "active", "settled", "cancelled"];

/** Legal forward transitions for a purchase order; anything else is rejected upstream. */
const transitions: Record<OrderStatus, readonly OrderStatus[]> = {
  draft: ["active", "cancelled"],
  active: ["settled", "cancelled"],
  settled: [],
  cancelled: [],
};

export function canOrderTransition(from: OrderStatus, to: OrderStatus): boolean {
  return transitions[from].includes(to);
}

export function isOrderTerminal(value: Order): boolean {
  return transitions[value.status].length === 0;
}

export function newOrder(id: string, tenantId: string, reference: string): Order {
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

export function touchOrder(value: Order): Order {
  return { ...value, updatedAt: isoTimestamp() };
}

/** Applies a transition and records it; callers must check legality first. */
export function applyOrderTransition(value: Order, to: OrderStatus): Order {
  const change: OrderChange = { at: isoTimestamp(), from: value.status, to };
  return {
    ...value,
    status: to,
    history: [...value.history, change],
    updatedAt: change.at,
  };
}

export function withOrderAmount(value: Order, amountCents: number): Order {
  if (!Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("order amount must be a non-negative integer");
  }
  return touchOrder({ ...value, amountCents });
}

export function withOrderLabel(value: Order, label: string): Order {
  if (label.trim().length === 0) {
    throw new ValidationError("order label must not be blank");
  }
  if (value.labels.includes(label)) {
    return value;
  }
  return touchOrder({ ...value, labels: [...value.labels, label].sort() });
}

export function withoutOrderLabel(value: Order, label: string): Order {
  if (!value.labels.includes(label)) {
    return value;
  }
  return touchOrder({ ...value, labels: value.labels.filter((item) => item !== label) });
}

export function validateOrder(value: Order): void {
  if (value.id.length === 0 || value.tenantId.length === 0) {
    throw new ValidationError("order requires both id and tenantId");
  }
  if (value.reference.trim().length === 0) {
    throw new ValidationError("order reference must not be blank");
  }
  if (!Number.isInteger(value.amountCents) || value.amountCents < 0) {
    throw new ValidationError("order amount must be a non-negative integer");
  }
  if (isBefore(value.updatedAt, value.createdAt)) {
    throw new ValidationError("order updatedAt precedes createdAt");
  }
}

export function compareOrder(left: Order, right: Order): number {
  const byCreation = left.createdAt.localeCompare(right.createdAt);
  return byCreation !== 0 ? byCreation : left.id.localeCompare(right.id);
}

export function summarizeOrder(value: Order): string {
  return `${value.reference} (${value.status}, ${(value.amountCents / 100).toFixed(2)})`;
}

export function orderStatusCounts(values: readonly Order[]): Record<OrderStatus, number> {
  const counts: Record<OrderStatus, number> = { draft: 0, active: 0, settled: 0, cancelled: 0 };
  for (const value of values) {
    counts[value.status] += 1;
  }
  return counts;
}
