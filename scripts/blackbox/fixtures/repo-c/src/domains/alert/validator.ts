import { AlertStatus, alertStatuses } from "./model";
import { ValidationError } from "../../infra/errors";

export interface AlertCreateInput {
  id: string;
  reference: string;
  amountCents: number;
}

export interface AlertTransitionInput {
  status: AlertStatus;
}

export interface AlertLabelInput {
  label: string;
}

export interface AlertPageInput {
  offset: number;
  limit: number;
}

function requireRecord(body: unknown, what: string): Record<string, unknown> {
  if (typeof body !== "object" || body === null || Array.isArray(body)) {
    throw new ValidationError(`alert ${what} body must be an object`);
  }
  return body as Record<string, unknown>;
}

function requireText(value: unknown, field: string, max = 190): string {
  if (typeof value !== "string" || value.trim().length === 0) {
    throw new ValidationError(`alert.${field} must be a non-empty string`);
  }
  if (value.length > max) {
    throw new ValidationError(`alert.${field} must be at most ${max} characters`);
  }
  return value;
}

/** Request-shape validation for operational alert writes; never trusts client types. */
export function parseAlertCreate(body: unknown): AlertCreateInput {
  const record = requireRecord(body, "create");
  const id = requireText(record.id, "id", 64);
  const reference = requireText(record.reference, "reference");
  const amountCents = record.amountCents ?? 0;
  if (typeof amountCents !== "number" || !Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("alert.amountCents must be a non-negative integer");
  }
  return { id, reference, amountCents };
}

export function parseAlertTransition(body: unknown): AlertTransitionInput {
  const record = requireRecord(body, "transition");
  const status = record.status;
  if (typeof status !== "string" || !alertStatuses.includes(status as AlertStatus)) {
    throw new ValidationError(`alert.status must be one of ${alertStatuses.join(", ")}`);
  }
  return { status: status as AlertStatus };
}

export function parseAlertLabel(body: unknown): AlertLabelInput {
  const record = requireRecord(body, "label");
  return { label: requireText(record.label, "label", 40) };
}

export function parseAlertPage(query: unknown): AlertPageInput {
  const record = requireRecord(query ?? {}, "page");
  const offset = record.offset === undefined ? 0 : Number(record.offset);
  const limit = record.limit === undefined ? 50 : Number(record.limit);
  if (!Number.isInteger(offset) || offset < 0) {
    throw new ValidationError("alert.offset must be a non-negative integer");
  }
  if (!Number.isInteger(limit) || limit < 1 || limit > 200) {
    throw new ValidationError("alert.limit must be between 1 and 200");
  }
  return { offset, limit };
}
