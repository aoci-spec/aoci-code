import {
  Fraud,
  FraudStatus,
  applyFraudTransition,
  canFraudTransition,
  isFraudTerminal,
  newFraud,
  withFraudAmount,
  withFraudLabel,
  fraudStatusCounts,
} from "./model";
import { FraudPage, FraudRepository } from "./repository";
import { IllegalTransitionError, ValidationError } from "../../infra/errors";
import { auditEvent } from "../../infra/audit";

export interface FraudSummary {
  readonly total: number;
  readonly outstanding: number;
  readonly amountCents: number;
  readonly byStatus: Record<FraudStatus, number>;
}

/** Business rules for the fraud signal lifecycle. */
export class FraudService {
  constructor(private readonly repository: FraudRepository) {}

  create(tenantId: string, id: string, reference: string, amountCents: number): Fraud {
    const draft = withFraudAmount(newFraud(id, tenantId, reference), amountCents);
    const saved = this.repository.insert(draft);
    auditEvent("fraud.created", { tenantId, id });
    return saved;
  }

  get(tenantId: string, id: string): Fraud {
    return this.repository.require(tenantId, id);
  }

  transition(tenantId: string, id: string, next: FraudStatus): Fraud {
    const current = this.repository.require(tenantId, id);
    if (isFraudTerminal(current)) {
      throw new IllegalTransitionError(`fraud ${id} is terminal`);
    }
    if (!canFraudTransition(current.status, next)) {
      throw new IllegalTransitionError(`fraud ${id}: ${current.status} -> ${next}`);
    }
    const saved = this.repository.save(applyFraudTransition(current, next));
    auditEvent("fraud.transitioned", { tenantId, id, next });
    return saved;
  }

  adjustAmount(tenantId: string, id: string, deltaCents: number): Fraud {
    const current = this.repository.require(tenantId, id);
    if (isFraudTerminal(current)) {
      throw new IllegalTransitionError(`fraud ${id} is terminal`);
    }
    const amountCents = current.amountCents + deltaCents;
    if (amountCents < 0) {
      throw new ValidationError(`fraud ${id} cannot fall below zero`);
    }
    return this.repository.save(withFraudAmount(current, amountCents));
  }

  label(tenantId: string, id: string, label: string): Fraud {
    return this.repository.save(withFraudLabel(this.repository.require(tenantId, id), label));
  }

  cancelAllDrafts(tenantId: string): number {
    const drafts = this.repository.listByStatus(tenantId, "draft");
    for (const draft of drafts) {
      this.repository.save(applyFraudTransition(draft, "cancelled"));
    }
    if (drafts.length > 0) {
      auditEvent("fraud.drafts_cancelled", { tenantId, count: drafts.length });
    }
    return drafts.length;
  }

  outstanding(tenantId: string): Fraud[] {
    return [
      ...this.repository.listByStatus(tenantId, "draft"),
      ...this.repository.listByStatus(tenantId, "active"),
    ];
  }

  page(tenantId: string, offset: number, limit: number): FraudPage {
    return this.repository.page(tenantId, offset, limit);
  }

  summary(tenantId: string): FraudSummary {
    const all = this.repository.all(tenantId);
    return {
      total: all.length,
      outstanding: this.outstanding(tenantId).length,
      amountCents: all.reduce((sum, item) => sum + item.amountCents, 0),
      byStatus: fraudStatusCounts(all),
    };
  }

  discard(tenantId: string, id: string): void {
    const current = this.repository.require(tenantId, id);
    if (!isFraudTerminal(current)) {
      throw new IllegalTransitionError(`fraud ${id} must reach a terminal status first`);
    }
    this.repository.remove(tenantId, id);
    auditEvent("fraud.discarded", { tenantId, id });
  }
}
