import {
  Address,
  AddressStatus,
  applyAddressTransition,
  canAddressTransition,
  isAddressTerminal,
  newAddress,
  withAddressAmount,
  withAddressLabel,
  addressStatusCounts,
} from "./model";
import { AddressPage, AddressRepository } from "./repository";
import { IllegalTransitionError, ValidationError } from "../../infra/errors";
import { auditEvent } from "../../infra/audit";

export interface AddressSummary {
  readonly total: number;
  readonly outstanding: number;
  readonly amountCents: number;
  readonly byStatus: Record<AddressStatus, number>;
}

/** Business rules for the postal address lifecycle. */
export class AddressService {
  constructor(private readonly repository: AddressRepository) {}

  create(tenantId: string, id: string, reference: string, amountCents: number): Address {
    const draft = withAddressAmount(newAddress(id, tenantId, reference), amountCents);
    const saved = this.repository.insert(draft);
    auditEvent("address.created", { tenantId, id });
    return saved;
  }

  get(tenantId: string, id: string): Address {
    return this.repository.require(tenantId, id);
  }

  transition(tenantId: string, id: string, next: AddressStatus): Address {
    const current = this.repository.require(tenantId, id);
    if (isAddressTerminal(current)) {
      throw new IllegalTransitionError(`address ${id} is terminal`);
    }
    if (!canAddressTransition(current.status, next)) {
      throw new IllegalTransitionError(`address ${id}: ${current.status} -> ${next}`);
    }
    const saved = this.repository.save(applyAddressTransition(current, next));
    auditEvent("address.transitioned", { tenantId, id, next });
    return saved;
  }

  adjustAmount(tenantId: string, id: string, deltaCents: number): Address {
    const current = this.repository.require(tenantId, id);
    if (isAddressTerminal(current)) {
      throw new IllegalTransitionError(`address ${id} is terminal`);
    }
    const amountCents = current.amountCents + deltaCents;
    if (amountCents < 0) {
      throw new ValidationError(`address ${id} cannot fall below zero`);
    }
    return this.repository.save(withAddressAmount(current, amountCents));
  }

  label(tenantId: string, id: string, label: string): Address {
    return this.repository.save(withAddressLabel(this.repository.require(tenantId, id), label));
  }

  cancelAllDrafts(tenantId: string): number {
    const drafts = this.repository.listByStatus(tenantId, "draft");
    for (const draft of drafts) {
      this.repository.save(applyAddressTransition(draft, "cancelled"));
    }
    if (drafts.length > 0) {
      auditEvent("address.drafts_cancelled", { tenantId, count: drafts.length });
    }
    return drafts.length;
  }

  outstanding(tenantId: string): Address[] {
    return [
      ...this.repository.listByStatus(tenantId, "draft"),
      ...this.repository.listByStatus(tenantId, "active"),
    ];
  }

  page(tenantId: string, offset: number, limit: number): AddressPage {
    return this.repository.page(tenantId, offset, limit);
  }

  summary(tenantId: string): AddressSummary {
    const all = this.repository.all(tenantId);
    return {
      total: all.length,
      outstanding: this.outstanding(tenantId).length,
      amountCents: all.reduce((sum, item) => sum + item.amountCents, 0),
      byStatus: addressStatusCounts(all),
    };
  }

  discard(tenantId: string, id: string): void {
    const current = this.repository.require(tenantId, id);
    if (!isAddressTerminal(current)) {
      throw new IllegalTransitionError(`address ${id} must reach a terminal status first`);
    }
    this.repository.remove(tenantId, id);
    auditEvent("address.discarded", { tenantId, id });
  }
}
