import { Permission, PermissionStatus, summarizePermission } from "./model";
import { PermissionPage } from "./repository";
import { PermissionSummary } from "./service";

export interface PermissionPayload {
  id: string;
  status: string;
  amount: string;
  reference: string;
  labels: readonly string[];
  summary: string;
  updatedAt: string;
}

export interface PermissionPagePayload {
  items: readonly PermissionPayload[];
  total: number;
  offset: number;
}

export interface PermissionSummaryPayload {
  total: number;
  outstanding: number;
  amount: string;
  byStatus: Record<PermissionStatus, number>;
}

function money(amountCents: number): string {
  return (amountCents / 100).toFixed(2);
}

/** Wire representation of a permission grant; tenant identity never leaves the service. */
export function toPermissionPayload(value: Permission): PermissionPayload {
  return {
    id: value.id,
    status: value.status,
    amount: money(value.amountCents),
    reference: value.reference,
    labels: value.labels,
    summary: summarizePermission(value),
    updatedAt: value.updatedAt,
  };
}

export function toPermissionPayloads(values: readonly Permission[]): PermissionPayload[] {
  return values.map(toPermissionPayload);
}

export function toPermissionPagePayload(page: PermissionPage): PermissionPagePayload {
  return { items: toPermissionPayloads(page.items), total: page.total, offset: page.offset };
}

export function toPermissionSummaryPayload(summary: PermissionSummary): PermissionSummaryPayload {
  return {
    total: summary.total,
    outstanding: summary.outstanding,
    amount: money(summary.amountCents),
    byStatus: summary.byStatus,
  };
}

export function totalAmountCents(values: readonly Permission[]): number {
  return values.reduce((sum, value) => sum + value.amountCents, 0);
}

export function toPermissionCsvRow(value: Permission): string {
  return [value.id, value.status, money(value.amountCents), value.reference].join(",");
}
