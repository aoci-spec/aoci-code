import { OrderStatus, orderStatuses } from "./model";
import { ValidationError } from "../../infra/errors";

export interface OrderCreateInput {
  id: string;
  reference: string;
  amountCents: number;
}

export interface OrderTransitionInput {
  status: OrderStatus;
}

export interface OrderLabelInput {
  label: string;
}

export interface OrderPageInput {
  offset: number;
  limit: number;
}

function requireRecord(body: unknown, what: string): Record<string, unknown> {
  if (typeof body !== "object" || body === null || Array.isArray(body)) {
    throw new ValidationError(`order ${what} body must be an object`);
  }
  return body as Record<string, unknown>;
}

function requireText(value: unknown, field: string, max = 190): string {
  if (typeof value !== "string" || value.trim().length === 0) {
    throw new ValidationError(`order.${field} must be a non-empty string`);
  }
  if (value.length > max) {
    throw new ValidationError(`order.${field} must be at most ${max} characters`);
  }
  return value;
}

/** Request-shape validation for purchase order writes; never trusts client types. */
export function parseOrderCreate(body: unknown): OrderCreateInput {
  const record = requireRecord(body, "create");
  const id = requireText(record.id, "id", 64);
  const reference = requireText(record.reference, "reference");
  const amountCents = record.amountCents ?? 0;
  if (typeof amountCents !== "number" || !Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("order.amountCents must be a non-negative integer");
  }
  return { id, reference, amountCents };
}

export function parseOrderTransition(body: unknown): OrderTransitionInput {
  const record = requireRecord(body, "transition");
  const status = record.status;
  if (typeof status !== "string" || !orderStatuses.includes(status as OrderStatus)) {
    throw new ValidationError(`order.status must be one of ${orderStatuses.join(", ")}`);
  }
  return { status: status as OrderStatus };
}

export function parseOrderLabel(body: unknown): OrderLabelInput {
  const record = requireRecord(body, "label");
  return { label: requireText(record.label, "label", 40) };
}

export function parseOrderPage(query: unknown): OrderPageInput {
  const record = requireRecord(query ?? {}, "page");
  const offset = record.offset === undefined ? 0 : Number(record.offset);
  const limit = record.limit === undefined ? 50 : Number(record.limit);
  if (!Number.isInteger(offset) || offset < 0) {
    throw new ValidationError("order.offset must be a non-negative integer");
  }
  if (!Number.isInteger(limit) || limit < 1 || limit > 200) {
    throw new ValidationError("order.limit must be between 1 and 200");
  }
  return { offset, limit };
}
