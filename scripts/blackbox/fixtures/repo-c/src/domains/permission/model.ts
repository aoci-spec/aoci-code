import { isoTimestamp, isBefore } from "../../infra/time";
import { ValidationError } from "../../infra/errors";

/** Durable shape of one permission grant. */
export interface Permission {
  readonly id: string;
  readonly tenantId: string;
  status: PermissionStatus;
  amountCents: number;
  reference: string;
  labels: readonly string[];
  history: readonly PermissionChange[];
  readonly createdAt: string;
  updatedAt: string;
}

export interface PermissionChange {
  readonly at: string;
  readonly from: PermissionStatus;
  readonly to: PermissionStatus;
}

export type PermissionStatus = "draft" | "active" | "settled" | "cancelled";

export const permissionStatuses: readonly PermissionStatus[] = ["draft", "active", "settled", "cancelled"];

/** Legal forward transitions for a permission grant; anything else is rejected upstream. */
const transitions: Record<PermissionStatus, readonly PermissionStatus[]> = {
  draft: ["active", "cancelled"],
  active: ["settled", "cancelled"],
  settled: [],
  cancelled: [],
};

export function canPermissionTransition(from: PermissionStatus, to: PermissionStatus): boolean {
  return transitions[from].includes(to);
}

export function isPermissionTerminal(value: Permission): boolean {
  return transitions[value.status].length === 0;
}

export function newPermission(id: string, tenantId: string, reference: string): Permission {
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

export function touchPermission(value: Permission): Permission {
  return { ...value, updatedAt: isoTimestamp() };
}

/** Applies a transition and records it; callers must check legality first. */
export function applyPermissionTransition(value: Permission, to: PermissionStatus): Permission {
  const change: PermissionChange = { at: isoTimestamp(), from: value.status, to };
  return {
    ...value,
    status: to,
    history: [...value.history, change],
    updatedAt: change.at,
  };
}

export function withPermissionAmount(value: Permission, amountCents: number): Permission {
  if (!Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("permission amount must be a non-negative integer");
  }
  return touchPermission({ ...value, amountCents });
}

export function withPermissionLabel(value: Permission, label: string): Permission {
  if (label.trim().length === 0) {
    throw new ValidationError("permission label must not be blank");
  }
  if (value.labels.includes(label)) {
    return value;
  }
  return touchPermission({ ...value, labels: [...value.labels, label].sort() });
}

export function withoutPermissionLabel(value: Permission, label: string): Permission {
  if (!value.labels.includes(label)) {
    return value;
  }
  return touchPermission({ ...value, labels: value.labels.filter((item) => item !== label) });
}

export function validatePermission(value: Permission): void {
  if (value.id.length === 0 || value.tenantId.length === 0) {
    throw new ValidationError("permission requires both id and tenantId");
  }
  if (value.reference.trim().length === 0) {
    throw new ValidationError("permission reference must not be blank");
  }
  if (!Number.isInteger(value.amountCents) || value.amountCents < 0) {
    throw new ValidationError("permission amount must be a non-negative integer");
  }
  if (isBefore(value.updatedAt, value.createdAt)) {
    throw new ValidationError("permission updatedAt precedes createdAt");
  }
}

export function comparePermission(left: Permission, right: Permission): number {
  const byCreation = left.createdAt.localeCompare(right.createdAt);
  return byCreation !== 0 ? byCreation : left.id.localeCompare(right.id);
}

export function summarizePermission(value: Permission): string {
  return `${value.reference} (${value.status}, ${(value.amountCents / 100).toFixed(2)})`;
}

export function permissionStatusCounts(values: readonly Permission[]): Record<PermissionStatus, number> {
  const counts: Record<PermissionStatus, number> = { draft: 0, active: 0, settled: 0, cancelled: 0 };
  for (const value of values) {
    counts[value.status] += 1;
  }
  return counts;
}
