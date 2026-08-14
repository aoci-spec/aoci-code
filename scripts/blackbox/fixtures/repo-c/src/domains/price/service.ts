import {
  Price,
  PriceStatus,
  applyPriceTransition,
  canPriceTransition,
  isPriceTerminal,
  newPrice,
  withPriceAmount,
  withPriceLabel,
  priceStatusCounts,
} from "./model";
import { PricePage, PriceRepository } from "./repository";
import { IllegalTransitionError, ValidationError } from "../../infra/errors";
import { auditEvent } from "../../infra/audit";

export interface PriceSummary {
  readonly total: number;
  readonly outstanding: number;
  readonly amountCents: number;
  readonly byStatus: Record<PriceStatus, number>;
}

/** Business rules for the price definition lifecycle. */
export class PriceService {
  constructor(private readonly repository: PriceRepository) {}

  create(tenantId: string, id: string, reference: string, amountCents: number): Price {
    const draft = withPriceAmount(newPrice(id, tenantId, reference), amountCents);
    const saved = this.repository.insert(draft);
    auditEvent("price.created", { tenantId, id });
    return saved;
  }

  get(tenantId: string, id: string): Price {
    return this.repository.require(tenantId, id);
  }

  transition(tenantId: string, id: string, next: PriceStatus): Price {
    const current = this.repository.require(tenantId, id);
    if (isPriceTerminal(current)) {
      throw new IllegalTransitionError(`price ${id} is terminal`);
    }
    if (!canPriceTransition(current.status, next)) {
      throw new IllegalTransitionError(`price ${id}: ${current.status} -> ${next}`);
    }
    const saved = this.repository.save(applyPriceTransition(current, next));
    auditEvent("price.transitioned", { tenantId, id, next });
    return saved;
  }

  adjustAmount(tenantId: string, id: string, deltaCents: number): Price {
    const current = this.repository.require(tenantId, id);
    if (isPriceTerminal(current)) {
      throw new IllegalTransitionError(`price ${id} is terminal`);
    }
    const amountCents = current.amountCents + deltaCents;
    if (amountCents < 0) {
      throw new ValidationError(`price ${id} cannot fall below zero`);
    }
    return this.repository.save(withPriceAmount(current, amountCents));
  }

  label(tenantId: string, id: string, label: string): Price {
    return this.repository.save(withPriceLabel(this.repository.require(tenantId, id), label));
  }

  cancelAllDrafts(tenantId: string): number {
    const drafts = this.repository.listByStatus(tenantId, "draft");
    for (const draft of drafts) {
      this.repository.save(applyPriceTransition(draft, "cancelled"));
    }
    if (drafts.length > 0) {
      auditEvent("price.drafts_cancelled", { tenantId, count: drafts.length });
    }
    return drafts.length;
  }

  outstanding(tenantId: string): Price[] {
    return [
      ...this.repository.listByStatus(tenantId, "draft"),
      ...this.repository.listByStatus(tenantId, "active"),
    ];
  }

  page(tenantId: string, offset: number, limit: number): PricePage {
    return this.repository.page(tenantId, offset, limit);
  }

  summary(tenantId: string): PriceSummary {
    const all = this.repository.all(tenantId);
    return {
      total: all.length,
      outstanding: this.outstanding(tenantId).length,
      amountCents: all.reduce((sum, item) => sum + item.amountCents, 0),
      byStatus: priceStatusCounts(all),
    };
  }

  discard(tenantId: string, id: string): void {
    const current = this.repository.require(tenantId, id);
    if (!isPriceTerminal(current)) {
      throw new IllegalTransitionError(`price ${id} must reach a terminal status first`);
    }
    this.repository.remove(tenantId, id);
    auditEvent("price.discarded", { tenantId, id });
  }
}
