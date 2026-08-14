import { Quota, QuotaStatus, summarizeQuota } from "./model";
import { QuotaPage } from "./repository";
import { QuotaSummary } from "./service";

export interface QuotaPayload {
  id: string;
  status: string;
  amount: string;
  reference: string;
  labels: readonly string[];
  summary: string;
  updatedAt: string;
}

export interface QuotaPagePayload {
  items: readonly QuotaPayload[];
  total: number;
  offset: number;
}

export interface QuotaSummaryPayload {
  total: number;
  outstanding: number;
  amount: string;
  byStatus: Record<QuotaStatus, number>;
}

function money(amountCents: number): string {
  return (amountCents / 100).toFixed(2);
}

/** Wire representation of a consumption quota; tenant identity never leaves the service. */
export function toQuotaPayload(value: Quota): QuotaPayload {
  return {
    id: value.id,
    status: value.status,
    amount: money(value.amountCents),
    reference: value.reference,
    labels: value.labels,
    summary: summarizeQuota(value),
    updatedAt: value.updatedAt,
  };
}

export function toQuotaPayloads(values: readonly Quota[]): QuotaPayload[] {
  return values.map(toQuotaPayload);
}

export function toQuotaPagePayload(page: QuotaPage): QuotaPagePayload {
  return { items: toQuotaPayloads(page.items), total: page.total, offset: page.offset };
}

export function toQuotaSummaryPayload(summary: QuotaSummary): QuotaSummaryPayload {
  return {
    total: summary.total,
    outstanding: summary.outstanding,
    amount: money(summary.amountCents),
    byStatus: summary.byStatus,
  };
}

export function totalAmountCents(values: readonly Quota[]): number {
  return values.reduce((sum, value) => sum + value.amountCents, 0);
}

export function toQuotaCsvRow(value: Quota): string {
  return [value.id, value.status, money(value.amountCents), value.reference].join(",");
}
