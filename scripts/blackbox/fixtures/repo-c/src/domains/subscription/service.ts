import {
  Subscription,
  SubscriptionStatus,
  applySubscriptionTransition,
  canSubscriptionTransition,
  isSubscriptionTerminal,
  newSubscription,
  withSubscriptionAmount,
  withSubscriptionLabel,
  subscriptionStatusCounts,
} from "./model";
import { SubscriptionPage, SubscriptionRepository } from "./repository";
import { IllegalTransitionError, ValidationError } from "../../infra/errors";
import { auditEvent } from "../../infra/audit";

export interface SubscriptionSummary {
  readonly total: number;
  readonly outstanding: number;
  readonly amountCents: number;
  readonly byStatus: Record<SubscriptionStatus, number>;
}

/** Business rules for the recurring subscription lifecycle. */
export class SubscriptionService {
  constructor(private readonly repository: SubscriptionRepository) {}

  create(tenantId: string, id: string, reference: string, amountCents: number): Subscription {
    const draft = withSubscriptionAmount(newSubscription(id, tenantId, reference), amountCents);
    const saved = this.repository.insert(draft);
    auditEvent("subscription.created", { tenantId, id });
    return saved;
  }

  get(tenantId: string, id: string): Subscription {
    return this.repository.require(tenantId, id);
  }

  transition(tenantId: string, id: string, next: SubscriptionStatus): Subscription {
    const current = this.repository.require(tenantId, id);
    if (isSubscriptionTerminal(current)) {
      throw new IllegalTransitionError(`subscription ${id} is terminal`);
    }
    if (!canSubscriptionTransition(current.status, next)) {
      throw new IllegalTransitionError(`subscription ${id}: ${current.status} -> ${next}`);
    }
    const saved = this.repository.save(applySubscriptionTransition(current, next));
    auditEvent("subscription.transitioned", { tenantId, id, next });
    return saved;
  }

  adjustAmount(tenantId: string, id: string, deltaCents: number): Subscription {
    const current = this.repository.require(tenantId, id);
    if (isSubscriptionTerminal(current)) {
      throw new IllegalTransitionError(`subscription ${id} is terminal`);
    }
    const amountCents = current.amountCents + deltaCents;
    if (amountCents < 0) {
      throw new ValidationError(`subscription ${id} cannot fall below zero`);
    }
    return this.repository.save(withSubscriptionAmount(current, amountCents));
  }

  label(tenantId: string, id: string, label: string): Subscription {
    return this.repository.save(withSubscriptionLabel(this.repository.require(tenantId, id), label));
  }

  cancelAllDrafts(tenantId: string): number {
    const drafts = this.repository.listByStatus(tenantId, "draft");
    for (const draft of drafts) {
      this.repository.save(applySubscriptionTransition(draft, "cancelled"));
    }
    if (drafts.length > 0) {
      auditEvent("subscription.drafts_cancelled", { tenantId, count: drafts.length });
    }
    return drafts.length;
  }

  outstanding(tenantId: string): Subscription[] {
    return [
      ...this.repository.listByStatus(tenantId, "draft"),
      ...this.repository.listByStatus(tenantId, "active"),
    ];
  }

  page(tenantId: string, offset: number, limit: number): SubscriptionPage {
    return this.repository.page(tenantId, offset, limit);
  }

  summary(tenantId: string): SubscriptionSummary {
    const all = this.repository.all(tenantId);
    return {
      total: all.length,
      outstanding: this.outstanding(tenantId).length,
      amountCents: all.reduce((sum, item) => sum + item.amountCents, 0),
      byStatus: subscriptionStatusCounts(all),
    };
  }

  discard(tenantId: string, id: string): void {
    const current = this.repository.require(tenantId, id);
    if (!isSubscriptionTerminal(current)) {
      throw new IllegalTransitionError(`subscription ${id} must reach a terminal status first`);
    }
    this.repository.remove(tenantId, id);
    auditEvent("subscription.discarded", { tenantId, id });
  }
}
