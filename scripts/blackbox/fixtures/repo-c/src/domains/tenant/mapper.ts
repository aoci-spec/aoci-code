import { Tenant, TenantStatus, summarizeTenant } from "./model";
import { TenantPage } from "./repository";
import { TenantSummary } from "./service";

export interface TenantPayload {
  id: string;
  status: string;
  amount: string;
  reference: string;
  labels: readonly string[];
  summary: string;
  updatedAt: string;
}

export interface TenantPagePayload {
  items: readonly TenantPayload[];
  total: number;
  offset: number;
}

export interface TenantSummaryPayload {
  total: number;
  outstanding: number;
  amount: string;
  byStatus: Record<TenantStatus, number>;
}

function money(amountCents: number): string {
  return (amountCents / 100).toFixed(2);
}

/** Wire representation of a tenant boundary; tenant identity never leaves the service. */
export function toTenantPayload(value: Tenant): TenantPayload {
  return {
    id: value.id,
    status: value.status,
    amount: money(value.amountCents),
    reference: value.reference,
    labels: value.labels,
    summary: summarizeTenant(value),
    updatedAt: value.updatedAt,
  };
}

export function toTenantPayloads(values: readonly Tenant[]): TenantPayload[] {
  return values.map(toTenantPayload);
}

export function toTenantPagePayload(page: TenantPage): TenantPagePayload {
  return { items: toTenantPayloads(page.items), total: page.total, offset: page.offset };
}

export function toTenantSummaryPayload(summary: TenantSummary): TenantSummaryPayload {
  return {
    total: summary.total,
    outstanding: summary.outstanding,
    amount: money(summary.amountCents),
    byStatus: summary.byStatus,
  };
}

export function totalAmountCents(values: readonly Tenant[]): number {
  return values.reduce((sum, value) => sum + value.amountCents, 0);
}

export function toTenantCsvRow(value: Tenant): string {
  return [value.id, value.status, money(value.amountCents), value.reference].join(",");
}
