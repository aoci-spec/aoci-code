import { UsageStatus, usageStatuses } from "./model";
import { ValidationError } from "../../infra/errors";

export interface UsageCreateInput {
  id: string;
  reference: string;
  amountCents: number;
}

export interface UsageTransitionInput {
  status: UsageStatus;
}

export interface UsageLabelInput {
  label: string;
}

export interface UsagePageInput {
  offset: number;
  limit: number;
}

function requireRecord(body: unknown, what: string): Record<string, unknown> {
  if (typeof body !== "object" || body === null || Array.isArray(body)) {
    throw new ValidationError(`usage ${what} body must be an object`);
  }
  return body as Record<string, unknown>;
}

function requireText(value: unknown, field: string, max = 190): string {
  if (typeof value !== "string" || value.trim().length === 0) {
    throw new ValidationError(`usage.${field} must be a non-empty string`);
  }
  if (value.length > max) {
    throw new ValidationError(`usage.${field} must be at most ${max} characters`);
  }
  return value;
}

/** Request-shape validation for metered usage record writes; never trusts client types. */
export function parseUsageCreate(body: unknown): UsageCreateInput {
  const record = requireRecord(body, "create");
  const id = requireText(record.id, "id", 64);
  const reference = requireText(record.reference, "reference");
  const amountCents = record.amountCents ?? 0;
  if (typeof amountCents !== "number" || !Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("usage.amountCents must be a non-negative integer");
  }
  return { id, reference, amountCents };
}

export function parseUsageTransition(body: unknown): UsageTransitionInput {
  const record = requireRecord(body, "transition");
  const status = record.status;
  if (typeof status !== "string" || !usageStatuses.includes(status as UsageStatus)) {
    throw new ValidationError(`usage.status must be one of ${usageStatuses.join(", ")}`);
  }
  return { status: status as UsageStatus };
}

export function parseUsageLabel(body: unknown): UsageLabelInput {
  const record = requireRecord(body, "label");
  return { label: requireText(record.label, "label", 40) };
}

export function parseUsagePage(query: unknown): UsagePageInput {
  const record = requireRecord(query ?? {}, "page");
  const offset = record.offset === undefined ? 0 : Number(record.offset);
  const limit = record.limit === undefined ? 50 : Number(record.limit);
  if (!Number.isInteger(offset) || offset < 0) {
    throw new ValidationError("usage.offset must be a non-negative integer");
  }
  if (!Number.isInteger(limit) || limit < 1 || limit > 200) {
    throw new ValidationError("usage.limit must be between 1 and 200");
  }
  return { offset, limit };
}
