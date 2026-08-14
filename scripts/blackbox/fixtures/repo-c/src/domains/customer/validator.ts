import { CustomerStatus, customerStatuses } from "./model";
import { ValidationError } from "../../infra/errors";

export interface CustomerCreateInput {
  id: string;
  reference: string;
  amountCents: number;
}

export interface CustomerTransitionInput {
  status: CustomerStatus;
}

export interface CustomerLabelInput {
  label: string;
}

export interface CustomerPageInput {
  offset: number;
  limit: number;
}

function requireRecord(body: unknown, what: string): Record<string, unknown> {
  if (typeof body !== "object" || body === null || Array.isArray(body)) {
    throw new ValidationError(`customer ${what} body must be an object`);
  }
  return body as Record<string, unknown>;
}

function requireText(value: unknown, field: string, max = 190): string {
  if (typeof value !== "string" || value.trim().length === 0) {
    throw new ValidationError(`customer.${field} must be a non-empty string`);
  }
  if (value.length > max) {
    throw new ValidationError(`customer.${field} must be at most ${max} characters`);
  }
  return value;
}

/** Request-shape validation for customer account writes; never trusts client types. */
export function parseCustomerCreate(body: unknown): CustomerCreateInput {
  const record = requireRecord(body, "create");
  const id = requireText(record.id, "id", 64);
  const reference = requireText(record.reference, "reference");
  const amountCents = record.amountCents ?? 0;
  if (typeof amountCents !== "number" || !Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("customer.amountCents must be a non-negative integer");
  }
  return { id, reference, amountCents };
}

export function parseCustomerTransition(body: unknown): CustomerTransitionInput {
  const record = requireRecord(body, "transition");
  const status = record.status;
  if (typeof status !== "string" || !customerStatuses.includes(status as CustomerStatus)) {
    throw new ValidationError(`customer.status must be one of ${customerStatuses.join(", ")}`);
  }
  return { status: status as CustomerStatus };
}

export function parseCustomerLabel(body: unknown): CustomerLabelInput {
  const record = requireRecord(body, "label");
  return { label: requireText(record.label, "label", 40) };
}

export function parseCustomerPage(query: unknown): CustomerPageInput {
  const record = requireRecord(query ?? {}, "page");
  const offset = record.offset === undefined ? 0 : Number(record.offset);
  const limit = record.limit === undefined ? 50 : Number(record.limit);
  if (!Number.isInteger(offset) || offset < 0) {
    throw new ValidationError("customer.offset must be a non-negative integer");
  }
  if (!Number.isInteger(limit) || limit < 1 || limit > 200) {
    throw new ValidationError("customer.limit must be between 1 and 200");
  }
  return { offset, limit };
}
