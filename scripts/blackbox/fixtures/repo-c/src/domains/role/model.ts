import { isoTimestamp, isBefore } from "../../infra/time";
import { ValidationError } from "../../infra/errors";

/** Durable shape of one authorization role. */
export interface Role {
  readonly id: string;
  readonly tenantId: string;
  status: RoleStatus;
  amountCents: number;
  reference: string;
  labels: readonly string[];
  history: readonly RoleChange[];
  readonly createdAt: string;
  updatedAt: string;
}

export interface RoleChange {
  readonly at: string;
  readonly from: RoleStatus;
  readonly to: RoleStatus;
}

export type RoleStatus = "draft" | "active" | "settled" | "cancelled";

export const roleStatuses: readonly RoleStatus[] = ["draft", "active", "settled", "cancelled"];

/** Legal forward transitions for a authorization role; anything else is rejected upstream. */
const transitions: Record<RoleStatus, readonly RoleStatus[]> = {
  draft: ["active", "cancelled"],
  active: ["settled", "cancelled"],
  settled: [],
  cancelled: [],
};

export function canRoleTransition(from: RoleStatus, to: RoleStatus): boolean {
  return transitions[from].includes(to);
}

export function isRoleTerminal(value: Role): boolean {
  return transitions[value.status].length === 0;
}

export function newRole(id: string, tenantId: string, reference: string): Role {
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

export function touchRole(value: Role): Role {
  return { ...value, updatedAt: isoTimestamp() };
}

/** Applies a transition and records it; callers must check legality first. */
export function applyRoleTransition(value: Role, to: RoleStatus): Role {
  const change: RoleChange = { at: isoTimestamp(), from: value.status, to };
  return {
    ...value,
    status: to,
    history: [...value.history, change],
    updatedAt: change.at,
  };
}

export function withRoleAmount(value: Role, amountCents: number): Role {
  if (!Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("role amount must be a non-negative integer");
  }
  return touchRole({ ...value, amountCents });
}

export function withRoleLabel(value: Role, label: string): Role {
  if (label.trim().length === 0) {
    throw new ValidationError("role label must not be blank");
  }
  if (value.labels.includes(label)) {
    return value;
  }
  return touchRole({ ...value, labels: [...value.labels, label].sort() });
}

export function withoutRoleLabel(value: Role, label: string): Role {
  if (!value.labels.includes(label)) {
    return value;
  }
  return touchRole({ ...value, labels: value.labels.filter((item) => item !== label) });
}

export function validateRole(value: Role): void {
  if (value.id.length === 0 || value.tenantId.length === 0) {
    throw new ValidationError("role requires both id and tenantId");
  }
  if (value.reference.trim().length === 0) {
    throw new ValidationError("role reference must not be blank");
  }
  if (!Number.isInteger(value.amountCents) || value.amountCents < 0) {
    throw new ValidationError("role amount must be a non-negative integer");
  }
  if (isBefore(value.updatedAt, value.createdAt)) {
    throw new ValidationError("role updatedAt precedes createdAt");
  }
}

export function compareRole(left: Role, right: Role): number {
  const byCreation = left.createdAt.localeCompare(right.createdAt);
  return byCreation !== 0 ? byCreation : left.id.localeCompare(right.id);
}

export function summarizeRole(value: Role): string {
  return `${value.reference} (${value.status}, ${(value.amountCents / 100).toFixed(2)})`;
}

export function roleStatusCounts(values: readonly Role[]): Record<RoleStatus, number> {
  const counts: Record<RoleStatus, number> = { draft: 0, active: 0, settled: 0, cancelled: 0 };
  for (const value of values) {
    counts[value.status] += 1;
  }
  return counts;
}
