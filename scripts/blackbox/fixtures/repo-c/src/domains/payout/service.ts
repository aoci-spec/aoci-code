import {
  Payout,
  PayoutStatus,
  applyPayoutTransition,
  canPayoutTransition,
  isPayoutTerminal,
  newPayout,
  withPayoutAmount,
  withPayoutLabel,
  payoutStatusCounts,
} from "./model";
import { PayoutPage, PayoutRepository } from "./repository";
import { IllegalTransitionError, ValidationError } from "../../infra/errors";
import { auditEvent } from "../../infra/audit";

export interface PayoutSummary {
  readonly total: number;
  readonly outstanding: number;
  readonly amountCents: number;
  readonly byStatus: Record<PayoutStatus, number>;
}

/** Business rules for the merchant payout lifecycle. */
export class PayoutService {
  constructor(private readonly repository: PayoutRepository) {}

  create(tenantId: string, id: string, reference: string, amountCents: number): Payout {
    const draft = withPayoutAmount(newPayout(id, tenantId, reference), amountCents);
    const saved = this.repository.insert(draft);
    auditEvent("payout.created", { tenantId, id });
    return saved;
  }

  get(tenantId: string, id: string): Payout {
    return this.repository.require(tenantId, id);
  }

  transition(tenantId: string, id: string, next: PayoutStatus): Payout {
    const current = this.repository.require(tenantId, id);
    if (isPayoutTerminal(current)) {
      throw new IllegalTransitionError(`payout ${id} is terminal`);
    }
    if (!canPayoutTransition(current.status, next)) {
      throw new IllegalTransitionError(`payout ${id}: ${current.status} -> ${next}`);
    }
    const saved = this.repository.save(applyPayoutTransition(current, next));
    auditEvent("payout.transitioned", { tenantId, id, next });
    return saved;
  }

  adjustAmount(tenantId: string, id: string, deltaCents: number): Payout {
    const current = this.repository.require(tenantId, id);
    if (isPayoutTerminal(current)) {
      throw new IllegalTransitionError(`payout ${id} is terminal`);
    }
    const amountCents = current.amountCents + deltaCents;
    if (amountCents < 0) {
      throw new ValidationError(`payout ${id} cannot fall below zero`);
    }
    return this.repository.save(withPayoutAmount(current, amountCents));
  }

  label(tenantId: string, id: string, label: string): Payout {
    return this.repository.save(withPayoutLabel(this.repository.require(tenantId, id), label));
  }

  cancelAllDrafts(tenantId: string): number {
    const drafts = this.repository.listByStatus(tenantId, "draft");
    for (const draft of drafts) {
      this.repository.save(applyPayoutTransition(draft, "cancelled"));
    }
    if (drafts.length > 0) {
      auditEvent("payout.drafts_cancelled", { tenantId, count: drafts.length });
    }
    return drafts.length;
  }

  outstanding(tenantId: string): Payout[] {
    return [
      ...this.repository.listByStatus(tenantId, "draft"),
      ...this.repository.listByStatus(tenantId, "active"),
    ];
  }

  page(tenantId: string, offset: number, limit: number): PayoutPage {
    return this.repository.page(tenantId, offset, limit);
  }

  summary(tenantId: string): PayoutSummary {
    const all = this.repository.all(tenantId);
    return {
      total: all.length,
      outstanding: this.outstanding(tenantId).length,
      amountCents: all.reduce((sum, item) => sum + item.amountCents, 0),
      byStatus: payoutStatusCounts(all),
    };
  }

  discard(tenantId: string, id: string): void {
    const current = this.repository.require(tenantId, id);
    if (!isPayoutTerminal(current)) {
      throw new IllegalTransitionError(`payout ${id} must reach a terminal status first`);
    }
    this.repository.remove(tenantId, id);
    auditEvent("payout.discarded", { tenantId, id });
  }
}
