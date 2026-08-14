import {
  Metric,
  MetricStatus,
  applyMetricTransition,
  canMetricTransition,
  isMetricTerminal,
  newMetric,
  withMetricAmount,
  withMetricLabel,
  metricStatusCounts,
} from "./model";
import { MetricPage, MetricRepository } from "./repository";
import { IllegalTransitionError, ValidationError } from "../../infra/errors";
import { auditEvent } from "../../infra/audit";

export interface MetricSummary {
  readonly total: number;
  readonly outstanding: number;
  readonly amountCents: number;
  readonly byStatus: Record<MetricStatus, number>;
}

/** Business rules for the aggregated metric lifecycle. */
export class MetricService {
  constructor(private readonly repository: MetricRepository) {}

  create(tenantId: string, id: string, reference: string, amountCents: number): Metric {
    const draft = withMetricAmount(newMetric(id, tenantId, reference), amountCents);
    const saved = this.repository.insert(draft);
    auditEvent("metric.created", { tenantId, id });
    return saved;
  }

  get(tenantId: string, id: string): Metric {
    return this.repository.require(tenantId, id);
  }

  transition(tenantId: string, id: string, next: MetricStatus): Metric {
    const current = this.repository.require(tenantId, id);
    if (isMetricTerminal(current)) {
      throw new IllegalTransitionError(`metric ${id} is terminal`);
    }
    if (!canMetricTransition(current.status, next)) {
      throw new IllegalTransitionError(`metric ${id}: ${current.status} -> ${next}`);
    }
    const saved = this.repository.save(applyMetricTransition(current, next));
    auditEvent("metric.transitioned", { tenantId, id, next });
    return saved;
  }

  adjustAmount(tenantId: string, id: string, deltaCents: number): Metric {
    const current = this.repository.require(tenantId, id);
    if (isMetricTerminal(current)) {
      throw new IllegalTransitionError(`metric ${id} is terminal`);
    }
    const amountCents = current.amountCents + deltaCents;
    if (amountCents < 0) {
      throw new ValidationError(`metric ${id} cannot fall below zero`);
    }
    return this.repository.save(withMetricAmount(current, amountCents));
  }

  label(tenantId: string, id: string, label: string): Metric {
    return this.repository.save(withMetricLabel(this.repository.require(tenantId, id), label));
  }

  cancelAllDrafts(tenantId: string): number {
    const drafts = this.repository.listByStatus(tenantId, "draft");
    for (const draft of drafts) {
      this.repository.save(applyMetricTransition(draft, "cancelled"));
    }
    if (drafts.length > 0) {
      auditEvent("metric.drafts_cancelled", { tenantId, count: drafts.length });
    }
    return drafts.length;
  }

  outstanding(tenantId: string): Metric[] {
    return [
      ...this.repository.listByStatus(tenantId, "draft"),
      ...this.repository.listByStatus(tenantId, "active"),
    ];
  }

  page(tenantId: string, offset: number, limit: number): MetricPage {
    return this.repository.page(tenantId, offset, limit);
  }

  summary(tenantId: string): MetricSummary {
    const all = this.repository.all(tenantId);
    return {
      total: all.length,
      outstanding: this.outstanding(tenantId).length,
      amountCents: all.reduce((sum, item) => sum + item.amountCents, 0),
      byStatus: metricStatusCounts(all),
    };
  }

  discard(tenantId: string, id: string): void {
    const current = this.repository.require(tenantId, id);
    if (!isMetricTerminal(current)) {
      throw new IllegalTransitionError(`metric ${id} must reach a terminal status first`);
    }
    this.repository.remove(tenantId, id);
    auditEvent("metric.discarded", { tenantId, id });
  }
}
