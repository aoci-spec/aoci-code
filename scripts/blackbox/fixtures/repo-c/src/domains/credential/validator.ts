import { CredentialStatus, credentialStatuses } from "./model";
import { ValidationError } from "../../infra/errors";

export interface CredentialCreateInput {
  id: string;
  reference: string;
  amountCents: number;
}

export interface CredentialTransitionInput {
  status: CredentialStatus;
}

export interface CredentialLabelInput {
  label: string;
}

export interface CredentialPageInput {
  offset: number;
  limit: number;
}

function requireRecord(body: unknown, what: string): Record<string, unknown> {
  if (typeof body !== "object" || body === null || Array.isArray(body)) {
    throw new ValidationError(`credential ${what} body must be an object`);
  }
  return body as Record<string, unknown>;
}

function requireText(value: unknown, field: string, max = 190): string {
  if (typeof value !== "string" || value.trim().length === 0) {
    throw new ValidationError(`credential.${field} must be a non-empty string`);
  }
  if (value.length > max) {
    throw new ValidationError(`credential.${field} must be at most ${max} characters`);
  }
  return value;
}

/** Request-shape validation for stored credential writes; never trusts client types. */
export function parseCredentialCreate(body: unknown): CredentialCreateInput {
  const record = requireRecord(body, "create");
  const id = requireText(record.id, "id", 64);
  const reference = requireText(record.reference, "reference");
  const amountCents = record.amountCents ?? 0;
  if (typeof amountCents !== "number" || !Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("credential.amountCents must be a non-negative integer");
  }
  return { id, reference, amountCents };
}

export function parseCredentialTransition(body: unknown): CredentialTransitionInput {
  const record = requireRecord(body, "transition");
  const status = record.status;
  if (typeof status !== "string" || !credentialStatuses.includes(status as CredentialStatus)) {
    throw new ValidationError(`credential.status must be one of ${credentialStatuses.join(", ")}`);
  }
  return { status: status as CredentialStatus };
}

export function parseCredentialLabel(body: unknown): CredentialLabelInput {
  const record = requireRecord(body, "label");
  return { label: requireText(record.label, "label", 40) };
}

export function parseCredentialPage(query: unknown): CredentialPageInput {
  const record = requireRecord(query ?? {}, "page");
  const offset = record.offset === undefined ? 0 : Number(record.offset);
  const limit = record.limit === undefined ? 50 : Number(record.limit);
  if (!Number.isInteger(offset) || offset < 0) {
    throw new ValidationError("credential.offset must be a non-negative integer");
  }
  if (!Number.isInteger(limit) || limit < 1 || limit > 200) {
    throw new ValidationError("credential.limit must be between 1 and 200");
  }
  return { offset, limit };
}
