import { Variant, VariantStatus, summarizeVariant } from "./model";
import { VariantPage } from "./repository";
import { VariantSummary } from "./service";

export interface VariantPayload {
  id: string;
  status: string;
  amount: string;
  reference: string;
  labels: readonly string[];
  summary: string;
  updatedAt: string;
}

export interface VariantPagePayload {
  items: readonly VariantPayload[];
  total: number;
  offset: number;
}

export interface VariantSummaryPayload {
  total: number;
  outstanding: number;
  amount: string;
  byStatus: Record<VariantStatus, number>;
}

function money(amountCents: number): string {
  return (amountCents / 100).toFixed(2);
}

/** Wire representation of a product variant; tenant identity never leaves the service. */
export function toVariantPayload(value: Variant): VariantPayload {
  return {
    id: value.id,
    status: value.status,
    amount: money(value.amountCents),
    reference: value.reference,
    labels: value.labels,
    summary: summarizeVariant(value),
    updatedAt: value.updatedAt,
  };
}

export function toVariantPayloads(values: readonly Variant[]): VariantPayload[] {
  return values.map(toVariantPayload);
}

export function toVariantPagePayload(page: VariantPage): VariantPagePayload {
  return { items: toVariantPayloads(page.items), total: page.total, offset: page.offset };
}

export function toVariantSummaryPayload(summary: VariantSummary): VariantSummaryPayload {
  return {
    total: summary.total,
    outstanding: summary.outstanding,
    amount: money(summary.amountCents),
    byStatus: summary.byStatus,
  };
}

export function totalAmountCents(values: readonly Variant[]): number {
  return values.reduce((sum, value) => sum + value.amountCents, 0);
}

export function toVariantCsvRow(value: Variant): string {
  return [value.id, value.status, money(value.amountCents), value.reference].join(",");
}
