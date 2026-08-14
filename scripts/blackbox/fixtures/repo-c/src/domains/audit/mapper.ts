import { Audit, AuditStatus, summarizeAudit } from "./model";
import { AuditPage } from "./repository";
import { AuditSummary } from "./service";

export interface AuditPayload {
  id: string;
  status: string;
  amount: string;
  reference: string;
  labels: readonly string[];
  summary: string;
  updatedAt: string;
}

export interface AuditPagePayload {
  items: readonly AuditPayload[];
  total: number;
  offset: number;
}

export interface AuditSummaryPayload {
  total: number;
  outstanding: number;
  amount: string;
  byStatus: Record<AuditStatus, number>;
}

function money(amountCents: number): string {
  return (amountCents / 100).toFixed(2);
}

/** Wire representation of a audit trail record; tenant identity never leaves the service. */
export function toAuditPayload(value: Audit): AuditPayload {
  return {
    id: value.id,
    status: value.status,
    amount: money(value.amountCents),
    reference: value.reference,
    labels: value.labels,
    summary: summarizeAudit(value),
    updatedAt: value.updatedAt,
  };
}

export function toAuditPayloads(values: readonly Audit[]): AuditPayload[] {
  return values.map(toAuditPayload);
}

export function toAuditPagePayload(page: AuditPage): AuditPagePayload {
  return { items: toAuditPayloads(page.items), total: page.total, offset: page.offset };
}

export function toAuditSummaryPayload(summary: AuditSummary): AuditSummaryPayload {
  return {
    total: summary.total,
    outstanding: summary.outstanding,
    amount: money(summary.amountCents),
    byStatus: summary.byStatus,
  };
}

export function totalAmountCents(values: readonly Audit[]): number {
  return values.reduce((sum, value) => sum + value.amountCents, 0);
}

export function toAuditCsvRow(value: Audit): string {
  return [value.id, value.status, money(value.amountCents), value.reference].join(",");
}
