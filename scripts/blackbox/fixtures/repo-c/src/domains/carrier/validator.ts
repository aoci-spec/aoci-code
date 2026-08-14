import { CarrierStatus, carrierStatuses } from "./model";
import { ValidationError } from "../../infra/errors";

export interface CarrierCreateInput {
  id: string;
  reference: string;
  amountCents: number;
}

export interface CarrierTransitionInput {
  status: CarrierStatus;
}

export interface CarrierLabelInput {
  label: string;
}

export interface CarrierPageInput {
  offset: number;
  limit: number;
}

function requireRecord(body: unknown, what: string): Record<string, unknown> {
  if (typeof body !== "object" || body === null || Array.isArray(body)) {
    throw new ValidationError(`carrier ${what} body must be an object`);
  }
  return body as Record<string, unknown>;
}

function requireText(value: unknown, field: string, max = 190): string {
  if (typeof value !== "string" || value.trim().length === 0) {
    throw new ValidationError(`carrier.${field} must be a non-empty string`);
  }
  if (value.length > max) {
    throw new ValidationError(`carrier.${field} must be at most ${max} characters`);
  }
  return value;
}

/** Request-shape validation for delivery carrier writes; never trusts client types. */
export function parseCarrierCreate(body: unknown): CarrierCreateInput {
  const record = requireRecord(body, "create");
  const id = requireText(record.id, "id", 64);
  const reference = requireText(record.reference, "reference");
  const amountCents = record.amountCents ?? 0;
  if (typeof amountCents !== "number" || !Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("carrier.amountCents must be a non-negative integer");
  }
  return { id, reference, amountCents };
}

export function parseCarrierTransition(body: unknown): CarrierTransitionInput {
  const record = requireRecord(body, "transition");
  const status = record.status;
  if (typeof status !== "string" || !carrierStatuses.includes(status as CarrierStatus)) {
    throw new ValidationError(`carrier.status must be one of ${carrierStatuses.join(", ")}`);
  }
  return { status: status as CarrierStatus };
}

export function parseCarrierLabel(body: unknown): CarrierLabelInput {
  const record = requireRecord(body, "label");
  return { label: requireText(record.label, "label", 40) };
}

export function parseCarrierPage(query: unknown): CarrierPageInput {
  const record = requireRecord(query ?? {}, "page");
  const offset = record.offset === undefined ? 0 : Number(record.offset);
  const limit = record.limit === undefined ? 50 : Number(record.limit);
  if (!Number.isInteger(offset) || offset < 0) {
    throw new ValidationError("carrier.offset must be a non-negative integer");
  }
  if (!Number.isInteger(limit) || limit < 1 || limit > 200) {
    throw new ValidationError("carrier.limit must be between 1 and 200");
  }
  return { offset, limit };
}
