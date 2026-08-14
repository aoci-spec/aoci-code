import { Warranty, WarrantyStatus, summarizeWarranty } from "./model";
import { WarrantyPage } from "./repository";
import { WarrantySummary } from "./service";

export interface WarrantyPayload {
  id: string;
  status: string;
  amount: string;
  reference: string;
  labels: readonly string[];
  summary: string;
  updatedAt: string;
}

export interface WarrantyPagePayload {
  items: readonly WarrantyPayload[];
  total: number;
  offset: number;
}

export interface WarrantySummaryPayload {
  total: number;
  outstanding: number;
  amount: string;
  byStatus: Record<WarrantyStatus, number>;
}

function money(amountCents: number): string {
  return (amountCents / 100).toFixed(2);
}

/** Wire representation of a warranty claim; tenant identity never leaves the service. */
export function toWarrantyPayload(value: Warranty): WarrantyPayload {
  return {
    id: value.id,
    status: value.status,
    amount: money(value.amountCents),
    reference: value.reference,
    labels: value.labels,
    summary: summarizeWarranty(value),
    updatedAt: value.updatedAt,
  };
}

export function toWarrantyPayloads(values: readonly Warranty[]): WarrantyPayload[] {
  return values.map(toWarrantyPayload);
}

export function toWarrantyPagePayload(page: WarrantyPage): WarrantyPagePayload {
  return { items: toWarrantyPayloads(page.items), total: page.total, offset: page.offset };
}

export function toWarrantySummaryPayload(summary: WarrantySummary): WarrantySummaryPayload {
  return {
    total: summary.total,
    outstanding: summary.outstanding,
    amount: money(summary.amountCents),
    byStatus: summary.byStatus,
  };
}

export function totalAmountCents(values: readonly Warranty[]): number {
  return values.reduce((sum, value) => sum + value.amountCents, 0);
}

export function toWarrantyCsvRow(value: Warranty): string {
  return [value.id, value.status, money(value.amountCents), value.reference].join(",");
}
