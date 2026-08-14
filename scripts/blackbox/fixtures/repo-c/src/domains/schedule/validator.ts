import { ScheduleStatus, scheduleStatuses } from "./model";
import { ValidationError } from "../../infra/errors";

export interface ScheduleCreateInput {
  id: string;
  reference: string;
  amountCents: number;
}

export interface ScheduleTransitionInput {
  status: ScheduleStatus;
}

export interface ScheduleLabelInput {
  label: string;
}

export interface SchedulePageInput {
  offset: number;
  limit: number;
}

function requireRecord(body: unknown, what: string): Record<string, unknown> {
  if (typeof body !== "object" || body === null || Array.isArray(body)) {
    throw new ValidationError(`schedule ${what} body must be an object`);
  }
  return body as Record<string, unknown>;
}

function requireText(value: unknown, field: string, max = 190): string {
  if (typeof value !== "string" || value.trim().length === 0) {
    throw new ValidationError(`schedule.${field} must be a non-empty string`);
  }
  if (value.length > max) {
    throw new ValidationError(`schedule.${field} must be at most ${max} characters`);
  }
  return value;
}

/** Request-shape validation for scheduled run writes; never trusts client types. */
export function parseScheduleCreate(body: unknown): ScheduleCreateInput {
  const record = requireRecord(body, "create");
  const id = requireText(record.id, "id", 64);
  const reference = requireText(record.reference, "reference");
  const amountCents = record.amountCents ?? 0;
  if (typeof amountCents !== "number" || !Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("schedule.amountCents must be a non-negative integer");
  }
  return { id, reference, amountCents };
}

export function parseScheduleTransition(body: unknown): ScheduleTransitionInput {
  const record = requireRecord(body, "transition");
  const status = record.status;
  if (typeof status !== "string" || !scheduleStatuses.includes(status as ScheduleStatus)) {
    throw new ValidationError(`schedule.status must be one of ${scheduleStatuses.join(", ")}`);
  }
  return { status: status as ScheduleStatus };
}

export function parseScheduleLabel(body: unknown): ScheduleLabelInput {
  const record = requireRecord(body, "label");
  return { label: requireText(record.label, "label", 40) };
}

export function parseSchedulePage(query: unknown): SchedulePageInput {
  const record = requireRecord(query ?? {}, "page");
  const offset = record.offset === undefined ? 0 : Number(record.offset);
  const limit = record.limit === undefined ? 50 : Number(record.limit);
  if (!Number.isInteger(offset) || offset < 0) {
    throw new ValidationError("schedule.offset must be a non-negative integer");
  }
  if (!Number.isInteger(limit) || limit < 1 || limit > 200) {
    throw new ValidationError("schedule.limit must be between 1 and 200");
  }
  return { offset, limit };
}
