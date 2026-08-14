import { DisputeStatus, disputeStatuses } from "./model";
import { ValidationError } from "../../infra/errors";

export interface DisputeCreateInput {
  id: string;
  reference: string;
  amountCents: number;
}

export interface DisputeTransitionInput {
  status: DisputeStatus;
}

export interface DisputeLabelInput {
  label: string;
}

export interface DisputePageInput {
  offset: number;
  limit: number;
}

function requireRecord(body: unknown, what: string): Record<string, unknown> {
  if (typeof body !== "object" || body === null || Array.isArray(body)) {
    throw new ValidationError(`dispute ${what} body must be an object`);
  }
  return body as Record<string, unknown>;
}

function requireText(value: unknown, field: string, max = 190): string {
  if (typeof value !== "string" || value.trim().length === 0) {
    throw new ValidationError(`dispute.${field} must be a non-empty string`);
  }
  if (value.length > max) {
    throw new ValidationError(`dispute.${field} must be at most ${max} characters`);
  }
  return value;
}

/** Request-shape validation for payment dispute writes; never trusts client types. */
export function parseDisputeCreate(body: unknown): DisputeCreateInput {
  const record = requireRecord(body, "create");
  const id = requireText(record.id, "id", 64);
  const reference = requireText(record.reference, "reference");
  const amountCents = record.amountCents ?? 0;
  if (typeof amountCents !== "number" || !Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("dispute.amountCents must be a non-negative integer");
  }
  return { id, reference, amountCents };
}

export function parseDisputeTransition(body: unknown): DisputeTransitionInput {
  const record = requireRecord(body, "transition");
  const status = record.status;
  if (typeof status !== "string" || !disputeStatuses.includes(status as DisputeStatus)) {
    throw new ValidationError(`dispute.status must be one of ${disputeStatuses.join(", ")}`);
  }
  return { status: status as DisputeStatus };
}

export function parseDisputeLabel(body: unknown): DisputeLabelInput {
  const record = requireRecord(body, "label");
  return { label: requireText(record.label, "label", 40) };
}

export function parseDisputePage(query: unknown): DisputePageInput {
  const record = requireRecord(query ?? {}, "page");
  const offset = record.offset === undefined ? 0 : Number(record.offset);
  const limit = record.limit === undefined ? 50 : Number(record.limit);
  if (!Number.isInteger(offset) || offset < 0) {
    throw new ValidationError("dispute.offset must be a non-negative integer");
  }
  if (!Number.isInteger(limit) || limit < 1 || limit > 200) {
    throw new ValidationError("dispute.limit must be between 1 and 200");
  }
  return { offset, limit };
}
