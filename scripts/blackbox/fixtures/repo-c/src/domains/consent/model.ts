import { isoTimestamp, isBefore } from "../../infra/time";
import { ValidationError } from "../../infra/errors";

/** Durable shape of one privacy consent. */
export interface Consent {
  readonly id: string;
  readonly tenantId: string;
  status: ConsentStatus;
  amountCents: number;
  reference: string;
  labels: readonly string[];
  history: readonly ConsentChange[];
  readonly createdAt: string;
  updatedAt: string;
}

export interface ConsentChange {
  readonly at: string;
  readonly from: ConsentStatus;
  readonly to: ConsentStatus;
}

export type ConsentStatus = "draft" | "active" | "settled" | "cancelled";

export const consentStatuses: readonly ConsentStatus[] = ["draft", "active", "settled", "cancelled"];

/** Legal forward transitions for a privacy consent; anything else is rejected upstream. */
const transitions: Record<ConsentStatus, readonly ConsentStatus[]> = {
  draft: ["active", "cancelled"],
  active: ["settled", "cancelled"],
  settled: [],
  cancelled: [],
};

export function canConsentTransition(from: ConsentStatus, to: ConsentStatus): boolean {
  return transitions[from].includes(to);
}

export function isConsentTerminal(value: Consent): boolean {
  return transitions[value.status].length === 0;
}

export function newConsent(id: string, tenantId: string, reference: string): Consent {
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

export function touchConsent(value: Consent): Consent {
  return { ...value, updatedAt: isoTimestamp() };
}

/** Applies a transition and records it; callers must check legality first. */
export function applyConsentTransition(value: Consent, to: ConsentStatus): Consent {
  const change: ConsentChange = { at: isoTimestamp(), from: value.status, to };
  return {
    ...value,
    status: to,
    history: [...value.history, change],
    updatedAt: change.at,
  };
}

export function withConsentAmount(value: Consent, amountCents: number): Consent {
  if (!Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("consent amount must be a non-negative integer");
  }
  return touchConsent({ ...value, amountCents });
}

export function withConsentLabel(value: Consent, label: string): Consent {
  if (label.trim().length === 0) {
    throw new ValidationError("consent label must not be blank");
  }
  if (value.labels.includes(label)) {
    return value;
  }
  return touchConsent({ ...value, labels: [...value.labels, label].sort() });
}

export function withoutConsentLabel(value: Consent, label: string): Consent {
  if (!value.labels.includes(label)) {
    return value;
  }
  return touchConsent({ ...value, labels: value.labels.filter((item) => item !== label) });
}

export function validateConsent(value: Consent): void {
  if (value.id.length === 0 || value.tenantId.length === 0) {
    throw new ValidationError("consent requires both id and tenantId");
  }
  if (value.reference.trim().length === 0) {
    throw new ValidationError("consent reference must not be blank");
  }
  if (!Number.isInteger(value.amountCents) || value.amountCents < 0) {
    throw new ValidationError("consent amount must be a non-negative integer");
  }
  if (isBefore(value.updatedAt, value.createdAt)) {
    throw new ValidationError("consent updatedAt precedes createdAt");
  }
}

export function compareConsent(left: Consent, right: Consent): number {
  const byCreation = left.createdAt.localeCompare(right.createdAt);
  return byCreation !== 0 ? byCreation : left.id.localeCompare(right.id);
}

export function summarizeConsent(value: Consent): string {
  return `${value.reference} (${value.status}, ${(value.amountCents / 100).toFixed(2)})`;
}

export function consentStatusCounts(values: readonly Consent[]): Record<ConsentStatus, number> {
  const counts: Record<ConsentStatus, number> = { draft: 0, active: 0, settled: 0, cancelled: 0 };
  for (const value of values) {
    counts[value.status] += 1;
  }
  return counts;
}
