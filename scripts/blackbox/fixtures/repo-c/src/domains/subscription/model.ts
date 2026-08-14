import { isoTimestamp, isBefore } from "../../infra/time";
import { ValidationError } from "../../infra/errors";

/** Durable shape of one recurring subscription. */
export interface Subscription {
  readonly id: string;
  readonly tenantId: string;
  status: SubscriptionStatus;
  amountCents: number;
  reference: string;
  labels: readonly string[];
  history: readonly SubscriptionChange[];
  readonly createdAt: string;
  updatedAt: string;
}

export interface SubscriptionChange {
  readonly at: string;
  readonly from: SubscriptionStatus;
  readonly to: SubscriptionStatus;
}

export type SubscriptionStatus = "draft" | "active" | "settled" | "cancelled";

export const subscriptionStatuses: readonly SubscriptionStatus[] = ["draft", "active", "settled", "cancelled"];

/** Legal forward transitions for a recurring subscription; anything else is rejected upstream. */
const transitions: Record<SubscriptionStatus, readonly SubscriptionStatus[]> = {
  draft: ["active", "cancelled"],
  active: ["settled", "cancelled"],
  settled: [],
  cancelled: [],
};

export function canSubscriptionTransition(from: SubscriptionStatus, to: SubscriptionStatus): boolean {
  return transitions[from].includes(to);
}

export function isSubscriptionTerminal(value: Subscription): boolean {
  return transitions[value.status].length === 0;
}

export function newSubscription(id: string, tenantId: string, reference: string): Subscription {
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

export function touchSubscription(value: Subscription): Subscription {
  return { ...value, updatedAt: isoTimestamp() };
}

/** Applies a transition and records it; callers must check legality first. */
export function applySubscriptionTransition(value: Subscription, to: SubscriptionStatus): Subscription {
  const change: SubscriptionChange = { at: isoTimestamp(), from: value.status, to };
  return {
    ...value,
    status: to,
    history: [...value.history, change],
    updatedAt: change.at,
  };
}

export function withSubscriptionAmount(value: Subscription, amountCents: number): Subscription {
  if (!Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("subscription amount must be a non-negative integer");
  }
  return touchSubscription({ ...value, amountCents });
}

export function withSubscriptionLabel(value: Subscription, label: string): Subscription {
  if (label.trim().length === 0) {
    throw new ValidationError("subscription label must not be blank");
  }
  if (value.labels.includes(label)) {
    return value;
  }
  return touchSubscription({ ...value, labels: [...value.labels, label].sort() });
}

export function withoutSubscriptionLabel(value: Subscription, label: string): Subscription {
  if (!value.labels.includes(label)) {
    return value;
  }
  return touchSubscription({ ...value, labels: value.labels.filter((item) => item !== label) });
}

export function validateSubscription(value: Subscription): void {
  if (value.id.length === 0 || value.tenantId.length === 0) {
    throw new ValidationError("subscription requires both id and tenantId");
  }
  if (value.reference.trim().length === 0) {
    throw new ValidationError("subscription reference must not be blank");
  }
  if (!Number.isInteger(value.amountCents) || value.amountCents < 0) {
    throw new ValidationError("subscription amount must be a non-negative integer");
  }
  if (isBefore(value.updatedAt, value.createdAt)) {
    throw new ValidationError("subscription updatedAt precedes createdAt");
  }
}

export function compareSubscription(left: Subscription, right: Subscription): number {
  const byCreation = left.createdAt.localeCompare(right.createdAt);
  return byCreation !== 0 ? byCreation : left.id.localeCompare(right.id);
}

export function summarizeSubscription(value: Subscription): string {
  return `${value.reference} (${value.status}, ${(value.amountCents / 100).toFixed(2)})`;
}

export function subscriptionStatusCounts(values: readonly Subscription[]): Record<SubscriptionStatus, number> {
  const counts: Record<SubscriptionStatus, number> = { draft: 0, active: 0, settled: 0, cancelled: 0 };
  for (const value of values) {
    counts[value.status] += 1;
  }
  return counts;
}
