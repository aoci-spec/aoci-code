import { Price, PriceStatus, summarizePrice } from "./model";
import { PricePage } from "./repository";
import { PriceSummary } from "./service";

export interface PricePayload {
  id: string;
  status: string;
  amount: string;
  reference: string;
  labels: readonly string[];
  summary: string;
  updatedAt: string;
}

export interface PricePagePayload {
  items: readonly PricePayload[];
  total: number;
  offset: number;
}

export interface PriceSummaryPayload {
  total: number;
  outstanding: number;
  amount: string;
  byStatus: Record<PriceStatus, number>;
}

function money(amountCents: number): string {
  return (amountCents / 100).toFixed(2);
}

/** Wire representation of a price definition; tenant identity never leaves the service. */
export function toPricePayload(value: Price): PricePayload {
  return {
    id: value.id,
    status: value.status,
    amount: money(value.amountCents),
    reference: value.reference,
    labels: value.labels,
    summary: summarizePrice(value),
    updatedAt: value.updatedAt,
  };
}

export function toPricePayloads(values: readonly Price[]): PricePayload[] {
  return values.map(toPricePayload);
}

export function toPricePagePayload(page: PricePage): PricePagePayload {
  return { items: toPricePayloads(page.items), total: page.total, offset: page.offset };
}

export function toPriceSummaryPayload(summary: PriceSummary): PriceSummaryPayload {
  return {
    total: summary.total,
    outstanding: summary.outstanding,
    amount: money(summary.amountCents),
    byStatus: summary.byStatus,
  };
}

export function totalAmountCents(values: readonly Price[]): number {
  return values.reduce((sum, value) => sum + value.amountCents, 0);
}

export function toPriceCsvRow(value: Price): string {
  return [value.id, value.status, money(value.amountCents), value.reference].join(",");
}
