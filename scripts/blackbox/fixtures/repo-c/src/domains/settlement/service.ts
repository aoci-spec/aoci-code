import {
  Settlement,
  SettlementStatus,
  applySettlementTransition,
  canSettlementTransition,
  isSettlementTerminal,
  newSettlement,
  withSettlementAmount,
  withSettlementLabel,
  settlementStatusCounts,
} from "./model";
import { SettlementPage, SettlementRepository } from "./repository";
import { IllegalTransitionError, ValidationError } from "../../infra/errors";
import { auditEvent } from "../../infra/audit";

export interface SettlementSummary {
  readonly total: number;
  readonly outstanding: number;
  readonly amountCents: number;
  readonly byStatus: Record<SettlementStatus, number>;
}

/** Business rules for the settlement run lifecycle. */
export class SettlementService {
  constructor(private readonly repository: SettlementRepository) {}

  create(tenantId: string, id: string, reference: string, amountCents: number): Settlement {
    const draft = withSettlementAmount(newSettlement(id, tenantId, reference), amountCents);
    const saved = this.repository.insert(draft);
    auditEvent("settlement.created", { tenantId, id });
    return saved;
  }

  get(tenantId: string, id: string): Settlement {
    return this.repository.require(tenantId, id);
  }

  transition(tenantId: string, id: string, next: SettlementStatus): Settlement {
    const current = this.repository.require(tenantId, id);
    if (isSettlementTerminal(current)) {
      throw new IllegalTransitionError(`settlement ${id} is terminal`);
    }
    if (!canSettlementTransition(current.status, next)) {
      throw new IllegalTransitionError(`settlement ${id}: ${current.status} -> ${next}`);
    }
    const saved = this.repository.save(applySettlementTransition(current, next));
    auditEvent("settlement.transitioned", { tenantId, id, next });
    return saved;
  }

  adjustAmount(tenantId: string, id: string, deltaCents: number): Settlement {
    const current = this.repository.require(tenantId, id);
    if (isSettlementTerminal(current)) {
      throw new IllegalTransitionError(`settlement ${id} is terminal`);
    }
    const amountCents = current.amountCents + deltaCents;
    if (amountCents < 0) {
      throw new ValidationError(`settlement ${id} cannot fall below zero`);
    }
    return this.repository.save(withSettlementAmount(current, amountCents));
  }

  label(tenantId: string, id: string, label: string): Settlement {
    return this.repository.save(withSettlementLabel(this.repository.require(tenantId, id), label));
  }

  cancelAllDrafts(tenantId: string): number {
    const drafts = this.repository.listByStatus(tenantId, "draft");
    for (const draft of drafts) {
      this.repository.save(applySettlementTransition(draft, "cancelled"));
    }
    if (drafts.length > 0) {
      auditEvent("settlement.drafts_cancelled", { tenantId, count: drafts.length });
    }
    return drafts.length;
  }

  outstanding(tenantId: string): Settlement[] {
    return [
      ...this.repository.listByStatus(tenantId, "draft"),
      ...this.repository.listByStatus(tenantId, "active"),
    ];
  }

  page(tenantId: string, offset: number, limit: number): SettlementPage {
    return this.repository.page(tenantId, offset, limit);
  }

  summary(tenantId: string): SettlementSummary {
    const all = this.repository.all(tenantId);
    return {
      total: all.length,
      outstanding: this.outstanding(tenantId).length,
      amountCents: all.reduce((sum, item) => sum + item.amountCents, 0),
      byStatus: settlementStatusCounts(all),
    };
  }

  discard(tenantId: string, id: string): void {
    const current = this.repository.require(tenantId, id);
    if (!isSettlementTerminal(current)) {
      throw new IllegalTransitionError(`settlement ${id} must reach a terminal status first`);
    }
    this.repository.remove(tenantId, id);
    auditEvent("settlement.discarded", { tenantId, id });
  }
}
