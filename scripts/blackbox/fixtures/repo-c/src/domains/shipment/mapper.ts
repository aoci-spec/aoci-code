import { Shipment, ShipmentStatus, summarizeShipment } from "./model";
import { ShipmentPage } from "./repository";
import { ShipmentSummary } from "./service";

export interface ShipmentPayload {
  id: string;
  status: string;
  amount: string;
  reference: string;
  labels: readonly string[];
  summary: string;
  updatedAt: string;
}

export interface ShipmentPagePayload {
  items: readonly ShipmentPayload[];
  total: number;
  offset: number;
}

export interface ShipmentSummaryPayload {
  total: number;
  outstanding: number;
  amount: string;
  byStatus: Record<ShipmentStatus, number>;
}

function money(amountCents: number): string {
  return (amountCents / 100).toFixed(2);
}

/** Wire representation of a outbound shipment; tenant identity never leaves the service. */
export function toShipmentPayload(value: Shipment): ShipmentPayload {
  return {
    id: value.id,
    status: value.status,
    amount: money(value.amountCents),
    reference: value.reference,
    labels: value.labels,
    summary: summarizeShipment(value),
    updatedAt: value.updatedAt,
  };
}

export function toShipmentPayloads(values: readonly Shipment[]): ShipmentPayload[] {
  return values.map(toShipmentPayload);
}

export function toShipmentPagePayload(page: ShipmentPage): ShipmentPagePayload {
  return { items: toShipmentPayloads(page.items), total: page.total, offset: page.offset };
}

export function toShipmentSummaryPayload(summary: ShipmentSummary): ShipmentSummaryPayload {
  return {
    total: summary.total,
    outstanding: summary.outstanding,
    amount: money(summary.amountCents),
    byStatus: summary.byStatus,
  };
}

export function totalAmountCents(values: readonly Shipment[]): number {
  return values.reduce((sum, value) => sum + value.amountCents, 0);
}

export function toShipmentCsvRow(value: Shipment): string {
  return [value.id, value.status, money(value.amountCents), value.reference].join(",");
}
