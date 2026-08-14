import { Catalog, CatalogStatus, summarizeCatalog } from "./model";
import { CatalogPage } from "./repository";
import { CatalogSummary } from "./service";

export interface CatalogPayload {
  id: string;
  status: string;
  amount: string;
  reference: string;
  labels: readonly string[];
  summary: string;
  updatedAt: string;
}

export interface CatalogPagePayload {
  items: readonly CatalogPayload[];
  total: number;
  offset: number;
}

export interface CatalogSummaryPayload {
  total: number;
  outstanding: number;
  amount: string;
  byStatus: Record<CatalogStatus, number>;
}

function money(amountCents: number): string {
  return (amountCents / 100).toFixed(2);
}

/** Wire representation of a product catalog; tenant identity never leaves the service. */
export function toCatalogPayload(value: Catalog): CatalogPayload {
  return {
    id: value.id,
    status: value.status,
    amount: money(value.amountCents),
    reference: value.reference,
    labels: value.labels,
    summary: summarizeCatalog(value),
    updatedAt: value.updatedAt,
  };
}

export function toCatalogPayloads(values: readonly Catalog[]): CatalogPayload[] {
  return values.map(toCatalogPayload);
}

export function toCatalogPagePayload(page: CatalogPage): CatalogPagePayload {
  return { items: toCatalogPayloads(page.items), total: page.total, offset: page.offset };
}

export function toCatalogSummaryPayload(summary: CatalogSummary): CatalogSummaryPayload {
  return {
    total: summary.total,
    outstanding: summary.outstanding,
    amount: money(summary.amountCents),
    byStatus: summary.byStatus,
  };
}

export function totalAmountCents(values: readonly Catalog[]): number {
  return values.reduce((sum, value) => sum + value.amountCents, 0);
}

export function toCatalogCsvRow(value: Catalog): string {
  return [value.id, value.status, money(value.amountCents), value.reference].join(",");
}
