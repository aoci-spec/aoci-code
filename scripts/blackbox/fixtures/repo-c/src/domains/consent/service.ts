import {
  Consent,
  ConsentStatus,
  applyConsentTransition,
  canConsentTransition,
  isConsentTerminal,
  newConsent,
  withConsentAmount,
  withConsentLabel,
  consentStatusCounts,
} from "./model";
import { ConsentPage, ConsentRepository } from "./repository";
import { IllegalTransitionError, ValidationError } from "../../infra/errors";
import { auditEvent } from "../../infra/audit";

export interface ConsentSummary {
  readonly total: number;
  readonly outstanding: number;
  readonly amountCents: number;
  readonly byStatus: Record<ConsentStatus, number>;
}

/** Business rules for the privacy consent lifecycle. */
export class ConsentService {
  constructor(private readonly repository: ConsentRepository) {}

  create(tenantId: string, id: string, reference: string, amountCents: number): Consent {
    const draft = withConsentAmount(newConsent(id, tenantId, reference), amountCents);
    const saved = this.repository.insert(draft);
    auditEvent("consent.created", { tenantId, id });
    return saved;
  }

  get(tenantId: string, id: string): Consent {
    return this.repository.require(tenantId, id);
  }

  transition(tenantId: string, id: string, next: ConsentStatus): Consent {
    const current = this.repository.require(tenantId, id);
    if (isConsentTerminal(current)) {
      throw new IllegalTransitionError(`consent ${id} is terminal`);
    }
    if (!canConsentTransition(current.status, next)) {
      throw new IllegalTransitionError(`consent ${id}: ${current.status} -> ${next}`);
    }
    const saved = this.repository.save(applyConsentTransition(current, next));
    auditEvent("consent.transitioned", { tenantId, id, next });
    return saved;
  }

  adjustAmount(tenantId: string, id: string, deltaCents: number): Consent {
    const current = this.repository.require(tenantId, id);
    if (isConsentTerminal(current)) {
      throw new IllegalTransitionError(`consent ${id} is terminal`);
    }
    const amountCents = current.amountCents + deltaCents;
    if (amountCents < 0) {
      throw new ValidationError(`consent ${id} cannot fall below zero`);
    }
    return this.repository.save(withConsentAmount(current, amountCents));
  }

  label(tenantId: string, id: string, label: string): Consent {
    return this.repository.save(withConsentLabel(this.repository.require(tenantId, id), label));
  }

  cancelAllDrafts(tenantId: string): number {
    const drafts = this.repository.listByStatus(tenantId, "draft");
    for (const draft of drafts) {
      this.repository.save(applyConsentTransition(draft, "cancelled"));
    }
    if (drafts.length > 0) {
      auditEvent("consent.drafts_cancelled", { tenantId, count: drafts.length });
    }
    return drafts.length;
  }

  outstanding(tenantId: string): Consent[] {
    return [
      ...this.repository.listByStatus(tenantId, "draft"),
      ...this.repository.listByStatus(tenantId, "active"),
    ];
  }

  page(tenantId: string, offset: number, limit: number): ConsentPage {
    return this.repository.page(tenantId, offset, limit);
  }

  summary(tenantId: string): ConsentSummary {
    const all = this.repository.all(tenantId);
    return {
      total: all.length,
      outstanding: this.outstanding(tenantId).length,
      amountCents: all.reduce((sum, item) => sum + item.amountCents, 0),
      byStatus: consentStatusCounts(all),
    };
  }

  discard(tenantId: string, id: string): void {
    const current = this.repository.require(tenantId, id);
    if (!isConsentTerminal(current)) {
      throw new IllegalTransitionError(`consent ${id} must reach a terminal status first`);
    }
    this.repository.remove(tenantId, id);
    auditEvent("consent.discarded", { tenantId, id });
  }
}
