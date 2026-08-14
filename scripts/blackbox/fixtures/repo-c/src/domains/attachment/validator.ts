import { AttachmentStatus, attachmentStatuses } from "./model";
import { ValidationError } from "../../infra/errors";

export interface AttachmentCreateInput {
  id: string;
  reference: string;
  amountCents: number;
}

export interface AttachmentTransitionInput {
  status: AttachmentStatus;
}

export interface AttachmentLabelInput {
  label: string;
}

export interface AttachmentPageInput {
  offset: number;
  limit: number;
}

function requireRecord(body: unknown, what: string): Record<string, unknown> {
  if (typeof body !== "object" || body === null || Array.isArray(body)) {
    throw new ValidationError(`attachment ${what} body must be an object`);
  }
  return body as Record<string, unknown>;
}

function requireText(value: unknown, field: string, max = 190): string {
  if (typeof value !== "string" || value.trim().length === 0) {
    throw new ValidationError(`attachment.${field} must be a non-empty string`);
  }
  if (value.length > max) {
    throw new ValidationError(`attachment.${field} must be at most ${max} characters`);
  }
  return value;
}

/** Request-shape validation for file attachment writes; never trusts client types. */
export function parseAttachmentCreate(body: unknown): AttachmentCreateInput {
  const record = requireRecord(body, "create");
  const id = requireText(record.id, "id", 64);
  const reference = requireText(record.reference, "reference");
  const amountCents = record.amountCents ?? 0;
  if (typeof amountCents !== "number" || !Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("attachment.amountCents must be a non-negative integer");
  }
  return { id, reference, amountCents };
}

export function parseAttachmentTransition(body: unknown): AttachmentTransitionInput {
  const record = requireRecord(body, "transition");
  const status = record.status;
  if (typeof status !== "string" || !attachmentStatuses.includes(status as AttachmentStatus)) {
    throw new ValidationError(`attachment.status must be one of ${attachmentStatuses.join(", ")}`);
  }
  return { status: status as AttachmentStatus };
}

export function parseAttachmentLabel(body: unknown): AttachmentLabelInput {
  const record = requireRecord(body, "label");
  return { label: requireText(record.label, "label", 40) };
}

export function parseAttachmentPage(query: unknown): AttachmentPageInput {
  const record = requireRecord(query ?? {}, "page");
  const offset = record.offset === undefined ? 0 : Number(record.offset);
  const limit = record.limit === undefined ? 50 : Number(record.limit);
  if (!Number.isInteger(offset) || offset < 0) {
    throw new ValidationError("attachment.offset must be a non-negative integer");
  }
  if (!Number.isInteger(limit) || limit < 1 || limit > 200) {
    throw new ValidationError("attachment.limit must be between 1 and 200");
  }
  return { offset, limit };
}
