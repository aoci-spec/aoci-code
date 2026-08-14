import { WarrantyStatus, warrantyStatuses } from "./model";
import { ValidationError } from "../../infra/errors";

export interface WarrantyCreateInput {
  id: string;
  reference: string;
  amountCents: number;
}

export interface WarrantyTransitionInput {
  status: WarrantyStatus;
}

export interface WarrantyLabelInput {
  label: string;
}

export interface WarrantyPageInput {
  offset: number;
  limit: number;
}

function requireRecord(body: unknown, what: string): Record<string, unknown> {
  if (typeof body !== "object" || body === null || Array.isArray(body)) {
    throw new ValidationError(`warranty ${what} body must be an object`);
  }
  return body as Record<string, unknown>;
}

function requireText(value: unknown, field: string, max = 190): string {
  if (typeof value !== "string" || value.trim().length === 0) {
    throw new ValidationError(`warranty.${field} must be a non-empty string`);
  }
  if (value.length > max) {
    throw new ValidationError(`warranty.${field} must be at most ${max} characters`);
  }
  return value;
}

/** Request-shape validation for warranty claim writes; never trusts client types. */
export function parseWarrantyCreate(body: unknown): WarrantyCreateInput {
  const record = requireRecord(body, "create");
  const id = requireText(record.id, "id", 64);
  const reference = requireText(record.reference, "reference");
  const amountCents = record.amountCents ?? 0;
  if (typeof amountCents !== "number" || !Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("warranty.amountCents must be a non-negative integer");
  }
  return { id, reference, amountCents };
}

export function parseWarrantyTransition(body: unknown): WarrantyTransitionInput {
  const record = requireRecord(body, "transition");
  const status = record.status;
  if (typeof status !== "string" || !warrantyStatuses.includes(status as WarrantyStatus)) {
    throw new ValidationError(`warranty.status must be one of ${warrantyStatuses.join(", ")}`);
  }
  return { status: status as WarrantyStatus };
}

export function parseWarrantyLabel(body: unknown): WarrantyLabelInput {
  const record = requireRecord(body, "label");
  return { label: requireText(record.label, "label", 40) };
}

export function parseWarrantyPage(query: unknown): WarrantyPageInput {
  const record = requireRecord(query ?? {}, "page");
  const offset = record.offset === undefined ? 0 : Number(record.offset);
  const limit = record.limit === undefined ? 50 : Number(record.limit);
  if (!Number.isInteger(offset) || offset < 0) {
    throw new ValidationError("warranty.offset must be a non-negative integer");
  }
  if (!Number.isInteger(limit) || limit < 1 || limit > 200) {
    throw new ValidationError("warranty.limit must be between 1 and 200");
  }
  return { offset, limit };
}
