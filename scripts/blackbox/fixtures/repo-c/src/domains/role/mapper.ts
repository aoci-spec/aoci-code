import { Role, RoleStatus, summarizeRole } from "./model";
import { RolePage } from "./repository";
import { RoleSummary } from "./service";

export interface RolePayload {
  id: string;
  status: string;
  amount: string;
  reference: string;
  labels: readonly string[];
  summary: string;
  updatedAt: string;
}

export interface RolePagePayload {
  items: readonly RolePayload[];
  total: number;
  offset: number;
}

export interface RoleSummaryPayload {
  total: number;
  outstanding: number;
  amount: string;
  byStatus: Record<RoleStatus, number>;
}

function money(amountCents: number): string {
  return (amountCents / 100).toFixed(2);
}

/** Wire representation of a authorization role; tenant identity never leaves the service. */
export function toRolePayload(value: Role): RolePayload {
  return {
    id: value.id,
    status: value.status,
    amount: money(value.amountCents),
    reference: value.reference,
    labels: value.labels,
    summary: summarizeRole(value),
    updatedAt: value.updatedAt,
  };
}

export function toRolePayloads(values: readonly Role[]): RolePayload[] {
  return values.map(toRolePayload);
}

export function toRolePagePayload(page: RolePage): RolePagePayload {
  return { items: toRolePayloads(page.items), total: page.total, offset: page.offset };
}

export function toRoleSummaryPayload(summary: RoleSummary): RoleSummaryPayload {
  return {
    total: summary.total,
    outstanding: summary.outstanding,
    amount: money(summary.amountCents),
    byStatus: summary.byStatus,
  };
}

export function totalAmountCents(values: readonly Role[]): number {
  return values.reduce((sum, value) => sum + value.amountCents, 0);
}

export function toRoleCsvRow(value: Role): string {
  return [value.id, value.status, money(value.amountCents), value.reference].join(",");
}
