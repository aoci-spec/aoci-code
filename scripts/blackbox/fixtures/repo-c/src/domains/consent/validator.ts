import { ConsentStatus, consentStatuses } from "./model";
import { ValidationError } from "../../infra/errors";

export interface ConsentCreateInput {
  id: string;
  reference: string;
  amountCents: number;
}

export interface ConsentTransitionInput {
  status: ConsentStatus;
}

export interface ConsentLabelInput {
  label: string;
}

export interface ConsentPageInput {
  offset: number;
  limit: number;
}

function requireRecord(body: unknown, what: string): Record<string, unknown> {
  if (typeof body !== "object" || body === null || Array.isArray(body)) {
    throw new ValidationError(`consent ${what} body must be an object`);
  }
  return body as Record<string, unknown>;
}

function requireText(value: unknown, field: string, max = 190): string {
  if (typeof value !== "string" || value.trim().length === 0) {
    throw new ValidationError(`consent.${field} must be a non-empty string`);
  }
  if (value.length > max) {
    throw new ValidationError(`consent.${field} must be at most ${max} characters`);
  }
  return value;
}

/** Request-shape validation for privacy consent writes; never trusts client types. */
export function parseConsentCreate(body: unknown): ConsentCreateInput {
  const record = requireRecord(body, "create");
  const id = requireText(record.id, "id", 64);
  const reference = requireText(record.reference, "reference");
  const amountCents = record.amountCents ?? 0;
  if (typeof amountCents !== "number" || !Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("consent.amountCents must be a non-negative integer");
  }
  return { id, reference, amountCents };
}

export function parseConsentTransition(body: unknown): ConsentTransitionInput {
  const record = requireRecord(body, "transition");
  const status = record.status;
  if (typeof status !== "string" || !consentStatuses.includes(status as ConsentStatus)) {
    throw new ValidationError(`consent.status must be one of ${consentStatuses.join(", ")}`);
  }
  return { status: status as ConsentStatus };
}

export function parseConsentLabel(body: unknown): ConsentLabelInput {
  const record = requireRecord(body, "label");
  return { label: requireText(record.label, "label", 40) };
}

export function parseConsentPage(query: unknown): ConsentPageInput {
  const record = requireRecord(query ?? {}, "page");
  const offset = record.offset === undefined ? 0 : Number(record.offset);
  const limit = record.limit === undefined ? 50 : Number(record.limit);
  if (!Number.isInteger(offset) || offset < 0) {
    throw new ValidationError("consent.offset must be a non-negative integer");
  }
  if (!Number.isInteger(limit) || limit < 1 || limit > 200) {
    throw new ValidationError("consent.limit must be between 1 and 200");
  }
  return { offset, limit };
}
