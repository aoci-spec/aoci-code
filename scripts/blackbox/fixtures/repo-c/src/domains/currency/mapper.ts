import { Currency, CurrencyStatus, summarizeCurrency } from "./model";
import { CurrencyPage } from "./repository";
import { CurrencySummary } from "./service";

export interface CurrencyPayload {
  id: string;
  status: string;
  amount: string;
  reference: string;
  labels: readonly string[];
  summary: string;
  updatedAt: string;
}

export interface CurrencyPagePayload {
  items: readonly CurrencyPayload[];
  total: number;
  offset: number;
}

export interface CurrencySummaryPayload {
  total: number;
  outstanding: number;
  amount: string;
  byStatus: Record<CurrencyStatus, number>;
}

function money(amountCents: number): string {
  return (amountCents / 100).toFixed(2);
}

/** Wire representation of a currency rate; tenant identity never leaves the service. */
export function toCurrencyPayload(value: Currency): CurrencyPayload {
  return {
    id: value.id,
    status: value.status,
    amount: money(value.amountCents),
    reference: value.reference,
    labels: value.labels,
    summary: summarizeCurrency(value),
    updatedAt: value.updatedAt,
  };
}

export function toCurrencyPayloads(values: readonly Currency[]): CurrencyPayload[] {
  return values.map(toCurrencyPayload);
}

export function toCurrencyPagePayload(page: CurrencyPage): CurrencyPagePayload {
  return { items: toCurrencyPayloads(page.items), total: page.total, offset: page.offset };
}

export function toCurrencySummaryPayload(summary: CurrencySummary): CurrencySummaryPayload {
  return {
    total: summary.total,
    outstanding: summary.outstanding,
    amount: money(summary.amountCents),
    byStatus: summary.byStatus,
  };
}

export function totalAmountCents(values: readonly Currency[]): number {
  return values.reduce((sum, value) => sum + value.amountCents, 0);
}

export function toCurrencyCsvRow(value: Currency): string {
  return [value.id, value.status, money(value.amountCents), value.reference].join(",");
}
