import { Dispute, DisputeStatus, summarizeDispute } from "./model";
import { DisputePage } from "./repository";
import { DisputeSummary } from "./service";

export interface DisputePayload {
  id: string;
  status: string;
  amount: string;
  reference: string;
  labels: readonly string[];
  summary: string;
  updatedAt: string;
}

export interface DisputePagePayload {
  items: readonly DisputePayload[];
  total: number;
  offset: number;
}

export interface DisputeSummaryPayload {
  total: number;
  outstanding: number;
  amount: string;
  byStatus: Record<DisputeStatus, number>;
}

function money(amountCents: number): string {
  return (amountCents / 100).toFixed(2);
}

/** Wire representation of a payment dispute; tenant identity never leaves the service. */
export function toDisputePayload(value: Dispute): DisputePayload {
  return {
    id: value.id,
    status: value.status,
    amount: money(value.amountCents),
    reference: value.reference,
    labels: value.labels,
    summary: summarizeDispute(value),
    updatedAt: value.updatedAt,
  };
}

export function toDisputePayloads(values: readonly Dispute[]): DisputePayload[] {
  return values.map(toDisputePayload);
}

export function toDisputePagePayload(page: DisputePage): DisputePagePayload {
  return { items: toDisputePayloads(page.items), total: page.total, offset: page.offset };
}

export function toDisputeSummaryPayload(summary: DisputeSummary): DisputeSummaryPayload {
  return {
    total: summary.total,
    outstanding: summary.outstanding,
    amount: money(summary.amountCents),
    byStatus: summary.byStatus,
  };
}

export function totalAmountCents(values: readonly Dispute[]): number {
  return values.reduce((sum, value) => sum + value.amountCents, 0);
}

export function toDisputeCsvRow(value: Dispute): string {
  return [value.id, value.status, money(value.amountCents), value.reference].join(",");
}
