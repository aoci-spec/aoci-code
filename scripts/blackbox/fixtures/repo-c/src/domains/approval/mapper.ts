import { Approval, ApprovalStatus, summarizeApproval } from "./model";
import { ApprovalPage } from "./repository";
import { ApprovalSummary } from "./service";

export interface ApprovalPayload {
  id: string;
  status: string;
  amount: string;
  reference: string;
  labels: readonly string[];
  summary: string;
  updatedAt: string;
}

export interface ApprovalPagePayload {
  items: readonly ApprovalPayload[];
  total: number;
  offset: number;
}

export interface ApprovalSummaryPayload {
  total: number;
  outstanding: number;
  amount: string;
  byStatus: Record<ApprovalStatus, number>;
}

function money(amountCents: number): string {
  return (amountCents / 100).toFixed(2);
}

/** Wire representation of a approval decision; tenant identity never leaves the service. */
export function toApprovalPayload(value: Approval): ApprovalPayload {
  return {
    id: value.id,
    status: value.status,
    amount: money(value.amountCents),
    reference: value.reference,
    labels: value.labels,
    summary: summarizeApproval(value),
    updatedAt: value.updatedAt,
  };
}

export function toApprovalPayloads(values: readonly Approval[]): ApprovalPayload[] {
  return values.map(toApprovalPayload);
}

export function toApprovalPagePayload(page: ApprovalPage): ApprovalPagePayload {
  return { items: toApprovalPayloads(page.items), total: page.total, offset: page.offset };
}

export function toApprovalSummaryPayload(summary: ApprovalSummary): ApprovalSummaryPayload {
  return {
    total: summary.total,
    outstanding: summary.outstanding,
    amount: money(summary.amountCents),
    byStatus: summary.byStatus,
  };
}

export function totalAmountCents(values: readonly Approval[]): number {
  return values.reduce((sum, value) => sum + value.amountCents, 0);
}

export function toApprovalCsvRow(value: Approval): string {
  return [value.id, value.status, money(value.amountCents), value.reference].join(",");
}
