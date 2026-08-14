import { Warehouse, WarehouseStatus, summarizeWarehouse } from "./model";
import { WarehousePage } from "./repository";
import { WarehouseSummary } from "./service";

export interface WarehousePayload {
  id: string;
  status: string;
  amount: string;
  reference: string;
  labels: readonly string[];
  summary: string;
  updatedAt: string;
}

export interface WarehousePagePayload {
  items: readonly WarehousePayload[];
  total: number;
  offset: number;
}

export interface WarehouseSummaryPayload {
  total: number;
  outstanding: number;
  amount: string;
  byStatus: Record<WarehouseStatus, number>;
}

function money(amountCents: number): string {
  return (amountCents / 100).toFixed(2);
}

/** Wire representation of a storage facility; tenant identity never leaves the service. */
export function toWarehousePayload(value: Warehouse): WarehousePayload {
  return {
    id: value.id,
    status: value.status,
    amount: money(value.amountCents),
    reference: value.reference,
    labels: value.labels,
    summary: summarizeWarehouse(value),
    updatedAt: value.updatedAt,
  };
}

export function toWarehousePayloads(values: readonly Warehouse[]): WarehousePayload[] {
  return values.map(toWarehousePayload);
}

export function toWarehousePagePayload(page: WarehousePage): WarehousePagePayload {
  return { items: toWarehousePayloads(page.items), total: page.total, offset: page.offset };
}

export function toWarehouseSummaryPayload(summary: WarehouseSummary): WarehouseSummaryPayload {
  return {
    total: summary.total,
    outstanding: summary.outstanding,
    amount: money(summary.amountCents),
    byStatus: summary.byStatus,
  };
}

export function totalAmountCents(values: readonly Warehouse[]): number {
  return values.reduce((sum, value) => sum + value.amountCents, 0);
}

export function toWarehouseCsvRow(value: Warehouse): string {
  return [value.id, value.status, money(value.amountCents), value.reference].join(",");
}
