import { Subscription, SubscriptionStatus, summarizeSubscription } from "./model";
import { SubscriptionPage } from "./repository";
import { SubscriptionSummary } from "./service";

export interface SubscriptionPayload {
  id: string;
  status: string;
  amount: string;
  reference: string;
  labels: readonly string[];
  summary: string;
  updatedAt: string;
}

export interface SubscriptionPagePayload {
  items: readonly SubscriptionPayload[];
  total: number;
  offset: number;
}

export interface SubscriptionSummaryPayload {
  total: number;
  outstanding: number;
  amount: string;
  byStatus: Record<SubscriptionStatus, number>;
}

function money(amountCents: number): string {
  return (amountCents / 100).toFixed(2);
}

/** Wire representation of a recurring subscription; tenant identity never leaves the service. */
export function toSubscriptionPayload(value: Subscription): SubscriptionPayload {
  return {
    id: value.id,
    status: value.status,
    amount: money(value.amountCents),
    reference: value.reference,
    labels: value.labels,
    summary: summarizeSubscription(value),
    updatedAt: value.updatedAt,
  };
}

export function toSubscriptionPayloads(values: readonly Subscription[]): SubscriptionPayload[] {
  return values.map(toSubscriptionPayload);
}

export function toSubscriptionPagePayload(page: SubscriptionPage): SubscriptionPagePayload {
  return { items: toSubscriptionPayloads(page.items), total: page.total, offset: page.offset };
}

export function toSubscriptionSummaryPayload(summary: SubscriptionSummary): SubscriptionSummaryPayload {
  return {
    total: summary.total,
    outstanding: summary.outstanding,
    amount: money(summary.amountCents),
    byStatus: summary.byStatus,
  };
}

export function totalAmountCents(values: readonly Subscription[]): number {
  return values.reduce((sum, value) => sum + value.amountCents, 0);
}

export function toSubscriptionCsvRow(value: Subscription): string {
  return [value.id, value.status, money(value.amountCents), value.reference].join(",");
}
