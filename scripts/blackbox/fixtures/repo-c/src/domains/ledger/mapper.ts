import { Ledger, LedgerStatus, summarizeLedger } from "./model";
import { LedgerPage } from "./repository";
import { LedgerSummary } from "./service";

export interface LedgerPayload {
  id: string;
  status: string;
  amount: string;
  reference: string;
  labels: readonly string[];
  summary: string;
  updatedAt: string;
}

export interface LedgerPagePayload {
  items: readonly LedgerPayload[];
  total: number;
  offset: number;
}

export interface LedgerSummaryPayload {
  total: number;
  outstanding: number;
  amount: string;
  byStatus: Record<LedgerStatus, number>;
}

function money(amountCents: number): string {
  return (amountCents / 100).toFixed(2);
}

/** Wire representation of a accounting ledger entry; tenant identity never leaves the service. */
export function toLedgerPayload(value: Ledger): LedgerPayload {
  return {
    id: value.id,
    status: value.status,
    amount: money(value.amountCents),
    reference: value.reference,
    labels: value.labels,
    summary: summarizeLedger(value),
    updatedAt: value.updatedAt,
  };
}

export function toLedgerPayloads(values: readonly Ledger[]): LedgerPayload[] {
  return values.map(toLedgerPayload);
}

export function toLedgerPagePayload(page: LedgerPage): LedgerPagePayload {
  return { items: toLedgerPayloads(page.items), total: page.total, offset: page.offset };
}

export function toLedgerSummaryPayload(summary: LedgerSummary): LedgerSummaryPayload {
  return {
    total: summary.total,
    outstanding: summary.outstanding,
    amount: money(summary.amountCents),
    byStatus: summary.byStatus,
  };
}

export function totalAmountCents(values: readonly Ledger[]): number {
  return values.reduce((sum, value) => sum + value.amountCents, 0);
}

export function toLedgerCsvRow(value: Ledger): string {
  return [value.id, value.status, money(value.amountCents), value.reference].join(",");
}
