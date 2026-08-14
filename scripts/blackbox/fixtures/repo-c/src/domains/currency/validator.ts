import { CurrencyStatus, currencyStatuses } from "./model";
import { ValidationError } from "../../infra/errors";

export interface CurrencyCreateInput {
  id: string;
  reference: string;
  amountCents: number;
}

export interface CurrencyTransitionInput {
  status: CurrencyStatus;
}

export interface CurrencyLabelInput {
  label: string;
}

export interface CurrencyPageInput {
  offset: number;
  limit: number;
}

function requireRecord(body: unknown, what: string): Record<string, unknown> {
  if (typeof body !== "object" || body === null || Array.isArray(body)) {
    throw new ValidationError(`currency ${what} body must be an object`);
  }
  return body as Record<string, unknown>;
}

function requireText(value: unknown, field: string, max = 190): string {
  if (typeof value !== "string" || value.trim().length === 0) {
    throw new ValidationError(`currency.${field} must be a non-empty string`);
  }
  if (value.length > max) {
    throw new ValidationError(`currency.${field} must be at most ${max} characters`);
  }
  return value;
}

/** Request-shape validation for currency rate writes; never trusts client types. */
export function parseCurrencyCreate(body: unknown): CurrencyCreateInput {
  const record = requireRecord(body, "create");
  const id = requireText(record.id, "id", 64);
  const reference = requireText(record.reference, "reference");
  const amountCents = record.amountCents ?? 0;
  if (typeof amountCents !== "number" || !Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("currency.amountCents must be a non-negative integer");
  }
  return { id, reference, amountCents };
}

export function parseCurrencyTransition(body: unknown): CurrencyTransitionInput {
  const record = requireRecord(body, "transition");
  const status = record.status;
  if (typeof status !== "string" || !currencyStatuses.includes(status as CurrencyStatus)) {
    throw new ValidationError(`currency.status must be one of ${currencyStatuses.join(", ")}`);
  }
  return { status: status as CurrencyStatus };
}

export function parseCurrencyLabel(body: unknown): CurrencyLabelInput {
  const record = requireRecord(body, "label");
  return { label: requireText(record.label, "label", 40) };
}

export function parseCurrencyPage(query: unknown): CurrencyPageInput {
  const record = requireRecord(query ?? {}, "page");
  const offset = record.offset === undefined ? 0 : Number(record.offset);
  const limit = record.limit === undefined ? 50 : Number(record.limit);
  if (!Number.isInteger(offset) || offset < 0) {
    throw new ValidationError("currency.offset must be a non-negative integer");
  }
  if (!Number.isInteger(limit) || limit < 1 || limit > 200) {
    throw new ValidationError("currency.limit must be between 1 and 200");
  }
  return { offset, limit };
}
