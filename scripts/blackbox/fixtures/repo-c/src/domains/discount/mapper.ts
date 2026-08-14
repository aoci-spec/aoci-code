import { Discount, DiscountStatus, summarizeDiscount } from "./model";
import { DiscountPage } from "./repository";
import { DiscountSummary } from "./service";

export interface DiscountPayload {
  id: string;
  status: string;
  amount: string;
  reference: string;
  labels: readonly string[];
  summary: string;
  updatedAt: string;
}

export interface DiscountPagePayload {
  items: readonly DiscountPayload[];
  total: number;
  offset: number;
}

export interface DiscountSummaryPayload {
  total: number;
  outstanding: number;
  amount: string;
  byStatus: Record<DiscountStatus, number>;
}

function money(amountCents: number): string {
  return (amountCents / 100).toFixed(2);
}

/** Wire representation of a discount rule; tenant identity never leaves the service. */
export function toDiscountPayload(value: Discount): DiscountPayload {
  return {
    id: value.id,
    status: value.status,
    amount: money(value.amountCents),
    reference: value.reference,
    labels: value.labels,
    summary: summarizeDiscount(value),
    updatedAt: value.updatedAt,
  };
}

export function toDiscountPayloads(values: readonly Discount[]): DiscountPayload[] {
  return values.map(toDiscountPayload);
}

export function toDiscountPagePayload(page: DiscountPage): DiscountPagePayload {
  return { items: toDiscountPayloads(page.items), total: page.total, offset: page.offset };
}

export function toDiscountSummaryPayload(summary: DiscountSummary): DiscountSummaryPayload {
  return {
    total: summary.total,
    outstanding: summary.outstanding,
    amount: money(summary.amountCents),
    byStatus: summary.byStatus,
  };
}

export function totalAmountCents(values: readonly Discount[]): number {
  return values.reduce((sum, value) => sum + value.amountCents, 0);
}

export function toDiscountCsvRow(value: Discount): string {
  return [value.id, value.status, money(value.amountCents), value.reference].join(",");
}
