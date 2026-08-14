import { Address, AddressStatus, summarizeAddress } from "./model";
import { AddressPage } from "./repository";
import { AddressSummary } from "./service";

export interface AddressPayload {
  id: string;
  status: string;
  amount: string;
  reference: string;
  labels: readonly string[];
  summary: string;
  updatedAt: string;
}

export interface AddressPagePayload {
  items: readonly AddressPayload[];
  total: number;
  offset: number;
}

export interface AddressSummaryPayload {
  total: number;
  outstanding: number;
  amount: string;
  byStatus: Record<AddressStatus, number>;
}

function money(amountCents: number): string {
  return (amountCents / 100).toFixed(2);
}

/** Wire representation of a postal address; tenant identity never leaves the service. */
export function toAddressPayload(value: Address): AddressPayload {
  return {
    id: value.id,
    status: value.status,
    amount: money(value.amountCents),
    reference: value.reference,
    labels: value.labels,
    summary: summarizeAddress(value),
    updatedAt: value.updatedAt,
  };
}

export function toAddressPayloads(values: readonly Address[]): AddressPayload[] {
  return values.map(toAddressPayload);
}

export function toAddressPagePayload(page: AddressPage): AddressPagePayload {
  return { items: toAddressPayloads(page.items), total: page.total, offset: page.offset };
}

export function toAddressSummaryPayload(summary: AddressSummary): AddressSummaryPayload {
  return {
    total: summary.total,
    outstanding: summary.outstanding,
    amount: money(summary.amountCents),
    byStatus: summary.byStatus,
  };
}

export function totalAmountCents(values: readonly Address[]): number {
  return values.reduce((sum, value) => sum + value.amountCents, 0);
}

export function toAddressCsvRow(value: Address): string {
  return [value.id, value.status, money(value.amountCents), value.reference].join(",");
}
