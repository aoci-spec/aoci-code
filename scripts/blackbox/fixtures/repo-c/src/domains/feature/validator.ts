import { FeatureStatus, featureStatuses } from "./model";
import { ValidationError } from "../../infra/errors";

export interface FeatureCreateInput {
  id: string;
  reference: string;
  amountCents: number;
}

export interface FeatureTransitionInput {
  status: FeatureStatus;
}

export interface FeatureLabelInput {
  label: string;
}

export interface FeaturePageInput {
  offset: number;
  limit: number;
}

function requireRecord(body: unknown, what: string): Record<string, unknown> {
  if (typeof body !== "object" || body === null || Array.isArray(body)) {
    throw new ValidationError(`feature ${what} body must be an object`);
  }
  return body as Record<string, unknown>;
}

function requireText(value: unknown, field: string, max = 190): string {
  if (typeof value !== "string" || value.trim().length === 0) {
    throw new ValidationError(`feature.${field} must be a non-empty string`);
  }
  if (value.length > max) {
    throw new ValidationError(`feature.${field} must be at most ${max} characters`);
  }
  return value;
}

/** Request-shape validation for feature flag writes; never trusts client types. */
export function parseFeatureCreate(body: unknown): FeatureCreateInput {
  const record = requireRecord(body, "create");
  const id = requireText(record.id, "id", 64);
  const reference = requireText(record.reference, "reference");
  const amountCents = record.amountCents ?? 0;
  if (typeof amountCents !== "number" || !Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("feature.amountCents must be a non-negative integer");
  }
  return { id, reference, amountCents };
}

export function parseFeatureTransition(body: unknown): FeatureTransitionInput {
  const record = requireRecord(body, "transition");
  const status = record.status;
  if (typeof status !== "string" || !featureStatuses.includes(status as FeatureStatus)) {
    throw new ValidationError(`feature.status must be one of ${featureStatuses.join(", ")}`);
  }
  return { status: status as FeatureStatus };
}

export function parseFeatureLabel(body: unknown): FeatureLabelInput {
  const record = requireRecord(body, "label");
  return { label: requireText(record.label, "label", 40) };
}

export function parseFeaturePage(query: unknown): FeaturePageInput {
  const record = requireRecord(query ?? {}, "page");
  const offset = record.offset === undefined ? 0 : Number(record.offset);
  const limit = record.limit === undefined ? 50 : Number(record.limit);
  if (!Number.isInteger(offset) || offset < 0) {
    throw new ValidationError("feature.offset must be a non-negative integer");
  }
  if (!Number.isInteger(limit) || limit < 1 || limit > 200) {
    throw new ValidationError("feature.limit must be between 1 and 200");
  }
  return { offset, limit };
}
