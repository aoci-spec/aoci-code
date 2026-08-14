import { RoleStatus, roleStatuses } from "./model";
import { ValidationError } from "../../infra/errors";

export interface RoleCreateInput {
  id: string;
  reference: string;
  amountCents: number;
}

export interface RoleTransitionInput {
  status: RoleStatus;
}

export interface RoleLabelInput {
  label: string;
}

export interface RolePageInput {
  offset: number;
  limit: number;
}

function requireRecord(body: unknown, what: string): Record<string, unknown> {
  if (typeof body !== "object" || body === null || Array.isArray(body)) {
    throw new ValidationError(`role ${what} body must be an object`);
  }
  return body as Record<string, unknown>;
}

function requireText(value: unknown, field: string, max = 190): string {
  if (typeof value !== "string" || value.trim().length === 0) {
    throw new ValidationError(`role.${field} must be a non-empty string`);
  }
  if (value.length > max) {
    throw new ValidationError(`role.${field} must be at most ${max} characters`);
  }
  return value;
}

/** Request-shape validation for authorization role writes; never trusts client types. */
export function parseRoleCreate(body: unknown): RoleCreateInput {
  const record = requireRecord(body, "create");
  const id = requireText(record.id, "id", 64);
  const reference = requireText(record.reference, "reference");
  const amountCents = record.amountCents ?? 0;
  if (typeof amountCents !== "number" || !Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("role.amountCents must be a non-negative integer");
  }
  return { id, reference, amountCents };
}

export function parseRoleTransition(body: unknown): RoleTransitionInput {
  const record = requireRecord(body, "transition");
  const status = record.status;
  if (typeof status !== "string" || !roleStatuses.includes(status as RoleStatus)) {
    throw new ValidationError(`role.status must be one of ${roleStatuses.join(", ")}`);
  }
  return { status: status as RoleStatus };
}

export function parseRoleLabel(body: unknown): RoleLabelInput {
  const record = requireRecord(body, "label");
  return { label: requireText(record.label, "label", 40) };
}

export function parseRolePage(query: unknown): RolePageInput {
  const record = requireRecord(query ?? {}, "page");
  const offset = record.offset === undefined ? 0 : Number(record.offset);
  const limit = record.limit === undefined ? 50 : Number(record.limit);
  if (!Number.isInteger(offset) || offset < 0) {
    throw new ValidationError("role.offset must be a non-negative integer");
  }
  if (!Number.isInteger(limit) || limit < 1 || limit > 200) {
    throw new ValidationError("role.limit must be between 1 and 200");
  }
  return { offset, limit };
}
