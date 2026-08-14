import {
  Feature,
  FeatureStatus,
  applyFeatureTransition,
  canFeatureTransition,
  isFeatureTerminal,
  newFeature,
  withFeatureAmount,
  withFeatureLabel,
  featureStatusCounts,
} from "./model";
import { FeaturePage, FeatureRepository } from "./repository";
import { IllegalTransitionError, ValidationError } from "../../infra/errors";
import { auditEvent } from "../../infra/audit";

export interface FeatureSummary {
  readonly total: number;
  readonly outstanding: number;
  readonly amountCents: number;
  readonly byStatus: Record<FeatureStatus, number>;
}

/** Business rules for the feature flag lifecycle. */
export class FeatureService {
  constructor(private readonly repository: FeatureRepository) {}

  create(tenantId: string, id: string, reference: string, amountCents: number): Feature {
    const draft = withFeatureAmount(newFeature(id, tenantId, reference), amountCents);
    const saved = this.repository.insert(draft);
    auditEvent("feature.created", { tenantId, id });
    return saved;
  }

  get(tenantId: string, id: string): Feature {
    return this.repository.require(tenantId, id);
  }

  transition(tenantId: string, id: string, next: FeatureStatus): Feature {
    const current = this.repository.require(tenantId, id);
    if (isFeatureTerminal(current)) {
      throw new IllegalTransitionError(`feature ${id} is terminal`);
    }
    if (!canFeatureTransition(current.status, next)) {
      throw new IllegalTransitionError(`feature ${id}: ${current.status} -> ${next}`);
    }
    const saved = this.repository.save(applyFeatureTransition(current, next));
    auditEvent("feature.transitioned", { tenantId, id, next });
    return saved;
  }

  adjustAmount(tenantId: string, id: string, deltaCents: number): Feature {
    const current = this.repository.require(tenantId, id);
    if (isFeatureTerminal(current)) {
      throw new IllegalTransitionError(`feature ${id} is terminal`);
    }
    const amountCents = current.amountCents + deltaCents;
    if (amountCents < 0) {
      throw new ValidationError(`feature ${id} cannot fall below zero`);
    }
    return this.repository.save(withFeatureAmount(current, amountCents));
  }

  label(tenantId: string, id: string, label: string): Feature {
    return this.repository.save(withFeatureLabel(this.repository.require(tenantId, id), label));
  }

  cancelAllDrafts(tenantId: string): number {
    const drafts = this.repository.listByStatus(tenantId, "draft");
    for (const draft of drafts) {
      this.repository.save(applyFeatureTransition(draft, "cancelled"));
    }
    if (drafts.length > 0) {
      auditEvent("feature.drafts_cancelled", { tenantId, count: drafts.length });
    }
    return drafts.length;
  }

  outstanding(tenantId: string): Feature[] {
    return [
      ...this.repository.listByStatus(tenantId, "draft"),
      ...this.repository.listByStatus(tenantId, "active"),
    ];
  }

  page(tenantId: string, offset: number, limit: number): FeaturePage {
    return this.repository.page(tenantId, offset, limit);
  }

  summary(tenantId: string): FeatureSummary {
    const all = this.repository.all(tenantId);
    return {
      total: all.length,
      outstanding: this.outstanding(tenantId).length,
      amountCents: all.reduce((sum, item) => sum + item.amountCents, 0),
      byStatus: featureStatusCounts(all),
    };
  }

  discard(tenantId: string, id: string): void {
    const current = this.repository.require(tenantId, id);
    if (!isFeatureTerminal(current)) {
      throw new IllegalTransitionError(`feature ${id} must reach a terminal status first`);
    }
    this.repository.remove(tenantId, id);
    auditEvent("feature.discarded", { tenantId, id });
  }
}
