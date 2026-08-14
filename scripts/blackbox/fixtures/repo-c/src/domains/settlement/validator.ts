import { SettlementStatus, settlementStatuses } from "./model";
import { ValidationError } from "../../infra/errors";

export interface SettlementCreateInput {
  id: string;
  reference: string;
  amountCents: number;
}

export interface SettlementTransitionInput {
  status: SettlementStatus;
}

export interface SettlementLabelInput {
  label: string;
}

export interface SettlementPageInput {
  offset: number;
  limit: number;
}

function requireRecord(body: unknown, what: string): Record<string, unknown> {
  if (typeof body !== "object" || body === null || Array.isArray(body)) {
    throw new ValidationError(`settlement ${what} body must be an object`);
  }
  return body as Record<string, unknown>;
}

function requireText(value: unknown, field: string, max = 190): string {
  if (typeof value !== "string" || value.trim().length === 0) {
    throw new ValidationError(`settlement.${field} must be a non-empty string`);
  }
  if (value.length > max) {
    throw new ValidationError(`settlement.${field} must be at most ${max} characters`);
  }
  return value;
}

/** Request-shape validation for settlement run writes; never trusts client types. */
export function parseSettlementCreate(body: unknown): SettlementCreateInput {
  const record = requireRecord(body, "create");
  const id = requireText(record.id, "id", 64);
  const reference = requireText(record.reference, "reference");
  const amountCents = record.amountCents ?? 0;
  if (typeof amountCents !== "number" || !Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("settlement.amountCents must be a non-negative integer");
  }
  return { id, reference, amountCents };
}

export function parseSettlementTransition(body: unknown): SettlementTransitionInput {
  const record = requireRecord(body, "transition");
  const status = record.status;
  if (typeof status !== "string" || !settlementStatuses.includes(status as SettlementStatus)) {
    throw new ValidationError(`settlement.status must be one of ${settlementStatuses.join(", ")}`);
  }
  return { status: status as SettlementStatus };
}

export function parseSettlementLabel(body: unknown): SettlementLabelInput {
  const record = requireRecord(body, "label");
  return { label: requireText(record.label, "label", 40) };
}

export function parseSettlementPage(query: unknown): SettlementPageInput {
  const record = requireRecord(query ?? {}, "page");
  const offset = record.offset === undefined ? 0 : Number(record.offset);
  const limit = record.limit === undefined ? 50 : Number(record.limit);
  if (!Number.isInteger(offset) || offset < 0) {
    throw new ValidationError("settlement.offset must be a non-negative integer");
  }
  if (!Number.isInteger(limit) || limit < 1 || limit > 200) {
    throw new ValidationError("settlement.limit must be between 1 and 200");
  }
  return { offset, limit };
}
