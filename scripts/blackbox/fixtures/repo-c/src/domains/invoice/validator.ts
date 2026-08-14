import { InvoiceStatus, invoiceStatuses } from "./model";
import { ValidationError } from "../../infra/errors";

export interface InvoiceCreateInput {
  id: string;
  reference: string;
  amountCents: number;
}

export interface InvoiceTransitionInput {
  status: InvoiceStatus;
}

export interface InvoiceLabelInput {
  label: string;
}

export interface InvoicePageInput {
  offset: number;
  limit: number;
}

function requireRecord(body: unknown, what: string): Record<string, unknown> {
  if (typeof body !== "object" || body === null || Array.isArray(body)) {
    throw new ValidationError(`invoice ${what} body must be an object`);
  }
  return body as Record<string, unknown>;
}

function requireText(value: unknown, field: string, max = 190): string {
  if (typeof value !== "string" || value.trim().length === 0) {
    throw new ValidationError(`invoice.${field} must be a non-empty string`);
  }
  if (value.length > max) {
    throw new ValidationError(`invoice.${field} must be at most ${max} characters`);
  }
  return value;
}

/** Request-shape validation for billing invoice writes; never trusts client types. */
export function parseInvoiceCreate(body: unknown): InvoiceCreateInput {
  const record = requireRecord(body, "create");
  const id = requireText(record.id, "id", 64);
  const reference = requireText(record.reference, "reference");
  const amountCents = record.amountCents ?? 0;
  if (typeof amountCents !== "number" || !Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("invoice.amountCents must be a non-negative integer");
  }
  return { id, reference, amountCents };
}

export function parseInvoiceTransition(body: unknown): InvoiceTransitionInput {
  const record = requireRecord(body, "transition");
  const status = record.status;
  if (typeof status !== "string" || !invoiceStatuses.includes(status as InvoiceStatus)) {
    throw new ValidationError(`invoice.status must be one of ${invoiceStatuses.join(", ")}`);
  }
  return { status: status as InvoiceStatus };
}

export function parseInvoiceLabel(body: unknown): InvoiceLabelInput {
  const record = requireRecord(body, "label");
  return { label: requireText(record.label, "label", 40) };
}

export function parseInvoicePage(query: unknown): InvoicePageInput {
  const record = requireRecord(query ?? {}, "page");
  const offset = record.offset === undefined ? 0 : Number(record.offset);
  const limit = record.limit === undefined ? 50 : Number(record.limit);
  if (!Number.isInteger(offset) || offset < 0) {
    throw new ValidationError("invoice.offset must be a non-negative integer");
  }
  if (!Number.isInteger(limit) || limit < 1 || limit > 200) {
    throw new ValidationError("invoice.limit must be between 1 and 200");
  }
  return { offset, limit };
}
