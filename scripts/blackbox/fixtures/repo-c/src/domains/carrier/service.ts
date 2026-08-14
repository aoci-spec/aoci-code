import {
  Carrier,
  CarrierStatus,
  applyCarrierTransition,
  canCarrierTransition,
  isCarrierTerminal,
  newCarrier,
  withCarrierAmount,
  withCarrierLabel,
  carrierStatusCounts,
} from "./model";
import { CarrierPage, CarrierRepository } from "./repository";
import { IllegalTransitionError, ValidationError } from "../../infra/errors";
import { auditEvent } from "../../infra/audit";

export interface CarrierSummary {
  readonly total: number;
  readonly outstanding: number;
  readonly amountCents: number;
  readonly byStatus: Record<CarrierStatus, number>;
}

/** Business rules for the delivery carrier lifecycle. */
export class CarrierService {
  constructor(private readonly repository: CarrierRepository) {}

  create(tenantId: string, id: string, reference: string, amountCents: number): Carrier {
    const draft = withCarrierAmount(newCarrier(id, tenantId, reference), amountCents);
    const saved = this.repository.insert(draft);
    auditEvent("carrier.created", { tenantId, id });
    return saved;
  }

  get(tenantId: string, id: string): Carrier {
    return this.repository.require(tenantId, id);
  }

  transition(tenantId: string, id: string, next: CarrierStatus): Carrier {
    const current = this.repository.require(tenantId, id);
    if (isCarrierTerminal(current)) {
      throw new IllegalTransitionError(`carrier ${id} is terminal`);
    }
    if (!canCarrierTransition(current.status, next)) {
      throw new IllegalTransitionError(`carrier ${id}: ${current.status} -> ${next}`);
    }
    const saved = this.repository.save(applyCarrierTransition(current, next));
    auditEvent("carrier.transitioned", { tenantId, id, next });
    return saved;
  }

  adjustAmount(tenantId: string, id: string, deltaCents: number): Carrier {
    const current = this.repository.require(tenantId, id);
    if (isCarrierTerminal(current)) {
      throw new IllegalTransitionError(`carrier ${id} is terminal`);
    }
    const amountCents = current.amountCents + deltaCents;
    if (amountCents < 0) {
      throw new ValidationError(`carrier ${id} cannot fall below zero`);
    }
    return this.repository.save(withCarrierAmount(current, amountCents));
  }

  label(tenantId: string, id: string, label: string): Carrier {
    return this.repository.save(withCarrierLabel(this.repository.require(tenantId, id), label));
  }

  cancelAllDrafts(tenantId: string): number {
    const drafts = this.repository.listByStatus(tenantId, "draft");
    for (const draft of drafts) {
      this.repository.save(applyCarrierTransition(draft, "cancelled"));
    }
    if (drafts.length > 0) {
      auditEvent("carrier.drafts_cancelled", { tenantId, count: drafts.length });
    }
    return drafts.length;
  }

  outstanding(tenantId: string): Carrier[] {
    return [
      ...this.repository.listByStatus(tenantId, "draft"),
      ...this.repository.listByStatus(tenantId, "active"),
    ];
  }

  page(tenantId: string, offset: number, limit: number): CarrierPage {
    return this.repository.page(tenantId, offset, limit);
  }

  summary(tenantId: string): CarrierSummary {
    const all = this.repository.all(tenantId);
    return {
      total: all.length,
      outstanding: this.outstanding(tenantId).length,
      amountCents: all.reduce((sum, item) => sum + item.amountCents, 0),
      byStatus: carrierStatusCounts(all),
    };
  }

  discard(tenantId: string, id: string): void {
    const current = this.repository.require(tenantId, id);
    if (!isCarrierTerminal(current)) {
      throw new IllegalTransitionError(`carrier ${id} must reach a terminal status first`);
    }
    this.repository.remove(tenantId, id);
    auditEvent("carrier.discarded", { tenantId, id });
  }
}
