import { PlanStatus, planStatuses } from "./model";
import { ValidationError } from "../../infra/errors";

export interface PlanCreateInput {
  id: string;
  reference: string;
  amountCents: number;
}

export interface PlanTransitionInput {
  status: PlanStatus;
}

export interface PlanLabelInput {
  label: string;
}

export interface PlanPageInput {
  offset: number;
  limit: number;
}

function requireRecord(body: unknown, what: string): Record<string, unknown> {
  if (typeof body !== "object" || body === null || Array.isArray(body)) {
    throw new ValidationError(`plan ${what} body must be an object`);
  }
  return body as Record<string, unknown>;
}

function requireText(value: unknown, field: string, max = 190): string {
  if (typeof value !== "string" || value.trim().length === 0) {
    throw new ValidationError(`plan.${field} must be a non-empty string`);
  }
  if (value.length > max) {
    throw new ValidationError(`plan.${field} must be at most ${max} characters`);
  }
  return value;
}

/** Request-shape validation for subscription plan writes; never trusts client types. */
export function parsePlanCreate(body: unknown): PlanCreateInput {
  const record = requireRecord(body, "create");
  const id = requireText(record.id, "id", 64);
  const reference = requireText(record.reference, "reference");
  const amountCents = record.amountCents ?? 0;
  if (typeof amountCents !== "number" || !Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("plan.amountCents must be a non-negative integer");
  }
  return { id, reference, amountCents };
}

export function parsePlanTransition(body: unknown): PlanTransitionInput {
  const record = requireRecord(body, "transition");
  const status = record.status;
  if (typeof status !== "string" || !planStatuses.includes(status as PlanStatus)) {
    throw new ValidationError(`plan.status must be one of ${planStatuses.join(", ")}`);
  }
  return { status: status as PlanStatus };
}

export function parsePlanLabel(body: unknown): PlanLabelInput {
  const record = requireRecord(body, "label");
  return { label: requireText(record.label, "label", 40) };
}

export function parsePlanPage(query: unknown): PlanPageInput {
  const record = requireRecord(query ?? {}, "page");
  const offset = record.offset === undefined ? 0 : Number(record.offset);
  const limit = record.limit === undefined ? 50 : Number(record.limit);
  if (!Number.isInteger(offset) || offset < 0) {
    throw new ValidationError("plan.offset must be a non-negative integer");
  }
  if (!Number.isInteger(limit) || limit < 1 || limit > 200) {
    throw new ValidationError("plan.limit must be between 1 and 200");
  }
  return { offset, limit };
}
