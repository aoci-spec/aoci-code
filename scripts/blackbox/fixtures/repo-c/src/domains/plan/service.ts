import {
  Plan,
  PlanStatus,
  applyPlanTransition,
  canPlanTransition,
  isPlanTerminal,
  newPlan,
  withPlanAmount,
  withPlanLabel,
  planStatusCounts,
} from "./model";
import { PlanPage, PlanRepository } from "./repository";
import { IllegalTransitionError, ValidationError } from "../../infra/errors";
import { auditEvent } from "../../infra/audit";

export interface PlanSummary {
  readonly total: number;
  readonly outstanding: number;
  readonly amountCents: number;
  readonly byStatus: Record<PlanStatus, number>;
}

/** Business rules for the subscription plan lifecycle. */
export class PlanService {
  constructor(private readonly repository: PlanRepository) {}

  create(tenantId: string, id: string, reference: string, amountCents: number): Plan {
    const draft = withPlanAmount(newPlan(id, tenantId, reference), amountCents);
    const saved = this.repository.insert(draft);
    auditEvent("plan.created", { tenantId, id });
    return saved;
  }

  get(tenantId: string, id: string): Plan {
    return this.repository.require(tenantId, id);
  }

  transition(tenantId: string, id: string, next: PlanStatus): Plan {
    const current = this.repository.require(tenantId, id);
    if (isPlanTerminal(current)) {
      throw new IllegalTransitionError(`plan ${id} is terminal`);
    }
    if (!canPlanTransition(current.status, next)) {
      throw new IllegalTransitionError(`plan ${id}: ${current.status} -> ${next}`);
    }
    const saved = this.repository.save(applyPlanTransition(current, next));
    auditEvent("plan.transitioned", { tenantId, id, next });
    return saved;
  }

  adjustAmount(tenantId: string, id: string, deltaCents: number): Plan {
    const current = this.repository.require(tenantId, id);
    if (isPlanTerminal(current)) {
      throw new IllegalTransitionError(`plan ${id} is terminal`);
    }
    const amountCents = current.amountCents + deltaCents;
    if (amountCents < 0) {
      throw new ValidationError(`plan ${id} cannot fall below zero`);
    }
    return this.repository.save(withPlanAmount(current, amountCents));
  }

  label(tenantId: string, id: string, label: string): Plan {
    return this.repository.save(withPlanLabel(this.repository.require(tenantId, id), label));
  }

  cancelAllDrafts(tenantId: string): number {
    const drafts = this.repository.listByStatus(tenantId, "draft");
    for (const draft of drafts) {
      this.repository.save(applyPlanTransition(draft, "cancelled"));
    }
    if (drafts.length > 0) {
      auditEvent("plan.drafts_cancelled", { tenantId, count: drafts.length });
    }
    return drafts.length;
  }

  outstanding(tenantId: string): Plan[] {
    return [
      ...this.repository.listByStatus(tenantId, "draft"),
      ...this.repository.listByStatus(tenantId, "active"),
    ];
  }

  page(tenantId: string, offset: number, limit: number): PlanPage {
    return this.repository.page(tenantId, offset, limit);
  }

  summary(tenantId: string): PlanSummary {
    const all = this.repository.all(tenantId);
    return {
      total: all.length,
      outstanding: this.outstanding(tenantId).length,
      amountCents: all.reduce((sum, item) => sum + item.amountCents, 0),
      byStatus: planStatusCounts(all),
    };
  }

  discard(tenantId: string, id: string): void {
    const current = this.repository.require(tenantId, id);
    if (!isPlanTerminal(current)) {
      throw new IllegalTransitionError(`plan ${id} must reach a terminal status first`);
    }
    this.repository.remove(tenantId, id);
    auditEvent("plan.discarded", { tenantId, id });
  }
}
