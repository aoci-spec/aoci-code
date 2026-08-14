import {
  Usage,
  UsageStatus,
  applyUsageTransition,
  canUsageTransition,
  isUsageTerminal,
  newUsage,
  withUsageAmount,
  withUsageLabel,
  usageStatusCounts,
} from "./model";
import { UsagePage, UsageRepository } from "./repository";
import { IllegalTransitionError, ValidationError } from "../../infra/errors";
import { auditEvent } from "../../infra/audit";

export interface UsageSummary {
  readonly total: number;
  readonly outstanding: number;
  readonly amountCents: number;
  readonly byStatus: Record<UsageStatus, number>;
}

/** Business rules for the metered usage record lifecycle. */
export class UsageService {
  constructor(private readonly repository: UsageRepository) {}

  create(tenantId: string, id: string, reference: string, amountCents: number): Usage {
    const draft = withUsageAmount(newUsage(id, tenantId, reference), amountCents);
    const saved = this.repository.insert(draft);
    auditEvent("usage.created", { tenantId, id });
    return saved;
  }

  get(tenantId: string, id: string): Usage {
    return this.repository.require(tenantId, id);
  }

  transition(tenantId: string, id: string, next: UsageStatus): Usage {
    const current = this.repository.require(tenantId, id);
    if (isUsageTerminal(current)) {
      throw new IllegalTransitionError(`usage ${id} is terminal`);
    }
    if (!canUsageTransition(current.status, next)) {
      throw new IllegalTransitionError(`usage ${id}: ${current.status} -> ${next}`);
    }
    const saved = this.repository.save(applyUsageTransition(current, next));
    auditEvent("usage.transitioned", { tenantId, id, next });
    return saved;
  }

  adjustAmount(tenantId: string, id: string, deltaCents: number): Usage {
    const current = this.repository.require(tenantId, id);
    if (isUsageTerminal(current)) {
      throw new IllegalTransitionError(`usage ${id} is terminal`);
    }
    const amountCents = current.amountCents + deltaCents;
    if (amountCents < 0) {
      throw new ValidationError(`usage ${id} cannot fall below zero`);
    }
    return this.repository.save(withUsageAmount(current, amountCents));
  }

  label(tenantId: string, id: string, label: string): Usage {
    return this.repository.save(withUsageLabel(this.repository.require(tenantId, id), label));
  }

  cancelAllDrafts(tenantId: string): number {
    const drafts = this.repository.listByStatus(tenantId, "draft");
    for (const draft of drafts) {
      this.repository.save(applyUsageTransition(draft, "cancelled"));
    }
    if (drafts.length > 0) {
      auditEvent("usage.drafts_cancelled", { tenantId, count: drafts.length });
    }
    return drafts.length;
  }

  outstanding(tenantId: string): Usage[] {
    return [
      ...this.repository.listByStatus(tenantId, "draft"),
      ...this.repository.listByStatus(tenantId, "active"),
    ];
  }

  page(tenantId: string, offset: number, limit: number): UsagePage {
    return this.repository.page(tenantId, offset, limit);
  }

  summary(tenantId: string): UsageSummary {
    const all = this.repository.all(tenantId);
    return {
      total: all.length,
      outstanding: this.outstanding(tenantId).length,
      amountCents: all.reduce((sum, item) => sum + item.amountCents, 0),
      byStatus: usageStatusCounts(all),
    };
  }

  discard(tenantId: string, id: string): void {
    const current = this.repository.require(tenantId, id);
    if (!isUsageTerminal(current)) {
      throw new IllegalTransitionError(`usage ${id} must reach a terminal status first`);
    }
    this.repository.remove(tenantId, id);
    auditEvent("usage.discarded", { tenantId, id });
  }
}
