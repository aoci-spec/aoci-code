import {
  Integration,
  IntegrationStatus,
  applyIntegrationTransition,
  canIntegrationTransition,
  isIntegrationTerminal,
  newIntegration,
  withIntegrationAmount,
  withIntegrationLabel,
  integrationStatusCounts,
} from "./model";
import { IntegrationPage, IntegrationRepository } from "./repository";
import { IllegalTransitionError, ValidationError } from "../../infra/errors";
import { auditEvent } from "../../infra/audit";

export interface IntegrationSummary {
  readonly total: number;
  readonly outstanding: number;
  readonly amountCents: number;
  readonly byStatus: Record<IntegrationStatus, number>;
}

/** Business rules for the external integration lifecycle. */
export class IntegrationService {
  constructor(private readonly repository: IntegrationRepository) {}

  create(tenantId: string, id: string, reference: string, amountCents: number): Integration {
    const draft = withIntegrationAmount(newIntegration(id, tenantId, reference), amountCents);
    const saved = this.repository.insert(draft);
    auditEvent("integration.created", { tenantId, id });
    return saved;
  }

  get(tenantId: string, id: string): Integration {
    return this.repository.require(tenantId, id);
  }

  transition(tenantId: string, id: string, next: IntegrationStatus): Integration {
    const current = this.repository.require(tenantId, id);
    if (isIntegrationTerminal(current)) {
      throw new IllegalTransitionError(`integration ${id} is terminal`);
    }
    if (!canIntegrationTransition(current.status, next)) {
      throw new IllegalTransitionError(`integration ${id}: ${current.status} -> ${next}`);
    }
    const saved = this.repository.save(applyIntegrationTransition(current, next));
    auditEvent("integration.transitioned", { tenantId, id, next });
    return saved;
  }

  adjustAmount(tenantId: string, id: string, deltaCents: number): Integration {
    const current = this.repository.require(tenantId, id);
    if (isIntegrationTerminal(current)) {
      throw new IllegalTransitionError(`integration ${id} is terminal`);
    }
    const amountCents = current.amountCents + deltaCents;
    if (amountCents < 0) {
      throw new ValidationError(`integration ${id} cannot fall below zero`);
    }
    return this.repository.save(withIntegrationAmount(current, amountCents));
  }

  label(tenantId: string, id: string, label: string): Integration {
    return this.repository.save(withIntegrationLabel(this.repository.require(tenantId, id), label));
  }

  cancelAllDrafts(tenantId: string): number {
    const drafts = this.repository.listByStatus(tenantId, "draft");
    for (const draft of drafts) {
      this.repository.save(applyIntegrationTransition(draft, "cancelled"));
    }
    if (drafts.length > 0) {
      auditEvent("integration.drafts_cancelled", { tenantId, count: drafts.length });
    }
    return drafts.length;
  }

  outstanding(tenantId: string): Integration[] {
    return [
      ...this.repository.listByStatus(tenantId, "draft"),
      ...this.repository.listByStatus(tenantId, "active"),
    ];
  }

  page(tenantId: string, offset: number, limit: number): IntegrationPage {
    return this.repository.page(tenantId, offset, limit);
  }

  summary(tenantId: string): IntegrationSummary {
    const all = this.repository.all(tenantId);
    return {
      total: all.length,
      outstanding: this.outstanding(tenantId).length,
      amountCents: all.reduce((sum, item) => sum + item.amountCents, 0),
      byStatus: integrationStatusCounts(all),
    };
  }

  discard(tenantId: string, id: string): void {
    const current = this.repository.require(tenantId, id);
    if (!isIntegrationTerminal(current)) {
      throw new IllegalTransitionError(`integration ${id} must reach a terminal status first`);
    }
    this.repository.remove(tenantId, id);
    auditEvent("integration.discarded", { tenantId, id });
  }
}
