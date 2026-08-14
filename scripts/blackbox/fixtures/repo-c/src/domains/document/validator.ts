import { DocumentStatus, documentStatuses } from "./model";
import { ValidationError } from "../../infra/errors";

export interface DocumentCreateInput {
  id: string;
  reference: string;
  amountCents: number;
}

export interface DocumentTransitionInput {
  status: DocumentStatus;
}

export interface DocumentLabelInput {
  label: string;
}

export interface DocumentPageInput {
  offset: number;
  limit: number;
}

function requireRecord(body: unknown, what: string): Record<string, unknown> {
  if (typeof body !== "object" || body === null || Array.isArray(body)) {
    throw new ValidationError(`document ${what} body must be an object`);
  }
  return body as Record<string, unknown>;
}

function requireText(value: unknown, field: string, max = 190): string {
  if (typeof value !== "string" || value.trim().length === 0) {
    throw new ValidationError(`document.${field} must be a non-empty string`);
  }
  if (value.length > max) {
    throw new ValidationError(`document.${field} must be at most ${max} characters`);
  }
  return value;
}

/** Request-shape validation for stored document writes; never trusts client types. */
export function parseDocumentCreate(body: unknown): DocumentCreateInput {
  const record = requireRecord(body, "create");
  const id = requireText(record.id, "id", 64);
  const reference = requireText(record.reference, "reference");
  const amountCents = record.amountCents ?? 0;
  if (typeof amountCents !== "number" || !Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("document.amountCents must be a non-negative integer");
  }
  return { id, reference, amountCents };
}

export function parseDocumentTransition(body: unknown): DocumentTransitionInput {
  const record = requireRecord(body, "transition");
  const status = record.status;
  if (typeof status !== "string" || !documentStatuses.includes(status as DocumentStatus)) {
    throw new ValidationError(`document.status must be one of ${documentStatuses.join(", ")}`);
  }
  return { status: status as DocumentStatus };
}

export function parseDocumentLabel(body: unknown): DocumentLabelInput {
  const record = requireRecord(body, "label");
  return { label: requireText(record.label, "label", 40) };
}

export function parseDocumentPage(query: unknown): DocumentPageInput {
  const record = requireRecord(query ?? {}, "page");
  const offset = record.offset === undefined ? 0 : Number(record.offset);
  const limit = record.limit === undefined ? 50 : Number(record.limit);
  if (!Number.isInteger(offset) || offset < 0) {
    throw new ValidationError("document.offset must be a non-negative integer");
  }
  if (!Number.isInteger(limit) || limit < 1 || limit > 200) {
    throw new ValidationError("document.limit must be between 1 and 200");
  }
  return { offset, limit };
}
