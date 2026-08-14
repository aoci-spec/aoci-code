import { ReportStatus, reportStatuses } from "./model";
import { ValidationError } from "../../infra/errors";

export interface ReportCreateInput {
  id: string;
  reference: string;
  amountCents: number;
}

export interface ReportTransitionInput {
  status: ReportStatus;
}

export interface ReportLabelInput {
  label: string;
}

export interface ReportPageInput {
  offset: number;
  limit: number;
}

function requireRecord(body: unknown, what: string): Record<string, unknown> {
  if (typeof body !== "object" || body === null || Array.isArray(body)) {
    throw new ValidationError(`report ${what} body must be an object`);
  }
  return body as Record<string, unknown>;
}

function requireText(value: unknown, field: string, max = 190): string {
  if (typeof value !== "string" || value.trim().length === 0) {
    throw new ValidationError(`report.${field} must be a non-empty string`);
  }
  if (value.length > max) {
    throw new ValidationError(`report.${field} must be at most ${max} characters`);
  }
  return value;
}

/** Request-shape validation for generated report writes; never trusts client types. */
export function parseReportCreate(body: unknown): ReportCreateInput {
  const record = requireRecord(body, "create");
  const id = requireText(record.id, "id", 64);
  const reference = requireText(record.reference, "reference");
  const amountCents = record.amountCents ?? 0;
  if (typeof amountCents !== "number" || !Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("report.amountCents must be a non-negative integer");
  }
  return { id, reference, amountCents };
}

export function parseReportTransition(body: unknown): ReportTransitionInput {
  const record = requireRecord(body, "transition");
  const status = record.status;
  if (typeof status !== "string" || !reportStatuses.includes(status as ReportStatus)) {
    throw new ValidationError(`report.status must be one of ${reportStatuses.join(", ")}`);
  }
  return { status: status as ReportStatus };
}

export function parseReportLabel(body: unknown): ReportLabelInput {
  const record = requireRecord(body, "label");
  return { label: requireText(record.label, "label", 40) };
}

export function parseReportPage(query: unknown): ReportPageInput {
  const record = requireRecord(query ?? {}, "page");
  const offset = record.offset === undefined ? 0 : Number(record.offset);
  const limit = record.limit === undefined ? 50 : Number(record.limit);
  if (!Number.isInteger(offset) || offset < 0) {
    throw new ValidationError("report.offset must be a non-negative integer");
  }
  if (!Number.isInteger(limit) || limit < 1 || limit > 200) {
    throw new ValidationError("report.limit must be between 1 and 200");
  }
  return { offset, limit };
}
