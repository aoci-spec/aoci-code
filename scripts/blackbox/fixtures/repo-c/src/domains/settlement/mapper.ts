import { Settlement, SettlementStatus, summarizeSettlement } from "./model";
import { SettlementPage } from "./repository";
import { SettlementSummary } from "./service";

export interface SettlementPayload {
  id: string;
  status: string;
  amount: string;
  reference: string;
  labels: readonly string[];
  summary: string;
  updatedAt: string;
}

export interface SettlementPagePayload {
  items: readonly SettlementPayload[];
  total: number;
  offset: number;
}

export interface SettlementSummaryPayload {
  total: number;
  outstanding: number;
  amount: string;
  byStatus: Record<SettlementStatus, number>;
}

function money(amountCents: number): string {
  return (amountCents / 100).toFixed(2);
}

/** Wire representation of a settlement run; tenant identity never leaves the service. */
export function toSettlementPayload(value: Settlement): SettlementPayload {
  return {
    id: value.id,
    status: value.status,
    amount: money(value.amountCents),
    reference: value.reference,
    labels: value.labels,
    summary: summarizeSettlement(value),
    updatedAt: value.updatedAt,
  };
}

export function toSettlementPayloads(values: readonly Settlement[]): SettlementPayload[] {
  return values.map(toSettlementPayload);
}

export function toSettlementPagePayload(page: SettlementPage): SettlementPagePayload {
  return { items: toSettlementPayloads(page.items), total: page.total, offset: page.offset };
}

export function toSettlementSummaryPayload(summary: SettlementSummary): SettlementSummaryPayload {
  return {
    total: summary.total,
    outstanding: summary.outstanding,
    amount: money(summary.amountCents),
    byStatus: summary.byStatus,
  };
}

export function totalAmountCents(values: readonly Settlement[]): number {
  return values.reduce((sum, value) => sum + value.amountCents, 0);
}

export function toSettlementCsvRow(value: Settlement): string {
  return [value.id, value.status, money(value.amountCents), value.reference].join(",");
}
