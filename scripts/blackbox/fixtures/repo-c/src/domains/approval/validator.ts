import { ApprovalStatus, approvalStatuses } from "./model";
import { ValidationError } from "../../infra/errors";

export interface ApprovalCreateInput {
  id: string;
  reference: string;
  amountCents: number;
}

export interface ApprovalTransitionInput {
  status: ApprovalStatus;
}

export interface ApprovalLabelInput {
  label: string;
}

export interface ApprovalPageInput {
  offset: number;
  limit: number;
}

function requireRecord(body: unknown, what: string): Record<string, unknown> {
  if (typeof body !== "object" || body === null || Array.isArray(body)) {
    throw new ValidationError(`approval ${what} body must be an object`);
  }
  return body as Record<string, unknown>;
}

function requireText(value: unknown, field: string, max = 190): string {
  if (typeof value !== "string" || value.trim().length === 0) {
    throw new ValidationError(`approval.${field} must be a non-empty string`);
  }
  if (value.length > max) {
    throw new ValidationError(`approval.${field} must be at most ${max} characters`);
  }
  return value;
}

/** Request-shape validation for approval decision writes; never trusts client types. */
export function parseApprovalCreate(body: unknown): ApprovalCreateInput {
  const record = requireRecord(body, "create");
  const id = requireText(record.id, "id", 64);
  const reference = requireText(record.reference, "reference");
  const amountCents = record.amountCents ?? 0;
  if (typeof amountCents !== "number" || !Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("approval.amountCents must be a non-negative integer");
  }
  return { id, reference, amountCents };
}

export function parseApprovalTransition(body: unknown): ApprovalTransitionInput {
  const record = requireRecord(body, "transition");
  const status = record.status;
  if (typeof status !== "string" || !approvalStatuses.includes(status as ApprovalStatus)) {
    throw new ValidationError(`approval.status must be one of ${approvalStatuses.join(", ")}`);
  }
  return { status: status as ApprovalStatus };
}

export function parseApprovalLabel(body: unknown): ApprovalLabelInput {
  const record = requireRecord(body, "label");
  return { label: requireText(record.label, "label", 40) };
}

export function parseApprovalPage(query: unknown): ApprovalPageInput {
  const record = requireRecord(query ?? {}, "page");
  const offset = record.offset === undefined ? 0 : Number(record.offset);
  const limit = record.limit === undefined ? 50 : Number(record.limit);
  if (!Number.isInteger(offset) || offset < 0) {
    throw new ValidationError("approval.offset must be a non-negative integer");
  }
  if (!Number.isInteger(limit) || limit < 1 || limit > 200) {
    throw new ValidationError("approval.limit must be between 1 and 200");
  }
  return { offset, limit };
}
