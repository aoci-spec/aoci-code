import { LedgerStatus, ledgerStatuses } from "./model";
import { ValidationError } from "../../infra/errors";

export interface LedgerCreateInput {
  id: string;
  reference: string;
  amountCents: number;
}

export interface LedgerTransitionInput {
  status: LedgerStatus;
}

export interface LedgerLabelInput {
  label: string;
}

export interface LedgerPageInput {
  offset: number;
  limit: number;
}

function requireRecord(body: unknown, what: string): Record<string, unknown> {
  if (typeof body !== "object" || body === null || Array.isArray(body)) {
    throw new ValidationError(`ledger ${what} body must be an object`);
  }
  return body as Record<string, unknown>;
}

function requireText(value: unknown, field: string, max = 190): string {
  if (typeof value !== "string" || value.trim().length === 0) {
    throw new ValidationError(`ledger.${field} must be a non-empty string`);
  }
  if (value.length > max) {
    throw new ValidationError(`ledger.${field} must be at most ${max} characters`);
  }
  return value;
}

/** Request-shape validation for accounting ledger entry writes; never trusts client types. */
export function parseLedgerCreate(body: unknown): LedgerCreateInput {
  const record = requireRecord(body, "create");
  const id = requireText(record.id, "id", 64);
  const reference = requireText(record.reference, "reference");
  const amountCents = record.amountCents ?? 0;
  if (typeof amountCents !== "number" || !Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("ledger.amountCents must be a non-negative integer");
  }
  return { id, reference, amountCents };
}

export function parseLedgerTransition(body: unknown): LedgerTransitionInput {
  const record = requireRecord(body, "transition");
  const status = record.status;
  if (typeof status !== "string" || !ledgerStatuses.includes(status as LedgerStatus)) {
    throw new ValidationError(`ledger.status must be one of ${ledgerStatuses.join(", ")}`);
  }
  return { status: status as LedgerStatus };
}

export function parseLedgerLabel(body: unknown): LedgerLabelInput {
  const record = requireRecord(body, "label");
  return { label: requireText(record.label, "label", 40) };
}

export function parseLedgerPage(query: unknown): LedgerPageInput {
  const record = requireRecord(query ?? {}, "page");
  const offset = record.offset === undefined ? 0 : Number(record.offset);
  const limit = record.limit === undefined ? 50 : Number(record.limit);
  if (!Number.isInteger(offset) || offset < 0) {
    throw new ValidationError("ledger.offset must be a non-negative integer");
  }
  if (!Number.isInteger(limit) || limit < 1 || limit > 200) {
    throw new ValidationError("ledger.limit must be between 1 and 200");
  }
  return { offset, limit };
}
