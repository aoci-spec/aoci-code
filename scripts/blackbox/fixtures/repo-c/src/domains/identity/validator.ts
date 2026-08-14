import { IdentityStatus, identityStatuses } from "./model";
import { ValidationError } from "../../infra/errors";

export interface IdentityCreateInput {
  id: string;
  reference: string;
  amountCents: number;
}

export interface IdentityTransitionInput {
  status: IdentityStatus;
}

export interface IdentityLabelInput {
  label: string;
}

export interface IdentityPageInput {
  offset: number;
  limit: number;
}

function requireRecord(body: unknown, what: string): Record<string, unknown> {
  if (typeof body !== "object" || body === null || Array.isArray(body)) {
    throw new ValidationError(`identity ${what} body must be an object`);
  }
  return body as Record<string, unknown>;
}

function requireText(value: unknown, field: string, max = 190): string {
  if (typeof value !== "string" || value.trim().length === 0) {
    throw new ValidationError(`identity.${field} must be a non-empty string`);
  }
  if (value.length > max) {
    throw new ValidationError(`identity.${field} must be at most ${max} characters`);
  }
  return value;
}

/** Request-shape validation for identity record writes; never trusts client types. */
export function parseIdentityCreate(body: unknown): IdentityCreateInput {
  const record = requireRecord(body, "create");
  const id = requireText(record.id, "id", 64);
  const reference = requireText(record.reference, "reference");
  const amountCents = record.amountCents ?? 0;
  if (typeof amountCents !== "number" || !Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("identity.amountCents must be a non-negative integer");
  }
  return { id, reference, amountCents };
}

export function parseIdentityTransition(body: unknown): IdentityTransitionInput {
  const record = requireRecord(body, "transition");
  const status = record.status;
  if (typeof status !== "string" || !identityStatuses.includes(status as IdentityStatus)) {
    throw new ValidationError(`identity.status must be one of ${identityStatuses.join(", ")}`);
  }
  return { status: status as IdentityStatus };
}

export function parseIdentityLabel(body: unknown): IdentityLabelInput {
  const record = requireRecord(body, "label");
  return { label: requireText(record.label, "label", 40) };
}

export function parseIdentityPage(query: unknown): IdentityPageInput {
  const record = requireRecord(query ?? {}, "page");
  const offset = record.offset === undefined ? 0 : Number(record.offset);
  const limit = record.limit === undefined ? 50 : Number(record.limit);
  if (!Number.isInteger(offset) || offset < 0) {
    throw new ValidationError("identity.offset must be a non-negative integer");
  }
  if (!Number.isInteger(limit) || limit < 1 || limit > 200) {
    throw new ValidationError("identity.limit must be between 1 and 200");
  }
  return { offset, limit };
}
