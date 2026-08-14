import { PriceStatus, priceStatuses } from "./model";
import { ValidationError } from "../../infra/errors";

export interface PriceCreateInput {
  id: string;
  reference: string;
  amountCents: number;
}

export interface PriceTransitionInput {
  status: PriceStatus;
}

export interface PriceLabelInput {
  label: string;
}

export interface PricePageInput {
  offset: number;
  limit: number;
}

function requireRecord(body: unknown, what: string): Record<string, unknown> {
  if (typeof body !== "object" || body === null || Array.isArray(body)) {
    throw new ValidationError(`price ${what} body must be an object`);
  }
  return body as Record<string, unknown>;
}

function requireText(value: unknown, field: string, max = 190): string {
  if (typeof value !== "string" || value.trim().length === 0) {
    throw new ValidationError(`price.${field} must be a non-empty string`);
  }
  if (value.length > max) {
    throw new ValidationError(`price.${field} must be at most ${max} characters`);
  }
  return value;
}

/** Request-shape validation for price definition writes; never trusts client types. */
export function parsePriceCreate(body: unknown): PriceCreateInput {
  const record = requireRecord(body, "create");
  const id = requireText(record.id, "id", 64);
  const reference = requireText(record.reference, "reference");
  const amountCents = record.amountCents ?? 0;
  if (typeof amountCents !== "number" || !Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("price.amountCents must be a non-negative integer");
  }
  return { id, reference, amountCents };
}

export function parsePriceTransition(body: unknown): PriceTransitionInput {
  const record = requireRecord(body, "transition");
  const status = record.status;
  if (typeof status !== "string" || !priceStatuses.includes(status as PriceStatus)) {
    throw new ValidationError(`price.status must be one of ${priceStatuses.join(", ")}`);
  }
  return { status: status as PriceStatus };
}

export function parsePriceLabel(body: unknown): PriceLabelInput {
  const record = requireRecord(body, "label");
  return { label: requireText(record.label, "label", 40) };
}

export function parsePricePage(query: unknown): PricePageInput {
  const record = requireRecord(query ?? {}, "page");
  const offset = record.offset === undefined ? 0 : Number(record.offset);
  const limit = record.limit === undefined ? 50 : Number(record.limit);
  if (!Number.isInteger(offset) || offset < 0) {
    throw new ValidationError("price.offset must be a non-negative integer");
  }
  if (!Number.isInteger(limit) || limit < 1 || limit > 200) {
    throw new ValidationError("price.limit must be between 1 and 200");
  }
  return { offset, limit };
}
