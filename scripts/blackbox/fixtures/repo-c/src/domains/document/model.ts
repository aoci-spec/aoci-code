import { isoTimestamp, isBefore } from "../../infra/time";
import { ValidationError } from "../../infra/errors";

/** Durable shape of one stored document. */
export interface Document {
  readonly id: string;
  readonly tenantId: string;
  status: DocumentStatus;
  amountCents: number;
  reference: string;
  labels: readonly string[];
  history: readonly DocumentChange[];
  readonly createdAt: string;
  updatedAt: string;
}

export interface DocumentChange {
  readonly at: string;
  readonly from: DocumentStatus;
  readonly to: DocumentStatus;
}

export type DocumentStatus = "draft" | "active" | "settled" | "cancelled";

export const documentStatuses: readonly DocumentStatus[] = ["draft", "active", "settled", "cancelled"];

/** Legal forward transitions for a stored document; anything else is rejected upstream. */
const transitions: Record<DocumentStatus, readonly DocumentStatus[]> = {
  draft: ["active", "cancelled"],
  active: ["settled", "cancelled"],
  settled: [],
  cancelled: [],
};

export function canDocumentTransition(from: DocumentStatus, to: DocumentStatus): boolean {
  return transitions[from].includes(to);
}

export function isDocumentTerminal(value: Document): boolean {
  return transitions[value.status].length === 0;
}

export function newDocument(id: string, tenantId: string, reference: string): Document {
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

export function touchDocument(value: Document): Document {
  return { ...value, updatedAt: isoTimestamp() };
}

/** Applies a transition and records it; callers must check legality first. */
export function applyDocumentTransition(value: Document, to: DocumentStatus): Document {
  const change: DocumentChange = { at: isoTimestamp(), from: value.status, to };
  return {
    ...value,
    status: to,
    history: [...value.history, change],
    updatedAt: change.at,
  };
}

export function withDocumentAmount(value: Document, amountCents: number): Document {
  if (!Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("document amount must be a non-negative integer");
  }
  return touchDocument({ ...value, amountCents });
}

export function withDocumentLabel(value: Document, label: string): Document {
  if (label.trim().length === 0) {
    throw new ValidationError("document label must not be blank");
  }
  if (value.labels.includes(label)) {
    return value;
  }
  return touchDocument({ ...value, labels: [...value.labels, label].sort() });
}

export function withoutDocumentLabel(value: Document, label: string): Document {
  if (!value.labels.includes(label)) {
    return value;
  }
  return touchDocument({ ...value, labels: value.labels.filter((item) => item !== label) });
}

export function validateDocument(value: Document): void {
  if (value.id.length === 0 || value.tenantId.length === 0) {
    throw new ValidationError("document requires both id and tenantId");
  }
  if (value.reference.trim().length === 0) {
    throw new ValidationError("document reference must not be blank");
  }
  if (!Number.isInteger(value.amountCents) || value.amountCents < 0) {
    throw new ValidationError("document amount must be a non-negative integer");
  }
  if (isBefore(value.updatedAt, value.createdAt)) {
    throw new ValidationError("document updatedAt precedes createdAt");
  }
}

export function compareDocument(left: Document, right: Document): number {
  const byCreation = left.createdAt.localeCompare(right.createdAt);
  return byCreation !== 0 ? byCreation : left.id.localeCompare(right.id);
}

export function summarizeDocument(value: Document): string {
  return `${value.reference} (${value.status}, ${(value.amountCents / 100).toFixed(2)})`;
}

export function documentStatusCounts(values: readonly Document[]): Record<DocumentStatus, number> {
  const counts: Record<DocumentStatus, number> = { draft: 0, active: 0, settled: 0, cancelled: 0 };
  for (const value of values) {
    counts[value.status] += 1;
  }
  return counts;
}
