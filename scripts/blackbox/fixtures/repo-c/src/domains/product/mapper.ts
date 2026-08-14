import { Product, ProductStatus, summarizeProduct } from "./model";
import { ProductPage } from "./repository";
import { ProductSummary } from "./service";

export interface ProductPayload {
  id: string;
  status: string;
  amount: string;
  reference: string;
  labels: readonly string[];
  summary: string;
  updatedAt: string;
}

export interface ProductPagePayload {
  items: readonly ProductPayload[];
  total: number;
  offset: number;
}

export interface ProductSummaryPayload {
  total: number;
  outstanding: number;
  amount: string;
  byStatus: Record<ProductStatus, number>;
}

function money(amountCents: number): string {
  return (amountCents / 100).toFixed(2);
}

/** Wire representation of a sellable product; tenant identity never leaves the service. */
export function toProductPayload(value: Product): ProductPayload {
  return {
    id: value.id,
    status: value.status,
    amount: money(value.amountCents),
    reference: value.reference,
    labels: value.labels,
    summary: summarizeProduct(value),
    updatedAt: value.updatedAt,
  };
}

export function toProductPayloads(values: readonly Product[]): ProductPayload[] {
  return values.map(toProductPayload);
}

export function toProductPagePayload(page: ProductPage): ProductPagePayload {
  return { items: toProductPayloads(page.items), total: page.total, offset: page.offset };
}

export function toProductSummaryPayload(summary: ProductSummary): ProductSummaryPayload {
  return {
    total: summary.total,
    outstanding: summary.outstanding,
    amount: money(summary.amountCents),
    byStatus: summary.byStatus,
  };
}

export function totalAmountCents(values: readonly Product[]): number {
  return values.reduce((sum, value) => sum + value.amountCents, 0);
}

export function toProductCsvRow(value: Product): string {
  return [value.id, value.status, money(value.amountCents), value.reference].join(",");
}
