import { ReturnCaseStatus, returnStatuses } from "./model";
import { ValidationError } from "../../infra/errors";

export interface ReturnCaseCreateInput {
  id: string;
  reference: string;
  amountCents: number;
}

export interface ReturnCaseTransitionInput {
  status: ReturnCaseStatus;
}

export interface ReturnCaseLabelInput {
  label: string;
}

export interface ReturnCasePageInput {
  offset: number;
  limit: number;
}

function requireRecord(body: unknown, what: string): Record<string, unknown> {
  if (typeof body !== "object" || body === null || Array.isArray(body)) {
    throw new ValidationError(`return ${what} body must be an object`);
  }
  return body as Record<string, unknown>;
}

function requireText(value: unknown, field: string, max = 190): string {
  if (typeof value !== "string" || value.trim().length === 0) {
    throw new ValidationError(`return.${field} must be a non-empty string`);
  }
  if (value.length > max) {
    throw new ValidationError(`return.${field} must be at most ${max} characters`);
  }
  return value;
}

/** Request-shape validation for return case writes; never trusts client types. */
export function parseReturnCaseCreate(body: unknown): ReturnCaseCreateInput {
  const record = requireRecord(body, "create");
  const id = requireText(record.id, "id", 64);
  const reference = requireText(record.reference, "reference");
  const amountCents = record.amountCents ?? 0;
  if (typeof amountCents !== "number" || !Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("return.amountCents must be a non-negative integer");
  }
  return { id, reference, amountCents };
}

export function parseReturnCaseTransition(body: unknown): ReturnCaseTransitionInput {
  const record = requireRecord(body, "transition");
  const status = record.status;
  if (typeof status !== "string" || !returnStatuses.includes(status as ReturnCaseStatus)) {
    throw new ValidationError(`return.status must be one of ${returnStatuses.join(", ")}`);
  }
  return { status: status as ReturnCaseStatus };
}

export function parseReturnCaseLabel(body: unknown): ReturnCaseLabelInput {
  const record = requireRecord(body, "label");
  return { label: requireText(record.label, "label", 40) };
}

export function parseReturnCasePage(query: unknown): ReturnCasePageInput {
  const record = requireRecord(query ?? {}, "page");
  const offset = record.offset === undefined ? 0 : Number(record.offset);
  const limit = record.limit === undefined ? 50 : Number(record.limit);
  if (!Number.isInteger(offset) || offset < 0) {
    throw new ValidationError("return.offset must be a non-negative integer");
  }
  if (!Number.isInteger(limit) || limit < 1 || limit > 200) {
    throw new ValidationError("return.limit must be between 1 and 200");
  }
  return { offset, limit };
}
