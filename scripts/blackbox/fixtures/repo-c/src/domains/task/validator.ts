import { TaskStatus, taskStatuses } from "./model";
import { ValidationError } from "../../infra/errors";

export interface TaskCreateInput {
  id: string;
  reference: string;
  amountCents: number;
}

export interface TaskTransitionInput {
  status: TaskStatus;
}

export interface TaskLabelInput {
  label: string;
}

export interface TaskPageInput {
  offset: number;
  limit: number;
}

function requireRecord(body: unknown, what: string): Record<string, unknown> {
  if (typeof body !== "object" || body === null || Array.isArray(body)) {
    throw new ValidationError(`task ${what} body must be an object`);
  }
  return body as Record<string, unknown>;
}

function requireText(value: unknown, field: string, max = 190): string {
  if (typeof value !== "string" || value.trim().length === 0) {
    throw new ValidationError(`task.${field} must be a non-empty string`);
  }
  if (value.length > max) {
    throw new ValidationError(`task.${field} must be at most ${max} characters`);
  }
  return value;
}

/** Request-shape validation for workflow task writes; never trusts client types. */
export function parseTaskCreate(body: unknown): TaskCreateInput {
  const record = requireRecord(body, "create");
  const id = requireText(record.id, "id", 64);
  const reference = requireText(record.reference, "reference");
  const amountCents = record.amountCents ?? 0;
  if (typeof amountCents !== "number" || !Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("task.amountCents must be a non-negative integer");
  }
  return { id, reference, amountCents };
}

export function parseTaskTransition(body: unknown): TaskTransitionInput {
  const record = requireRecord(body, "transition");
  const status = record.status;
  if (typeof status !== "string" || !taskStatuses.includes(status as TaskStatus)) {
    throw new ValidationError(`task.status must be one of ${taskStatuses.join(", ")}`);
  }
  return { status: status as TaskStatus };
}

export function parseTaskLabel(body: unknown): TaskLabelInput {
  const record = requireRecord(body, "label");
  return { label: requireText(record.label, "label", 40) };
}

export function parseTaskPage(query: unknown): TaskPageInput {
  const record = requireRecord(query ?? {}, "page");
  const offset = record.offset === undefined ? 0 : Number(record.offset);
  const limit = record.limit === undefined ? 50 : Number(record.limit);
  if (!Number.isInteger(offset) || offset < 0) {
    throw new ValidationError("task.offset must be a non-negative integer");
  }
  if (!Number.isInteger(limit) || limit < 1 || limit > 200) {
    throw new ValidationError("task.limit must be between 1 and 200");
  }
  return { offset, limit };
}
