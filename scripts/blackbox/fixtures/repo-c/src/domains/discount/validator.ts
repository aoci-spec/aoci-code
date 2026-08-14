import { DiscountStatus, discountStatuses } from "./model";
import { ValidationError } from "../../infra/errors";

export interface DiscountCreateInput {
  id: string;
  reference: string;
  amountCents: number;
}

export interface DiscountTransitionInput {
  status: DiscountStatus;
}

export interface DiscountLabelInput {
  label: string;
}

export interface DiscountPageInput {
  offset: number;
  limit: number;
}

function requireRecord(body: unknown, what: string): Record<string, unknown> {
  if (typeof body !== "object" || body === null || Array.isArray(body)) {
    throw new ValidationError(`discount ${what} body must be an object`);
  }
  return body as Record<string, unknown>;
}

function requireText(value: unknown, field: string, max = 190): string {
  if (typeof value !== "string" || value.trim().length === 0) {
    throw new ValidationError(`discount.${field} must be a non-empty string`);
  }
  if (value.length > max) {
    throw new ValidationError(`discount.${field} must be at most ${max} characters`);
  }
  return value;
}

/** Request-shape validation for discount rule writes; never trusts client types. */
export function parseDiscountCreate(body: unknown): DiscountCreateInput {
  const record = requireRecord(body, "create");
  const id = requireText(record.id, "id", 64);
  const reference = requireText(record.reference, "reference");
  const amountCents = record.amountCents ?? 0;
  if (typeof amountCents !== "number" || !Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("discount.amountCents must be a non-negative integer");
  }
  return { id, reference, amountCents };
}

export function parseDiscountTransition(body: unknown): DiscountTransitionInput {
  const record = requireRecord(body, "transition");
  const status = record.status;
  if (typeof status !== "string" || !discountStatuses.includes(status as DiscountStatus)) {
    throw new ValidationError(`discount.status must be one of ${discountStatuses.join(", ")}`);
  }
  return { status: status as DiscountStatus };
}

export function parseDiscountLabel(body: unknown): DiscountLabelInput {
  const record = requireRecord(body, "label");
  return { label: requireText(record.label, "label", 40) };
}

export function parseDiscountPage(query: unknown): DiscountPageInput {
  const record = requireRecord(query ?? {}, "page");
  const offset = record.offset === undefined ? 0 : Number(record.offset);
  const limit = record.limit === undefined ? 50 : Number(record.limit);
  if (!Number.isInteger(offset) || offset < 0) {
    throw new ValidationError("discount.offset must be a non-negative integer");
  }
  if (!Number.isInteger(limit) || limit < 1 || limit > 200) {
    throw new ValidationError("discount.limit must be between 1 and 200");
  }
  return { offset, limit };
}
