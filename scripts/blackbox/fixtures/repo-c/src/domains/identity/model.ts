import { isoTimestamp, isBefore } from "../../infra/time";
import { ValidationError } from "../../infra/errors";

/** Durable shape of one identity record. */
export interface Identity {
  readonly id: string;
  readonly tenantId: string;
  status: IdentityStatus;
  amountCents: number;
  reference: string;
  labels: readonly string[];
  history: readonly IdentityChange[];
  readonly createdAt: string;
  updatedAt: string;
}

export interface IdentityChange {
  readonly at: string;
  readonly from: IdentityStatus;
  readonly to: IdentityStatus;
}

export type IdentityStatus = "draft" | "active" | "settled" | "cancelled";

export const identityStatuses: readonly IdentityStatus[] = ["draft", "active", "settled", "cancelled"];

/** Legal forward transitions for a identity record; anything else is rejected upstream. */
const transitions: Record<IdentityStatus, readonly IdentityStatus[]> = {
  draft: ["active", "cancelled"],
  active: ["settled", "cancelled"],
  settled: [],
  cancelled: [],
};

export function canIdentityTransition(from: IdentityStatus, to: IdentityStatus): boolean {
  return transitions[from].includes(to);
}

export function isIdentityTerminal(value: Identity): boolean {
  return transitions[value.status].length === 0;
}

export function newIdentity(id: string, tenantId: string, reference: string): Identity {
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

export function touchIdentity(value: Identity): Identity {
  return { ...value, updatedAt: isoTimestamp() };
}

/** Applies a transition and records it; callers must check legality first. */
export function applyIdentityTransition(value: Identity, to: IdentityStatus): Identity {
  const change: IdentityChange = { at: isoTimestamp(), from: value.status, to };
  return {
    ...value,
    status: to,
    history: [...value.history, change],
    updatedAt: change.at,
  };
}

export function withIdentityAmount(value: Identity, amountCents: number): Identity {
  if (!Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("identity amount must be a non-negative integer");
  }
  return touchIdentity({ ...value, amountCents });
}

export function withIdentityLabel(value: Identity, label: string): Identity {
  if (label.trim().length === 0) {
    throw new ValidationError("identity label must not be blank");
  }
  if (value.labels.includes(label)) {
    return value;
  }
  return touchIdentity({ ...value, labels: [...value.labels, label].sort() });
}

export function withoutIdentityLabel(value: Identity, label: string): Identity {
  if (!value.labels.includes(label)) {
    return value;
  }
  return touchIdentity({ ...value, labels: value.labels.filter((item) => item !== label) });
}

export function validateIdentity(value: Identity): void {
  if (value.id.length === 0 || value.tenantId.length === 0) {
    throw new ValidationError("identity requires both id and tenantId");
  }
  if (value.reference.trim().length === 0) {
    throw new ValidationError("identity reference must not be blank");
  }
  if (!Number.isInteger(value.amountCents) || value.amountCents < 0) {
    throw new ValidationError("identity amount must be a non-negative integer");
  }
  if (isBefore(value.updatedAt, value.createdAt)) {
    throw new ValidationError("identity updatedAt precedes createdAt");
  }
}

export function compareIdentity(left: Identity, right: Identity): number {
  const byCreation = left.createdAt.localeCompare(right.createdAt);
  return byCreation !== 0 ? byCreation : left.id.localeCompare(right.id);
}

export function summarizeIdentity(value: Identity): string {
  return `${value.reference} (${value.status}, ${(value.amountCents / 100).toFixed(2)})`;
}

export function identityStatusCounts(values: readonly Identity[]): Record<IdentityStatus, number> {
  const counts: Record<IdentityStatus, number> = { draft: 0, active: 0, settled: 0, cancelled: 0 };
  for (const value of values) {
    counts[value.status] += 1;
  }
  return counts;
}
