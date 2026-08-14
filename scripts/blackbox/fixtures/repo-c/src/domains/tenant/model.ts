import { isoTimestamp, isBefore } from "../../infra/time";
import { ValidationError } from "../../infra/errors";

/** Durable shape of one tenant boundary. */
export interface Tenant {
  readonly id: string;
  readonly tenantId: string;
  status: TenantStatus;
  amountCents: number;
  reference: string;
  labels: readonly string[];
  history: readonly TenantChange[];
  readonly createdAt: string;
  updatedAt: string;
}

export interface TenantChange {
  readonly at: string;
  readonly from: TenantStatus;
  readonly to: TenantStatus;
}

export type TenantStatus = "draft" | "active" | "settled" | "cancelled";

export const tenantStatuses: readonly TenantStatus[] = ["draft", "active", "settled", "cancelled"];

/** Legal forward transitions for a tenant boundary; anything else is rejected upstream. */
const transitions: Record<TenantStatus, readonly TenantStatus[]> = {
  draft: ["active", "cancelled"],
  active: ["settled", "cancelled"],
  settled: [],
  cancelled: [],
};

export function canTenantTransition(from: TenantStatus, to: TenantStatus): boolean {
  return transitions[from].includes(to);
}

export function isTenantTerminal(value: Tenant): boolean {
  return transitions[value.status].length === 0;
}

export function newTenant(id: string, tenantId: string, reference: string): Tenant {
  const now = isoTimestamp();
  return {
    id,
    tenantId,
    status: "draft",
    amountCents: 0,
    reference,
    labels: [],
    history: [],
    createdAt: now,
    updatedAt: now,
  };
}

export function touchTenant(value: Tenant): Tenant {
  return { ...value, updatedAt: isoTimestamp() };
}

/** Applies a transition and records it; callers must check legality first. */
export function applyTenantTransition(value: Tenant, to: TenantStatus): Tenant {
  const change: TenantChange = { at: isoTimestamp(), from: value.status, to };
  return {
    ...value,
    status: to,
    history: [...value.history, change],
    updatedAt: change.at,
  };
}

export function withTenantAmount(value: Tenant, amountCents: number): Tenant {
  if (!Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("tenant amount must be a non-negative integer");
  }
  return touchTenant({ ...value, amountCents });
}

export function withTenantLabel(value: Tenant, label: string): Tenant {
  if (label.trim().length === 0) {
    throw new ValidationError("tenant label must not be blank");
  }
  if (value.labels.includes(label)) {
    return value;
  }
  return touchTenant({ ...value, labels: [...value.labels, label].sort() });
}

export function withoutTenantLabel(value: Tenant, label: string): Tenant {
  if (!value.labels.includes(label)) {
    return value;
  }
  return touchTenant({ ...value, labels: value.labels.filter((item) => item !== label) });
}

export function validateTenant(value: Tenant): void {
  if (value.id.length === 0 || value.tenantId.length === 0) {
    throw new ValidationError("tenant requires both id and tenantId");
  }
  if (value.reference.trim().length === 0) {
    throw new ValidationError("tenant reference must not be blank");
  }
  if (!Number.isInteger(value.amountCents) || value.amountCents < 0) {
    throw new ValidationError("tenant amount must be a non-negative integer");
  }
  if (isBefore(value.updatedAt, value.createdAt)) {
    throw new ValidationError("tenant updatedAt precedes createdAt");
  }
}

export function compareTenant(left: Tenant, right: Tenant): number {
  const byCreation = left.createdAt.localeCompare(right.createdAt);
  return byCreation !== 0 ? byCreation : left.id.localeCompare(right.id);
}

export function summarizeTenant(value: Tenant): string {
  return `${value.reference} (${value.status}, ${(value.amountCents / 100).toFixed(2)})`;
}

export function tenantStatusCounts(values: readonly Tenant[]): Record<TenantStatus, number> {
  const counts: Record<TenantStatus, number> = { draft: 0, active: 0, settled: 0, cancelled: 0 };
  for (const value of values) {
    counts[value.status] += 1;
  }
  return counts;
}
