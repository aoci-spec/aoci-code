import { MetricStatus, metricStatuses } from "./model";
import { ValidationError } from "../../infra/errors";

export interface MetricCreateInput {
  id: string;
  reference: string;
  amountCents: number;
}

export interface MetricTransitionInput {
  status: MetricStatus;
}

export interface MetricLabelInput {
  label: string;
}

export interface MetricPageInput {
  offset: number;
  limit: number;
}

function requireRecord(body: unknown, what: string): Record<string, unknown> {
  if (typeof body !== "object" || body === null || Array.isArray(body)) {
    throw new ValidationError(`metric ${what} body must be an object`);
  }
  return body as Record<string, unknown>;
}

function requireText(value: unknown, field: string, max = 190): string {
  if (typeof value !== "string" || value.trim().length === 0) {
    throw new ValidationError(`metric.${field} must be a non-empty string`);
  }
  if (value.length > max) {
    throw new ValidationError(`metric.${field} must be at most ${max} characters`);
  }
  return value;
}

/** Request-shape validation for aggregated metric writes; never trusts client types. */
export function parseMetricCreate(body: unknown): MetricCreateInput {
  const record = requireRecord(body, "create");
  const id = requireText(record.id, "id", 64);
  const reference = requireText(record.reference, "reference");
  const amountCents = record.amountCents ?? 0;
  if (typeof amountCents !== "number" || !Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("metric.amountCents must be a non-negative integer");
  }
  return { id, reference, amountCents };
}

export function parseMetricTransition(body: unknown): MetricTransitionInput {
  const record = requireRecord(body, "transition");
  const status = record.status;
  if (typeof status !== "string" || !metricStatuses.includes(status as MetricStatus)) {
    throw new ValidationError(`metric.status must be one of ${metricStatuses.join(", ")}`);
  }
  return { status: status as MetricStatus };
}

export function parseMetricLabel(body: unknown): MetricLabelInput {
  const record = requireRecord(body, "label");
  return { label: requireText(record.label, "label", 40) };
}

export function parseMetricPage(query: unknown): MetricPageInput {
  const record = requireRecord(query ?? {}, "page");
  const offset = record.offset === undefined ? 0 : Number(record.offset);
  const limit = record.limit === undefined ? 50 : Number(record.limit);
  if (!Number.isInteger(offset) || offset < 0) {
    throw new ValidationError("metric.offset must be a non-negative integer");
  }
  if (!Number.isInteger(limit) || limit < 1 || limit > 200) {
    throw new ValidationError("metric.limit must be between 1 and 200");
  }
  return { offset, limit };
}
