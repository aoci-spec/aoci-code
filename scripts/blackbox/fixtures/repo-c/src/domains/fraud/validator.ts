import { FraudStatus, fraudStatuses } from "./model";
import { ValidationError } from "../../infra/errors";

export interface FraudCreateInput {
  id: string;
  reference: string;
  amountCents: number;
}

export interface FraudTransitionInput {
  status: FraudStatus;
}

export interface FraudLabelInput {
  label: string;
}

export interface FraudPageInput {
  offset: number;
  limit: number;
}

function requireRecord(body: unknown, what: string): Record<string, unknown> {
  if (typeof body !== "object" || body === null || Array.isArray(body)) {
    throw new ValidationError(`fraud ${what} body must be an object`);
  }
  return body as Record<string, unknown>;
}

function requireText(value: unknown, field: string, max = 190): string {
  if (typeof value !== "string" || value.trim().length === 0) {
    throw new ValidationError(`fraud.${field} must be a non-empty string`);
  }
  if (value.length > max) {
    throw new ValidationError(`fraud.${field} must be at most ${max} characters`);
  }
  return value;
}

/** Request-shape validation for fraud signal writes; never trusts client types. */
export function parseFraudCreate(body: unknown): FraudCreateInput {
  const record = requireRecord(body, "create");
  const id = requireText(record.id, "id", 64);
  const reference = requireText(record.reference, "reference");
  const amountCents = record.amountCents ?? 0;
  if (typeof amountCents !== "number" || !Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("fraud.amountCents must be a non-negative integer");
  }
  return { id, reference, amountCents };
}

export function parseFraudTransition(body: unknown): FraudTransitionInput {
  const record = requireRecord(body, "transition");
  const status = record.status;
  if (typeof status !== "string" || !fraudStatuses.includes(status as FraudStatus)) {
    throw new ValidationError(`fraud.status must be one of ${fraudStatuses.join(", ")}`);
  }
  return { status: status as FraudStatus };
}

export function parseFraudLabel(body: unknown): FraudLabelInput {
  const record = requireRecord(body, "label");
  return { label: requireText(record.label, "label", 40) };
}

export function parseFraudPage(query: unknown): FraudPageInput {
  const record = requireRecord(query ?? {}, "page");
  const offset = record.offset === undefined ? 0 : Number(record.offset);
  const limit = record.limit === undefined ? 50 : Number(record.limit);
  if (!Number.isInteger(offset) || offset < 0) {
    throw new ValidationError("fraud.offset must be a non-negative integer");
  }
  if (!Number.isInteger(limit) || limit < 1 || limit > 200) {
    throw new ValidationError("fraud.limit must be between 1 and 200");
  }
  return { offset, limit };
}
