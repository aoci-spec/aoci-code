import { isoTimestamp, isBefore } from "../../infra/time";
import { ValidationError } from "../../infra/errors";

/** Durable shape of one file attachment. */
export interface Attachment {
  readonly id: string;
  readonly tenantId: string;
  status: AttachmentStatus;
  amountCents: number;
  reference: string;
  labels: readonly string[];
  history: readonly AttachmentChange[];
  readonly createdAt: string;
  updatedAt: string;
}

export interface AttachmentChange {
  readonly at: string;
  readonly from: AttachmentStatus;
  readonly to: AttachmentStatus;
}

export type AttachmentStatus = "draft" | "active" | "settled" | "cancelled";

export const attachmentStatuses: readonly AttachmentStatus[] = ["draft", "active", "settled", "cancelled"];

/** Legal forward transitions for a file attachment; anything else is rejected upstream. */
const transitions: Record<AttachmentStatus, readonly AttachmentStatus[]> = {
  draft: ["active", "cancelled"],
  active: ["settled", "cancelled"],
  settled: [],
  cancelled: [],
};

export function canAttachmentTransition(from: AttachmentStatus, to: AttachmentStatus): boolean {
  return transitions[from].includes(to);
}

export function isAttachmentTerminal(value: Attachment): boolean {
  return transitions[value.status].length === 0;
}

export function newAttachment(id: string, tenantId: string, reference: string): Attachment {
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

export function touchAttachment(value: Attachment): Attachment {
  return { ...value, updatedAt: isoTimestamp() };
}

/** Applies a transition and records it; callers must check legality first. */
export function applyAttachmentTransition(value: Attachment, to: AttachmentStatus): Attachment {
  const change: AttachmentChange = { at: isoTimestamp(), from: value.status, to };
  return {
    ...value,
    status: to,
    history: [...value.history, change],
    updatedAt: change.at,
  };
}

export function withAttachmentAmount(value: Attachment, amountCents: number): Attachment {
  if (!Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("attachment amount must be a non-negative integer");
  }
  return touchAttachment({ ...value, amountCents });
}

export function withAttachmentLabel(value: Attachment, label: string): Attachment {
  if (label.trim().length === 0) {
    throw new ValidationError("attachment label must not be blank");
  }
  if (value.labels.includes(label)) {
    return value;
  }
  return touchAttachment({ ...value, labels: [...value.labels, label].sort() });
}

export function withoutAttachmentLabel(value: Attachment, label: string): Attachment {
  if (!value.labels.includes(label)) {
    return value;
  }
  return touchAttachment({ ...value, labels: value.labels.filter((item) => item !== label) });
}

export function validateAttachment(value: Attachment): void {
  if (value.id.length === 0 || value.tenantId.length === 0) {
    throw new ValidationError("attachment requires both id and tenantId");
  }
  if (value.reference.trim().length === 0) {
    throw new ValidationError("attachment reference must not be blank");
  }
  if (!Number.isInteger(value.amountCents) || value.amountCents < 0) {
    throw new ValidationError("attachment amount must be a non-negative integer");
  }
  if (isBefore(value.updatedAt, value.createdAt)) {
    throw new ValidationError("attachment updatedAt precedes createdAt");
  }
}

export function compareAttachment(left: Attachment, right: Attachment): number {
  const byCreation = left.createdAt.localeCompare(right.createdAt);
  return byCreation !== 0 ? byCreation : left.id.localeCompare(right.id);
}

export function summarizeAttachment(value: Attachment): string {
  return `${value.reference} (${value.status}, ${(value.amountCents / 100).toFixed(2)})`;
}

export function attachmentStatusCounts(values: readonly Attachment[]): Record<AttachmentStatus, number> {
  const counts: Record<AttachmentStatus, number> = { draft: 0, active: 0, settled: 0, cancelled: 0 };
  for (const value of values) {
    counts[value.status] += 1;
  }
  return counts;
}
