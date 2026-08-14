import { SettingStatus, settingStatuses } from "./model";
import { ValidationError } from "../../infra/errors";

export interface SettingCreateInput {
  id: string;
  reference: string;
  amountCents: number;
}

export interface SettingTransitionInput {
  status: SettingStatus;
}

export interface SettingLabelInput {
  label: string;
}

export interface SettingPageInput {
  offset: number;
  limit: number;
}

function requireRecord(body: unknown, what: string): Record<string, unknown> {
  if (typeof body !== "object" || body === null || Array.isArray(body)) {
    throw new ValidationError(`setting ${what} body must be an object`);
  }
  return body as Record<string, unknown>;
}

function requireText(value: unknown, field: string, max = 190): string {
  if (typeof value !== "string" || value.trim().length === 0) {
    throw new ValidationError(`setting.${field} must be a non-empty string`);
  }
  if (value.length > max) {
    throw new ValidationError(`setting.${field} must be at most ${max} characters`);
  }
  return value;
}

/** Request-shape validation for tenant setting writes; never trusts client types. */
export function parseSettingCreate(body: unknown): SettingCreateInput {
  const record = requireRecord(body, "create");
  const id = requireText(record.id, "id", 64);
  const reference = requireText(record.reference, "reference");
  const amountCents = record.amountCents ?? 0;
  if (typeof amountCents !== "number" || !Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("setting.amountCents must be a non-negative integer");
  }
  return { id, reference, amountCents };
}

export function parseSettingTransition(body: unknown): SettingTransitionInput {
  const record = requireRecord(body, "transition");
  const status = record.status;
  if (typeof status !== "string" || !settingStatuses.includes(status as SettingStatus)) {
    throw new ValidationError(`setting.status must be one of ${settingStatuses.join(", ")}`);
  }
  return { status: status as SettingStatus };
}

export function parseSettingLabel(body: unknown): SettingLabelInput {
  const record = requireRecord(body, "label");
  return { label: requireText(record.label, "label", 40) };
}

export function parseSettingPage(query: unknown): SettingPageInput {
  const record = requireRecord(query ?? {}, "page");
  const offset = record.offset === undefined ? 0 : Number(record.offset);
  const limit = record.limit === undefined ? 50 : Number(record.limit);
  if (!Number.isInteger(offset) || offset < 0) {
    throw new ValidationError("setting.offset must be a non-negative integer");
  }
  if (!Number.isInteger(limit) || limit < 1 || limit > 200) {
    throw new ValidationError("setting.limit must be between 1 and 200");
  }
  return { offset, limit };
}
