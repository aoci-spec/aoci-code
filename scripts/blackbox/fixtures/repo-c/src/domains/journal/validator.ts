import { JournalStatus, journalStatuses } from "./model";
import { ValidationError } from "../../infra/errors";

export interface JournalCreateInput {
  id: string;
  reference: string;
  amountCents: number;
}

export interface JournalTransitionInput {
  status: JournalStatus;
}

export interface JournalLabelInput {
  label: string;
}

export interface JournalPageInput {
  offset: number;
  limit: number;
}

function requireRecord(body: unknown, what: string): Record<string, unknown> {
  if (typeof body !== "object" || body === null || Array.isArray(body)) {
    throw new ValidationError(`journal ${what} body must be an object`);
  }
  return body as Record<string, unknown>;
}

function requireText(value: unknown, field: string, max = 190): string {
  if (typeof value !== "string" || value.trim().length === 0) {
    throw new ValidationError(`journal.${field} must be a non-empty string`);
  }
  if (value.length > max) {
    throw new ValidationError(`journal.${field} must be at most ${max} characters`);
  }
  return value;
}

/** Request-shape validation for journal batch writes; never trusts client types. */
export function parseJournalCreate(body: unknown): JournalCreateInput {
  const record = requireRecord(body, "create");
  const id = requireText(record.id, "id", 64);
  const reference = requireText(record.reference, "reference");
  const amountCents = record.amountCents ?? 0;
  if (typeof amountCents !== "number" || !Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("journal.amountCents must be a non-negative integer");
  }
  return { id, reference, amountCents };
}

export function parseJournalTransition(body: unknown): JournalTransitionInput {
  const record = requireRecord(body, "transition");
  const status = record.status;
  if (typeof status !== "string" || !journalStatuses.includes(status as JournalStatus)) {
    throw new ValidationError(`journal.status must be one of ${journalStatuses.join(", ")}`);
  }
  return { status: status as JournalStatus };
}

export function parseJournalLabel(body: unknown): JournalLabelInput {
  const record = requireRecord(body, "label");
  return { label: requireText(record.label, "label", 40) };
}

export function parseJournalPage(query: unknown): JournalPageInput {
  const record = requireRecord(query ?? {}, "page");
  const offset = record.offset === undefined ? 0 : Number(record.offset);
  const limit = record.limit === undefined ? 50 : Number(record.limit);
  if (!Number.isInteger(offset) || offset < 0) {
    throw new ValidationError("journal.offset must be a non-negative integer");
  }
  if (!Number.isInteger(limit) || limit < 1 || limit > 200) {
    throw new ValidationError("journal.limit must be between 1 and 200");
  }
  return { offset, limit };
}
