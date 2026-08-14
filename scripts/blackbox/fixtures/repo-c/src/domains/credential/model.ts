import { isoTimestamp, isBefore } from "../../infra/time";
import { ValidationError } from "../../infra/errors";

/** Durable shape of one stored credential. */
export interface Credential {
  readonly id: string;
  readonly tenantId: string;
  status: CredentialStatus;
  amountCents: number;
  reference: string;
  labels: readonly string[];
  history: readonly CredentialChange[];
  readonly createdAt: string;
  updatedAt: string;
}

export interface CredentialChange {
  readonly at: string;
  readonly from: CredentialStatus;
  readonly to: CredentialStatus;
}

export type CredentialStatus = "draft" | "active" | "settled" | "cancelled";

export const credentialStatuses: readonly CredentialStatus[] = ["draft", "active", "settled", "cancelled"];

/** Legal forward transitions for a stored credential; anything else is rejected upstream. */
const transitions: Record<CredentialStatus, readonly CredentialStatus[]> = {
  draft: ["active", "cancelled"],
  active: ["settled", "cancelled"],
  settled: [],
  cancelled: [],
};

export function canCredentialTransition(from: CredentialStatus, to: CredentialStatus): boolean {
  return transitions[from].includes(to);
}

export function isCredentialTerminal(value: Credential): boolean {
  return transitions[value.status].length === 0;
}

export function newCredential(id: string, tenantId: string, reference: string): Credential {
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

export function touchCredential(value: Credential): Credential {
  return { ...value, updatedAt: isoTimestamp() };
}

/** Applies a transition and records it; callers must check legality first. */
export function applyCredentialTransition(value: Credential, to: CredentialStatus): Credential {
  const change: CredentialChange = { at: isoTimestamp(), from: value.status, to };
  return {
    ...value,
    status: to,
    history: [...value.history, change],
    updatedAt: change.at,
  };
}

export function withCredentialAmount(value: Credential, amountCents: number): Credential {
  if (!Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("credential amount must be a non-negative integer");
  }
  return touchCredential({ ...value, amountCents });
}

export function withCredentialLabel(value: Credential, label: string): Credential {
  if (label.trim().length === 0) {
    throw new ValidationError("credential label must not be blank");
  }
  if (value.labels.includes(label)) {
    return value;
  }
  return touchCredential({ ...value, labels: [...value.labels, label].sort() });
}

export function withoutCredentialLabel(value: Credential, label: string): Credential {
  if (!value.labels.includes(label)) {
    return value;
  }
  return touchCredential({ ...value, labels: value.labels.filter((item) => item !== label) });
}

export function validateCredential(value: Credential): void {
  if (value.id.length === 0 || value.tenantId.length === 0) {
    throw new ValidationError("credential requires both id and tenantId");
  }
  if (value.reference.trim().length === 0) {
    throw new ValidationError("credential reference must not be blank");
  }
  if (!Number.isInteger(value.amountCents) || value.amountCents < 0) {
    throw new ValidationError("credential amount must be a non-negative integer");
  }
  if (isBefore(value.updatedAt, value.createdAt)) {
    throw new ValidationError("credential updatedAt precedes createdAt");
  }
}

export function compareCredential(left: Credential, right: Credential): number {
  const byCreation = left.createdAt.localeCompare(right.createdAt);
  return byCreation !== 0 ? byCreation : left.id.localeCompare(right.id);
}

export function summarizeCredential(value: Credential): string {
  return `${value.reference} (${value.status}, ${(value.amountCents / 100).toFixed(2)})`;
}

export function credentialStatusCounts(values: readonly Credential[]): Record<CredentialStatus, number> {
  const counts: Record<CredentialStatus, number> = { draft: 0, active: 0, settled: 0, cancelled: 0 };
  for (const value of values) {
    counts[value.status] += 1;
  }
  return counts;
}
