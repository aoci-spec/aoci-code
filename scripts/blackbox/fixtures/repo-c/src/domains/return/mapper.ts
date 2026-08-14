import { ReturnCase, ReturnCaseStatus, summarizeReturnCase } from "./model";
import { ReturnCasePage } from "./repository";
import { ReturnCaseSummary } from "./service";

export interface ReturnCasePayload {
  id: string;
  status: string;
  amount: string;
  reference: string;
  labels: readonly string[];
  summary: string;
  updatedAt: string;
}

export interface ReturnCasePagePayload {
  items: readonly ReturnCasePayload[];
  total: number;
  offset: number;
}

export interface ReturnCaseSummaryPayload {
  total: number;
  outstanding: number;
  amount: string;
  byStatus: Record<ReturnCaseStatus, number>;
}

function money(amountCents: number): string {
  return (amountCents / 100).toFixed(2);
}

/** Wire representation of a return case; tenant identity never leaves the service. */
export function toReturnCasePayload(value: ReturnCase): ReturnCasePayload {
  return {
    id: value.id,
    status: value.status,
    amount: money(value.amountCents),
    reference: value.reference,
    labels: value.labels,
    summary: summarizeReturnCase(value),
    updatedAt: value.updatedAt,
  };
}

export function toReturnCasePayloads(values: readonly ReturnCase[]): ReturnCasePayload[] {
  return values.map(toReturnCasePayload);
}

export function toReturnCasePagePayload(page: ReturnCasePage): ReturnCasePagePayload {
  return { items: toReturnCasePayloads(page.items), total: page.total, offset: page.offset };
}

export function toReturnCaseSummaryPayload(summary: ReturnCaseSummary): ReturnCaseSummaryPayload {
  return {
    total: summary.total,
    outstanding: summary.outstanding,
    amount: money(summary.amountCents),
    byStatus: summary.byStatus,
  };
}

export function totalAmountCents(values: readonly ReturnCase[]): number {
  return values.reduce((sum, value) => sum + value.amountCents, 0);
}

export function toReturnCaseCsvRow(value: ReturnCase): string {
  return [value.id, value.status, money(value.amountCents), value.reference].join(",");
}
