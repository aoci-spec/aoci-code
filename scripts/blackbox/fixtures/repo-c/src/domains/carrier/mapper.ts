import { Carrier, CarrierStatus, summarizeCarrier } from "./model";
import { CarrierPage } from "./repository";
import { CarrierSummary } from "./service";

export interface CarrierPayload {
  id: string;
  status: string;
  amount: string;
  reference: string;
  labels: readonly string[];
  summary: string;
  updatedAt: string;
}

export interface CarrierPagePayload {
  items: readonly CarrierPayload[];
  total: number;
  offset: number;
}

export interface CarrierSummaryPayload {
  total: number;
  outstanding: number;
  amount: string;
  byStatus: Record<CarrierStatus, number>;
}

function money(amountCents: number): string {
  return (amountCents / 100).toFixed(2);
}

/** Wire representation of a delivery carrier; tenant identity never leaves the service. */
export function toCarrierPayload(value: Carrier): CarrierPayload {
  return {
    id: value.id,
    status: value.status,
    amount: money(value.amountCents),
    reference: value.reference,
    labels: value.labels,
    summary: summarizeCarrier(value),
    updatedAt: value.updatedAt,
  };
}

export function toCarrierPayloads(values: readonly Carrier[]): CarrierPayload[] {
  return values.map(toCarrierPayload);
}

export function toCarrierPagePayload(page: CarrierPage): CarrierPagePayload {
  return { items: toCarrierPayloads(page.items), total: page.total, offset: page.offset };
}

export function toCarrierSummaryPayload(summary: CarrierSummary): CarrierSummaryPayload {
  return {
    total: summary.total,
    outstanding: summary.outstanding,
    amount: money(summary.amountCents),
    byStatus: summary.byStatus,
  };
}

export function totalAmountCents(values: readonly Carrier[]): number {
  return values.reduce((sum, value) => sum + value.amountCents, 0);
}

export function toCarrierCsvRow(value: Carrier): string {
  return [value.id, value.status, money(value.amountCents), value.reference].join(",");
}
