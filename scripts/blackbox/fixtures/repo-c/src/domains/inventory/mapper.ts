import { Inventory, InventoryStatus, summarizeInventory } from "./model";
import { InventoryPage } from "./repository";
import { InventorySummary } from "./service";

export interface InventoryPayload {
  id: string;
  status: string;
  amount: string;
  reference: string;
  labels: readonly string[];
  summary: string;
  updatedAt: string;
}

export interface InventoryPagePayload {
  items: readonly InventoryPayload[];
  total: number;
  offset: number;
}

export interface InventorySummaryPayload {
  total: number;
  outstanding: number;
  amount: string;
  byStatus: Record<InventoryStatus, number>;
}

function money(amountCents: number): string {
  return (amountCents / 100).toFixed(2);
}

/** Wire representation of a stock position; tenant identity never leaves the service. */
export function toInventoryPayload(value: Inventory): InventoryPayload {
  return {
    id: value.id,
    status: value.status,
    amount: money(value.amountCents),
    reference: value.reference,
    labels: value.labels,
    summary: summarizeInventory(value),
    updatedAt: value.updatedAt,
  };
}

export function toInventoryPayloads(values: readonly Inventory[]): InventoryPayload[] {
  return values.map(toInventoryPayload);
}

export function toInventoryPagePayload(page: InventoryPage): InventoryPagePayload {
  return { items: toInventoryPayloads(page.items), total: page.total, offset: page.offset };
}

export function toInventorySummaryPayload(summary: InventorySummary): InventorySummaryPayload {
  return {
    total: summary.total,
    outstanding: summary.outstanding,
    amount: money(summary.amountCents),
    byStatus: summary.byStatus,
  };
}

export function totalAmountCents(values: readonly Inventory[]): number {
  return values.reduce((sum, value) => sum + value.amountCents, 0);
}

export function toInventoryCsvRow(value: Inventory): string {
  return [value.id, value.status, money(value.amountCents), value.reference].join(",");
}
