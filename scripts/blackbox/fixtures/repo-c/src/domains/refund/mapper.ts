import { Refund, RefundStatus, summarizeRefund } from "./model";
import { RefundPage } from "./repository";
import { RefundSummary } from "./service";

export interface RefundPayload {
  id: string;
  status: string;
  amount: string;
  reference: string;
  labels: readonly string[];
  summary: string;
  updatedAt: string;
}

export interface RefundPagePayload {
  items: readonly RefundPayload[];
  total: number;
  offset: number;
}

export interface RefundSummaryPayload {
  total: number;
  outstanding: number;
  amount: string;
  byStatus: Record<RefundStatus, number>;
}

function money(amountCents: number): string {
  return (amountCents / 100).toFixed(2);
}

/** Wire representation of a refund request; tenant identity never leaves the service. */
export function toRefundPayload(value: Refund): RefundPayload {
  return {
    id: value.id,
    status: value.status,
    amount: money(value.amountCents),
    reference: value.reference,
    labels: value.labels,
    summary: summarizeRefund(value),
    updatedAt: value.updatedAt,
  };
}

export function toRefundPayloads(values: readonly Refund[]): RefundPayload[] {
  return values.map(toRefundPayload);
}

export function toRefundPagePayload(page: RefundPage): RefundPagePayload {
  return { items: toRefundPayloads(page.items), total: page.total, offset: page.offset };
}

export function toRefundSummaryPayload(summary: RefundSummary): RefundSummaryPayload {
  return {
    total: summary.total,
    outstanding: summary.outstanding,
    amount: money(summary.amountCents),
    byStatus: summary.byStatus,
  };
}

export function totalAmountCents(values: readonly Refund[]): number {
  return values.reduce((sum, value) => sum + value.amountCents, 0);
}

export function toRefundCsvRow(value: Refund): string {
  return [value.id, value.status, money(value.amountCents), value.reference].join(",");
}
