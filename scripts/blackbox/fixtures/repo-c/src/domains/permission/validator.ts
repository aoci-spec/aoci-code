import { PermissionStatus, permissionStatuses } from "./model";
import { ValidationError } from "../../infra/errors";

export interface PermissionCreateInput {
  id: string;
  reference: string;
  amountCents: number;
}

export interface PermissionTransitionInput {
  status: PermissionStatus;
}

export interface PermissionLabelInput {
  label: string;
}

export interface PermissionPageInput {
  offset: number;
  limit: number;
}

function requireRecord(body: unknown, what: string): Record<string, unknown> {
  if (typeof body !== "object" || body === null || Array.isArray(body)) {
    throw new ValidationError(`permission ${what} body must be an object`);
  }
  return body as Record<string, unknown>;
}

function requireText(value: unknown, field: string, max = 190): string {
  if (typeof value !== "string" || value.trim().length === 0) {
    throw new ValidationError(`permission.${field} must be a non-empty string`);
  }
  if (value.length > max) {
    throw new ValidationError(`permission.${field} must be at most ${max} characters`);
  }
  return value;
}

/** Request-shape validation for permission grant writes; never trusts client types. */
export function parsePermissionCreate(body: unknown): PermissionCreateInput {
  const record = requireRecord(body, "create");
  const id = requireText(record.id, "id", 64);
  const reference = requireText(record.reference, "reference");
  const amountCents = record.amountCents ?? 0;
  if (typeof amountCents !== "number" || !Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("permission.amountCents must be a non-negative integer");
  }
  return { id, reference, amountCents };
}

export function parsePermissionTransition(body: unknown): PermissionTransitionInput {
  const record = requireRecord(body, "transition");
  const status = record.status;
  if (typeof status !== "string" || !permissionStatuses.includes(status as PermissionStatus)) {
    throw new ValidationError(`permission.status must be one of ${permissionStatuses.join(", ")}`);
  }
  return { status: status as PermissionStatus };
}

export function parsePermissionLabel(body: unknown): PermissionLabelInput {
  const record = requireRecord(body, "label");
  return { label: requireText(record.label, "label", 40) };
}

export function parsePermissionPage(query: unknown): PermissionPageInput {
  const record = requireRecord(query ?? {}, "page");
  const offset = record.offset === undefined ? 0 : Number(record.offset);
  const limit = record.limit === undefined ? 50 : Number(record.limit);
  if (!Number.isInteger(offset) || offset < 0) {
    throw new ValidationError("permission.offset must be a non-negative integer");
  }
  if (!Number.isInteger(limit) || limit < 1 || limit > 200) {
    throw new ValidationError("permission.limit must be between 1 and 200");
  }
  return { offset, limit };
}
