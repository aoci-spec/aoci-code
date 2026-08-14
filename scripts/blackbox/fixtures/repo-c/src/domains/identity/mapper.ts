import { Identity, IdentityStatus, summarizeIdentity } from "./model";
import { IdentityPage } from "./repository";
import { IdentitySummary } from "./service";

export interface IdentityPayload {
  id: string;
  status: string;
  amount: string;
  reference: string;
  labels: readonly string[];
  summary: string;
  updatedAt: string;
}

export interface IdentityPagePayload {
  items: readonly IdentityPayload[];
  total: number;
  offset: number;
}

export interface IdentitySummaryPayload {
  total: number;
  outstanding: number;
  amount: string;
  byStatus: Record<IdentityStatus, number>;
}

function money(amountCents: number): string {
  return (amountCents / 100).toFixed(2);
}

/** Wire representation of a identity record; tenant identity never leaves the service. */
export function toIdentityPayload(value: Identity): IdentityPayload {
  return {
    id: value.id,
    status: value.status,
    amount: money(value.amountCents),
    reference: value.reference,
    labels: value.labels,
    summary: summarizeIdentity(value),
    updatedAt: value.updatedAt,
  };
}

export function toIdentityPayloads(values: readonly Identity[]): IdentityPayload[] {
  return values.map(toIdentityPayload);
}

export function toIdentityPagePayload(page: IdentityPage): IdentityPagePayload {
  return { items: toIdentityPayloads(page.items), total: page.total, offset: page.offset };
}

export function toIdentitySummaryPayload(summary: IdentitySummary): IdentitySummaryPayload {
  return {
    total: summary.total,
    outstanding: summary.outstanding,
    amount: money(summary.amountCents),
    byStatus: summary.byStatus,
  };
}

export function totalAmountCents(values: readonly Identity[]): number {
  return values.reduce((sum, value) => sum + value.amountCents, 0);
}

export function toIdentityCsvRow(value: Identity): string {
  return [value.id, value.status, money(value.amountCents), value.reference].join(",");
}
