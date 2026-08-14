import { TenantStatus, tenantStatuses } from "./model";
import { ValidationError } from "../../infra/errors";

export interface TenantCreateInput {
  id: string;
  reference: string;
  amountCents: number;
}

export interface TenantTransitionInput {
  status: TenantStatus;
}

export interface TenantLabelInput {
  label: string;
}

export interface TenantPageInput {
  offset: number;
  limit: number;
}

function requireRecord(body: unknown, what: string): Record<string, unknown> {
  if (typeof body !== "object" || body === null || Array.isArray(body)) {
    throw new ValidationError(`tenant ${what} body must be an object`);
  }
  return body as Record<string, unknown>;
}

function requireText(value: unknown, field: string, max = 190): string {
  if (typeof value !== "string" || value.trim().length === 0) {
    throw new ValidationError(`tenant.${field} must be a non-empty string`);
  }
  if (value.length > max) {
    throw new ValidationError(`tenant.${field} must be at most ${max} characters`);
  }
  return value;
}

/** Request-shape validation for tenant boundary writes; never trusts client types. */
export function parseTenantCreate(body: unknown): TenantCreateInput {
  const record = requireRecord(body, "create");
  const id = requireText(record.id, "id", 64);
  const reference = requireText(record.reference, "reference");
  const amountCents = record.amountCents ?? 0;
  if (typeof amountCents !== "number" || !Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("tenant.amountCents must be a non-negative integer");
  }
  return { id, reference, amountCents };
}

export function parseTenantTransition(body: unknown): TenantTransitionInput {
  const record = requireRecord(body, "transition");
  const status = record.status;
  if (typeof status !== "string" || !tenantStatuses.includes(status as TenantStatus)) {
    throw new ValidationError(`tenant.status must be one of ${tenantStatuses.join(", ")}`);
  }
  return { status: status as TenantStatus };
}

export function parseTenantLabel(body: unknown): TenantLabelInput {
  const record = requireRecord(body, "label");
  return { label: requireText(record.label, "label", 40) };
}

export function parseTenantPage(query: unknown): TenantPageInput {
  const record = requireRecord(query ?? {}, "page");
  const offset = record.offset === undefined ? 0 : Number(record.offset);
  const limit = record.limit === undefined ? 50 : Number(record.limit);
  if (!Number.isInteger(offset) || offset < 0) {
    throw new ValidationError("tenant.offset must be a non-negative integer");
  }
  if (!Number.isInteger(limit) || limit < 1 || limit > 200) {
    throw new ValidationError("tenant.limit must be between 1 and 200");
  }
  return { offset, limit };
}
