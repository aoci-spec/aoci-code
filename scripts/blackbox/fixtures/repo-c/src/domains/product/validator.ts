import { ProductStatus, productStatuses } from "./model";
import { ValidationError } from "../../infra/errors";

export interface ProductCreateInput {
  id: string;
  reference: string;
  amountCents: number;
}

export interface ProductTransitionInput {
  status: ProductStatus;
}

export interface ProductLabelInput {
  label: string;
}

export interface ProductPageInput {
  offset: number;
  limit: number;
}

function requireRecord(body: unknown, what: string): Record<string, unknown> {
  if (typeof body !== "object" || body === null || Array.isArray(body)) {
    throw new ValidationError(`product ${what} body must be an object`);
  }
  return body as Record<string, unknown>;
}

function requireText(value: unknown, field: string, max = 190): string {
  if (typeof value !== "string" || value.trim().length === 0) {
    throw new ValidationError(`product.${field} must be a non-empty string`);
  }
  if (value.length > max) {
    throw new ValidationError(`product.${field} must be at most ${max} characters`);
  }
  return value;
}

/** Request-shape validation for sellable product writes; never trusts client types. */
export function parseProductCreate(body: unknown): ProductCreateInput {
  const record = requireRecord(body, "create");
  const id = requireText(record.id, "id", 64);
  const reference = requireText(record.reference, "reference");
  const amountCents = record.amountCents ?? 0;
  if (typeof amountCents !== "number" || !Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("product.amountCents must be a non-negative integer");
  }
  return { id, reference, amountCents };
}

export function parseProductTransition(body: unknown): ProductTransitionInput {
  const record = requireRecord(body, "transition");
  const status = record.status;
  if (typeof status !== "string" || !productStatuses.includes(status as ProductStatus)) {
    throw new ValidationError(`product.status must be one of ${productStatuses.join(", ")}`);
  }
  return { status: status as ProductStatus };
}

export function parseProductLabel(body: unknown): ProductLabelInput {
  const record = requireRecord(body, "label");
  return { label: requireText(record.label, "label", 40) };
}

export function parseProductPage(query: unknown): ProductPageInput {
  const record = requireRecord(query ?? {}, "page");
  const offset = record.offset === undefined ? 0 : Number(record.offset);
  const limit = record.limit === undefined ? 50 : Number(record.limit);
  if (!Number.isInteger(offset) || offset < 0) {
    throw new ValidationError("product.offset must be a non-negative integer");
  }
  if (!Number.isInteger(limit) || limit < 1 || limit > 200) {
    throw new ValidationError("product.limit must be between 1 and 200");
  }
  return { offset, limit };
}
