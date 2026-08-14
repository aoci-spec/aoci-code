import { CouponStatus, couponStatuses } from "./model";
import { ValidationError } from "../../infra/errors";

export interface CouponCreateInput {
  id: string;
  reference: string;
  amountCents: number;
}

export interface CouponTransitionInput {
  status: CouponStatus;
}

export interface CouponLabelInput {
  label: string;
}

export interface CouponPageInput {
  offset: number;
  limit: number;
}

function requireRecord(body: unknown, what: string): Record<string, unknown> {
  if (typeof body !== "object" || body === null || Array.isArray(body)) {
    throw new ValidationError(`coupon ${what} body must be an object`);
  }
  return body as Record<string, unknown>;
}

function requireText(value: unknown, field: string, max = 190): string {
  if (typeof value !== "string" || value.trim().length === 0) {
    throw new ValidationError(`coupon.${field} must be a non-empty string`);
  }
  if (value.length > max) {
    throw new ValidationError(`coupon.${field} must be at most ${max} characters`);
  }
  return value;
}

/** Request-shape validation for coupon grant writes; never trusts client types. */
export function parseCouponCreate(body: unknown): CouponCreateInput {
  const record = requireRecord(body, "create");
  const id = requireText(record.id, "id", 64);
  const reference = requireText(record.reference, "reference");
  const amountCents = record.amountCents ?? 0;
  if (typeof amountCents !== "number" || !Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("coupon.amountCents must be a non-negative integer");
  }
  return { id, reference, amountCents };
}

export function parseCouponTransition(body: unknown): CouponTransitionInput {
  const record = requireRecord(body, "transition");
  const status = record.status;
  if (typeof status !== "string" || !couponStatuses.includes(status as CouponStatus)) {
    throw new ValidationError(`coupon.status must be one of ${couponStatuses.join(", ")}`);
  }
  return { status: status as CouponStatus };
}

export function parseCouponLabel(body: unknown): CouponLabelInput {
  const record = requireRecord(body, "label");
  return { label: requireText(record.label, "label", 40) };
}

export function parseCouponPage(query: unknown): CouponPageInput {
  const record = requireRecord(query ?? {}, "page");
  const offset = record.offset === undefined ? 0 : Number(record.offset);
  const limit = record.limit === undefined ? 50 : Number(record.limit);
  if (!Number.isInteger(offset) || offset < 0) {
    throw new ValidationError("coupon.offset must be a non-negative integer");
  }
  if (!Number.isInteger(limit) || limit < 1 || limit > 200) {
    throw new ValidationError("coupon.limit must be between 1 and 200");
  }
  return { offset, limit };
}
