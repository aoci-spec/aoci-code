import { Payout, PayoutStatus, summarizePayout } from "./model";
import { PayoutPage } from "./repository";
import { PayoutSummary } from "./service";

export interface PayoutPayload {
  id: string;
  status: string;
  amount: string;
  reference: string;
  labels: readonly string[];
  summary: string;
  updatedAt: string;
}

export interface PayoutPagePayload {
  items: readonly PayoutPayload[];
  total: number;
  offset: number;
}

export interface PayoutSummaryPayload {
  total: number;
  outstanding: number;
  amount: string;
  byStatus: Record<PayoutStatus, number>;
}

function money(amountCents: number): string {
  return (amountCents / 100).toFixed(2);
}

/** Wire representation of a merchant payout; tenant identity never leaves the service. */
export function toPayoutPayload(value: Payout): PayoutPayload {
  return {
    id: value.id,
    status: value.status,
    amount: money(value.amountCents),
    reference: value.reference,
    labels: value.labels,
    summary: summarizePayout(value),
    updatedAt: value.updatedAt,
  };
}

export function toPayoutPayloads(values: readonly Payout[]): PayoutPayload[] {
  return values.map(toPayoutPayload);
}

export function toPayoutPagePayload(page: PayoutPage): PayoutPagePayload {
  return { items: toPayoutPayloads(page.items), total: page.total, offset: page.offset };
}

export function toPayoutSummaryPayload(summary: PayoutSummary): PayoutSummaryPayload {
  return {
    total: summary.total,
    outstanding: summary.outstanding,
    amount: money(summary.amountCents),
    byStatus: summary.byStatus,
  };
}

export function totalAmountCents(values: readonly Payout[]): number {
  return values.reduce((sum, value) => sum + value.amountCents, 0);
}

export function toPayoutCsvRow(value: Payout): string {
  return [value.id, value.status, money(value.amountCents), value.reference].join(",");
}
