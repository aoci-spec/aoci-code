import { isoTimestamp, isBefore } from "../../infra/time";
import { ValidationError } from "../../infra/errors";

/** Durable shape of one contact person. */
export interface Contact {
  readonly id: string;
  readonly tenantId: string;
  status: ContactStatus;
  amountCents: number;
  reference: string;
  labels: readonly string[];
  history: readonly ContactChange[];
  readonly createdAt: string;
  updatedAt: string;
}

export interface ContactChange {
  readonly at: string;
  readonly from: ContactStatus;
  readonly to: ContactStatus;
}

export type ContactStatus = "draft" | "active" | "settled" | "cancelled";

export const contactStatuses: readonly ContactStatus[] = ["draft", "active", "settled", "cancelled"];

/** Legal forward transitions for a contact person; anything else is rejected upstream. */
const transitions: Record<ContactStatus, readonly ContactStatus[]> = {
  draft: ["active", "cancelled"],
  active: ["settled", "cancelled"],
  settled: [],
  cancelled: [],
};

export function canContactTransition(from: ContactStatus, to: ContactStatus): boolean {
  return transitions[from].includes(to);
}

export function isContactTerminal(value: Contact): boolean {
  return transitions[value.status].length === 0;
}

export function newContact(id: string, tenantId: string, reference: string): Contact {
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

export function touchContact(value: Contact): Contact {
  return { ...value, updatedAt: isoTimestamp() };
}

/** Applies a transition and records it; callers must check legality first. */
export function applyContactTransition(value: Contact, to: ContactStatus): Contact {
  const change: ContactChange = { at: isoTimestamp(), from: value.status, to };
  return {
    ...value,
    status: to,
    history: [...value.history, change],
    updatedAt: change.at,
  };
}

export function withContactAmount(value: Contact, amountCents: number): Contact {
  if (!Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("contact amount must be a non-negative integer");
  }
  return touchContact({ ...value, amountCents });
}

export function withContactLabel(value: Contact, label: string): Contact {
  if (label.trim().length === 0) {
    throw new ValidationError("contact label must not be blank");
  }
  if (value.labels.includes(label)) {
    return value;
  }
  return touchContact({ ...value, labels: [...value.labels, label].sort() });
}

export function withoutContactLabel(value: Contact, label: string): Contact {
  if (!value.labels.includes(label)) {
    return value;
  }
  return touchContact({ ...value, labels: value.labels.filter((item) => item !== label) });
}

export function validateContact(value: Contact): void {
  if (value.id.length === 0 || value.tenantId.length === 0) {
    throw new ValidationError("contact requires both id and tenantId");
  }
  if (value.reference.trim().length === 0) {
    throw new ValidationError("contact reference must not be blank");
  }
  if (!Number.isInteger(value.amountCents) || value.amountCents < 0) {
    throw new ValidationError("contact amount must be a non-negative integer");
  }
  if (isBefore(value.updatedAt, value.createdAt)) {
    throw new ValidationError("contact updatedAt precedes createdAt");
  }
}

export function compareContact(left: Contact, right: Contact): number {
  const byCreation = left.createdAt.localeCompare(right.createdAt);
  return byCreation !== 0 ? byCreation : left.id.localeCompare(right.id);
}

export function summarizeContact(value: Contact): string {
  return `${value.reference} (${value.status}, ${(value.amountCents / 100).toFixed(2)})`;
}

export function contactStatusCounts(values: readonly Contact[]): Record<ContactStatus, number> {
  const counts: Record<ContactStatus, number> = { draft: 0, active: 0, settled: 0, cancelled: 0 };
  for (const value of values) {
    counts[value.status] += 1;
  }
  return counts;
}
