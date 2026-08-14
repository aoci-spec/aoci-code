import { Tax, TaxStatus, summarizeTax } from "./model";
import { TaxPage } from "./repository";
import { TaxSummary } from "./service";

export interface TaxPayload {
  id: string;
  status: string;
  amount: string;
  reference: string;
  labels: readonly string[];
  summary: string;
  updatedAt: string;
}

export interface TaxPagePayload {
  items: readonly TaxPayload[];
  total: number;
  offset: number;
}

export interface TaxSummaryPayload {
  total: number;
  outstanding: number;
  amount: string;
  byStatus: Record<TaxStatus, number>;
}

function money(amountCents: number): string {
  return (amountCents / 100).toFixed(2);
}

/** Wire representation of a tax determination; tenant identity never leaves the service. */
export function toTaxPayload(value: Tax): TaxPayload {
  return {
    id: value.id,
    status: value.status,
    amount: money(value.amountCents),
    reference: value.reference,
    labels: value.labels,
    summary: summarizeTax(value),
    updatedAt: value.updatedAt,
  };
}

export function toTaxPayloads(values: readonly Tax[]): TaxPayload[] {
  return values.map(toTaxPayload);
}

export function toTaxPagePayload(page: TaxPage): TaxPagePayload {
  return { items: toTaxPayloads(page.items), total: page.total, offset: page.offset };
}

export function toTaxSummaryPayload(summary: TaxSummary): TaxSummaryPayload {
  return {
    total: summary.total,
    outstanding: summary.outstanding,
    amount: money(summary.amountCents),
    byStatus: summary.byStatus,
  };
}

export function totalAmountCents(values: readonly Tax[]): number {
  return values.reduce((sum, value) => sum + value.amountCents, 0);
}

export function toTaxCsvRow(value: Tax): string {
  return [value.id, value.status, money(value.amountCents), value.reference].join(",");
}
