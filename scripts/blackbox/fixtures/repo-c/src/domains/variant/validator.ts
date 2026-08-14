import { VariantStatus, variantStatuses } from "./model";
import { ValidationError } from "../../infra/errors";

export interface VariantCreateInput {
  id: string;
  reference: string;
  amountCents: number;
}

export interface VariantTransitionInput {
  status: VariantStatus;
}

export interface VariantLabelInput {
  label: string;
}

export interface VariantPageInput {
  offset: number;
  limit: number;
}

function requireRecord(body: unknown, what: string): Record<string, unknown> {
  if (typeof body !== "object" || body === null || Array.isArray(body)) {
    throw new ValidationError(`variant ${what} body must be an object`);
  }
  return body as Record<string, unknown>;
}

function requireText(value: unknown, field: string, max = 190): string {
  if (typeof value !== "string" || value.trim().length === 0) {
    throw new ValidationError(`variant.${field} must be a non-empty string`);
  }
  if (value.length > max) {
    throw new ValidationError(`variant.${field} must be at most ${max} characters`);
  }
  return value;
}

/** Request-shape validation for product variant writes; never trusts client types. */
export function parseVariantCreate(body: unknown): VariantCreateInput {
  const record = requireRecord(body, "create");
  const id = requireText(record.id, "id", 64);
  const reference = requireText(record.reference, "reference");
  const amountCents = record.amountCents ?? 0;
  if (typeof amountCents !== "number" || !Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("variant.amountCents must be a non-negative integer");
  }
  return { id, reference, amountCents };
}

export function parseVariantTransition(body: unknown): VariantTransitionInput {
  const record = requireRecord(body, "transition");
  const status = record.status;
  if (typeof status !== "string" || !variantStatuses.includes(status as VariantStatus)) {
    throw new ValidationError(`variant.status must be one of ${variantStatuses.join(", ")}`);
  }
  return { status: status as VariantStatus };
}

export function parseVariantLabel(body: unknown): VariantLabelInput {
  const record = requireRecord(body, "label");
  return { label: requireText(record.label, "label", 40) };
}

export function parseVariantPage(query: unknown): VariantPageInput {
  const record = requireRecord(query ?? {}, "page");
  const offset = record.offset === undefined ? 0 : Number(record.offset);
  const limit = record.limit === undefined ? 50 : Number(record.limit);
  if (!Number.isInteger(offset) || offset < 0) {
    throw new ValidationError("variant.offset must be a non-negative integer");
  }
  if (!Number.isInteger(limit) || limit < 1 || limit > 200) {
    throw new ValidationError("variant.limit must be between 1 and 200");
  }
  return { offset, limit };
}
