import { PaymentStatus, paymentStatuses } from "./model";
import { ValidationError } from "../../infra/errors";

export interface PaymentCreateInput {
  id: string;
  reference: string;
  amountCents: number;
}

export interface PaymentTransitionInput {
  status: PaymentStatus;
}

export interface PaymentLabelInput {
  label: string;
}

export interface PaymentPageInput {
  offset: number;
  limit: number;
}

function requireRecord(body: unknown, what: string): Record<string, unknown> {
  if (typeof body !== "object" || body === null || Array.isArray(body)) {
    throw new ValidationError(`payment ${what} body must be an object`);
  }
  return body as Record<string, unknown>;
}

function requireText(value: unknown, field: string, max = 190): string {
  if (typeof value !== "string" || value.trim().length === 0) {
    throw new ValidationError(`payment.${field} must be a non-empty string`);
  }
  if (value.length > max) {
    throw new ValidationError(`payment.${field} must be at most ${max} characters`);
  }
  return value;
}

/** Request-shape validation for payment attempt writes; never trusts client types. */
export function parsePaymentCreate(body: unknown): PaymentCreateInput {
  const record = requireRecord(body, "create");
  const id = requireText(record.id, "id", 64);
  const reference = requireText(record.reference, "reference");
  const amountCents = record.amountCents ?? 0;
  if (typeof amountCents !== "number" || !Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("payment.amountCents must be a non-negative integer");
  }
  return { id, reference, amountCents };
}

export function parsePaymentTransition(body: unknown): PaymentTransitionInput {
  const record = requireRecord(body, "transition");
  const status = record.status;
  if (typeof status !== "string" || !paymentStatuses.includes(status as PaymentStatus)) {
    throw new ValidationError(`payment.status must be one of ${paymentStatuses.join(", ")}`);
  }
  return { status: status as PaymentStatus };
}

export function parsePaymentLabel(body: unknown): PaymentLabelInput {
  const record = requireRecord(body, "label");
  return { label: requireText(record.label, "label", 40) };
}

export function parsePaymentPage(query: unknown): PaymentPageInput {
  const record = requireRecord(query ?? {}, "page");
  const offset = record.offset === undefined ? 0 : Number(record.offset);
  const limit = record.limit === undefined ? 50 : Number(record.limit);
  if (!Number.isInteger(offset) || offset < 0) {
    throw new ValidationError("payment.offset must be a non-negative integer");
  }
  if (!Number.isInteger(limit) || limit < 1 || limit > 200) {
    throw new ValidationError("payment.limit must be between 1 and 200");
  }
  return { offset, limit };
}
