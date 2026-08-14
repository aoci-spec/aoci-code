import { AuditStatus, auditStatuses } from "./model";
import { ValidationError } from "../../infra/errors";

export interface AuditCreateInput {
  id: string;
  reference: string;
  amountCents: number;
}

export interface AuditTransitionInput {
  status: AuditStatus;
}

export interface AuditLabelInput {
  label: string;
}

export interface AuditPageInput {
  offset: number;
  limit: number;
}

function requireRecord(body: unknown, what: string): Record<string, unknown> {
  if (typeof body !== "object" || body === null || Array.isArray(body)) {
    throw new ValidationError(`audit ${what} body must be an object`);
  }
  return body as Record<string, unknown>;
}

function requireText(value: unknown, field: string, max = 190): string {
  if (typeof value !== "string" || value.trim().length === 0) {
    throw new ValidationError(`audit.${field} must be a non-empty string`);
  }
  if (value.length > max) {
    throw new ValidationError(`audit.${field} must be at most ${max} characters`);
  }
  return value;
}

/** Request-shape validation for audit trail record writes; never trusts client types. */
export function parseAuditCreate(body: unknown): AuditCreateInput {
  const record = requireRecord(body, "create");
  const id = requireText(record.id, "id", 64);
  const reference = requireText(record.reference, "reference");
  const amountCents = record.amountCents ?? 0;
  if (typeof amountCents !== "number" || !Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("audit.amountCents must be a non-negative integer");
  }
  return { id, reference, amountCents };
}

export function parseAuditTransition(body: unknown): AuditTransitionInput {
  const record = requireRecord(body, "transition");
  const status = record.status;
  if (typeof status !== "string" || !auditStatuses.includes(status as AuditStatus)) {
    throw new ValidationError(`audit.status must be one of ${auditStatuses.join(", ")}`);
  }
  return { status: status as AuditStatus };
}

export function parseAuditLabel(body: unknown): AuditLabelInput {
  const record = requireRecord(body, "label");
  return { label: requireText(record.label, "label", 40) };
}

export function parseAuditPage(query: unknown): AuditPageInput {
  const record = requireRecord(query ?? {}, "page");
  const offset = record.offset === undefined ? 0 : Number(record.offset);
  const limit = record.limit === undefined ? 50 : Number(record.limit);
  if (!Number.isInteger(offset) || offset < 0) {
    throw new ValidationError("audit.offset must be a non-negative integer");
  }
  if (!Number.isInteger(limit) || limit < 1 || limit > 200) {
    throw new ValidationError("audit.limit must be between 1 and 200");
  }
  return { offset, limit };
}
