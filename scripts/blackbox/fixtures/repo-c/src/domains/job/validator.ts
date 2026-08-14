import { JobStatus, jobStatuses } from "./model";
import { ValidationError } from "../../infra/errors";

export interface JobCreateInput {
  id: string;
  reference: string;
  amountCents: number;
}

export interface JobTransitionInput {
  status: JobStatus;
}

export interface JobLabelInput {
  label: string;
}

export interface JobPageInput {
  offset: number;
  limit: number;
}

function requireRecord(body: unknown, what: string): Record<string, unknown> {
  if (typeof body !== "object" || body === null || Array.isArray(body)) {
    throw new ValidationError(`job ${what} body must be an object`);
  }
  return body as Record<string, unknown>;
}

function requireText(value: unknown, field: string, max = 190): string {
  if (typeof value !== "string" || value.trim().length === 0) {
    throw new ValidationError(`job.${field} must be a non-empty string`);
  }
  if (value.length > max) {
    throw new ValidationError(`job.${field} must be at most ${max} characters`);
  }
  return value;
}

/** Request-shape validation for background job writes; never trusts client types. */
export function parseJobCreate(body: unknown): JobCreateInput {
  const record = requireRecord(body, "create");
  const id = requireText(record.id, "id", 64);
  const reference = requireText(record.reference, "reference");
  const amountCents = record.amountCents ?? 0;
  if (typeof amountCents !== "number" || !Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("job.amountCents must be a non-negative integer");
  }
  return { id, reference, amountCents };
}

export function parseJobTransition(body: unknown): JobTransitionInput {
  const record = requireRecord(body, "transition");
  const status = record.status;
  if (typeof status !== "string" || !jobStatuses.includes(status as JobStatus)) {
    throw new ValidationError(`job.status must be one of ${jobStatuses.join(", ")}`);
  }
  return { status: status as JobStatus };
}

export function parseJobLabel(body: unknown): JobLabelInput {
  const record = requireRecord(body, "label");
  return { label: requireText(record.label, "label", 40) };
}

export function parseJobPage(query: unknown): JobPageInput {
  const record = requireRecord(query ?? {}, "page");
  const offset = record.offset === undefined ? 0 : Number(record.offset);
  const limit = record.limit === undefined ? 50 : Number(record.limit);
  if (!Number.isInteger(offset) || offset < 0) {
    throw new ValidationError("job.offset must be a non-negative integer");
  }
  if (!Number.isInteger(limit) || limit < 1 || limit > 200) {
    throw new ValidationError("job.limit must be between 1 and 200");
  }
  return { offset, limit };
}
