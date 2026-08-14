import { WarehouseStatus, warehouseStatuses } from "./model";
import { ValidationError } from "../../infra/errors";

export interface WarehouseCreateInput {
  id: string;
  reference: string;
  amountCents: number;
}

export interface WarehouseTransitionInput {
  status: WarehouseStatus;
}

export interface WarehouseLabelInput {
  label: string;
}

export interface WarehousePageInput {
  offset: number;
  limit: number;
}

function requireRecord(body: unknown, what: string): Record<string, unknown> {
  if (typeof body !== "object" || body === null || Array.isArray(body)) {
    throw new ValidationError(`warehouse ${what} body must be an object`);
  }
  return body as Record<string, unknown>;
}

function requireText(value: unknown, field: string, max = 190): string {
  if (typeof value !== "string" || value.trim().length === 0) {
    throw new ValidationError(`warehouse.${field} must be a non-empty string`);
  }
  if (value.length > max) {
    throw new ValidationError(`warehouse.${field} must be at most ${max} characters`);
  }
  return value;
}

/** Request-shape validation for storage facility writes; never trusts client types. */
export function parseWarehouseCreate(body: unknown): WarehouseCreateInput {
  const record = requireRecord(body, "create");
  const id = requireText(record.id, "id", 64);
  const reference = requireText(record.reference, "reference");
  const amountCents = record.amountCents ?? 0;
  if (typeof amountCents !== "number" || !Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("warehouse.amountCents must be a non-negative integer");
  }
  return { id, reference, amountCents };
}

export function parseWarehouseTransition(body: unknown): WarehouseTransitionInput {
  const record = requireRecord(body, "transition");
  const status = record.status;
  if (typeof status !== "string" || !warehouseStatuses.includes(status as WarehouseStatus)) {
    throw new ValidationError(`warehouse.status must be one of ${warehouseStatuses.join(", ")}`);
  }
  return { status: status as WarehouseStatus };
}

export function parseWarehouseLabel(body: unknown): WarehouseLabelInput {
  const record = requireRecord(body, "label");
  return { label: requireText(record.label, "label", 40) };
}

export function parseWarehousePage(query: unknown): WarehousePageInput {
  const record = requireRecord(query ?? {}, "page");
  const offset = record.offset === undefined ? 0 : Number(record.offset);
  const limit = record.limit === undefined ? 50 : Number(record.limit);
  if (!Number.isInteger(offset) || offset < 0) {
    throw new ValidationError("warehouse.offset must be a non-negative integer");
  }
  if (!Number.isInteger(limit) || limit < 1 || limit > 200) {
    throw new ValidationError("warehouse.limit must be between 1 and 200");
  }
  return { offset, limit };
}
