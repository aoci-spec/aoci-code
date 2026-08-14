import { ExportRunStatus, exportStatuses } from "./model";
import { ValidationError } from "../../infra/errors";

export interface ExportRunCreateInput {
  id: string;
  reference: string;
  amountCents: number;
}

export interface ExportRunTransitionInput {
  status: ExportRunStatus;
}

export interface ExportRunLabelInput {
  label: string;
}

export interface ExportRunPageInput {
  offset: number;
  limit: number;
}

function requireRecord(body: unknown, what: string): Record<string, unknown> {
  if (typeof body !== "object" || body === null || Array.isArray(body)) {
    throw new ValidationError(`export ${what} body must be an object`);
  }
  return body as Record<string, unknown>;
}

function requireText(value: unknown, field: string, max = 190): string {
  if (typeof value !== "string" || value.trim().length === 0) {
    throw new ValidationError(`export.${field} must be a non-empty string`);
  }
  if (value.length > max) {
    throw new ValidationError(`export.${field} must be at most ${max} characters`);
  }
  return value;
}

/** Request-shape validation for bulk export run writes; never trusts client types. */
export function parseExportRunCreate(body: unknown): ExportRunCreateInput {
  const record = requireRecord(body, "create");
  const id = requireText(record.id, "id", 64);
  const reference = requireText(record.reference, "reference");
  const amountCents = record.amountCents ?? 0;
  if (typeof amountCents !== "number" || !Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("export.amountCents must be a non-negative integer");
  }
  return { id, reference, amountCents };
}

export function parseExportRunTransition(body: unknown): ExportRunTransitionInput {
  const record = requireRecord(body, "transition");
  const status = record.status;
  if (typeof status !== "string" || !exportStatuses.includes(status as ExportRunStatus)) {
    throw new ValidationError(`export.status must be one of ${exportStatuses.join(", ")}`);
  }
  return { status: status as ExportRunStatus };
}

export function parseExportRunLabel(body: unknown): ExportRunLabelInput {
  const record = requireRecord(body, "label");
  return { label: requireText(record.label, "label", 40) };
}

export function parseExportRunPage(query: unknown): ExportRunPageInput {
  const record = requireRecord(query ?? {}, "page");
  const offset = record.offset === undefined ? 0 : Number(record.offset);
  const limit = record.limit === undefined ? 50 : Number(record.limit);
  if (!Number.isInteger(offset) || offset < 0) {
    throw new ValidationError("export.offset must be a non-negative integer");
  }
  if (!Number.isInteger(limit) || limit < 1 || limit > 200) {
    throw new ValidationError("export.limit must be between 1 and 200");
  }
  return { offset, limit };
}
