import { QuotaStatus, quotaStatuses } from "./model";
import { ValidationError } from "../../infra/errors";

export interface QuotaCreateInput {
  id: string;
  reference: string;
  amountCents: number;
}

export interface QuotaTransitionInput {
  status: QuotaStatus;
}

export interface QuotaLabelInput {
  label: string;
}

export interface QuotaPageInput {
  offset: number;
  limit: number;
}

function requireRecord(body: unknown, what: string): Record<string, unknown> {
  if (typeof body !== "object" || body === null || Array.isArray(body)) {
    throw new ValidationError(`quota ${what} body must be an object`);
  }
  return body as Record<string, unknown>;
}

function requireText(value: unknown, field: string, max = 190): string {
  if (typeof value !== "string" || value.trim().length === 0) {
    throw new ValidationError(`quota.${field} must be a non-empty string`);
  }
  if (value.length > max) {
    throw new ValidationError(`quota.${field} must be at most ${max} characters`);
  }
  return value;
}

/** Request-shape validation for consumption quota writes; never trusts client types. */
export function parseQuotaCreate(body: unknown): QuotaCreateInput {
  const record = requireRecord(body, "create");
  const id = requireText(record.id, "id", 64);
  const reference = requireText(record.reference, "reference");
  const amountCents = record.amountCents ?? 0;
  if (typeof amountCents !== "number" || !Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("quota.amountCents must be a non-negative integer");
  }
  return { id, reference, amountCents };
}

export function parseQuotaTransition(body: unknown): QuotaTransitionInput {
  const record = requireRecord(body, "transition");
  const status = record.status;
  if (typeof status !== "string" || !quotaStatuses.includes(status as QuotaStatus)) {
    throw new ValidationError(`quota.status must be one of ${quotaStatuses.join(", ")}`);
  }
  return { status: status as QuotaStatus };
}

export function parseQuotaLabel(body: unknown): QuotaLabelInput {
  const record = requireRecord(body, "label");
  return { label: requireText(record.label, "label", 40) };
}

export function parseQuotaPage(query: unknown): QuotaPageInput {
  const record = requireRecord(query ?? {}, "page");
  const offset = record.offset === undefined ? 0 : Number(record.offset);
  const limit = record.limit === undefined ? 50 : Number(record.limit);
  if (!Number.isInteger(offset) || offset < 0) {
    throw new ValidationError("quota.offset must be a non-negative integer");
  }
  if (!Number.isInteger(limit) || limit < 1 || limit > 200) {
    throw new ValidationError("quota.limit must be between 1 and 200");
  }
  return { offset, limit };
}
