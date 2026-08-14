import {
  Credential,
  CredentialStatus,
  applyCredentialTransition,
  canCredentialTransition,
  isCredentialTerminal,
  newCredential,
  withCredentialAmount,
  withCredentialLabel,
  credentialStatusCounts,
} from "./model";
import { CredentialPage, CredentialRepository } from "./repository";
import { IllegalTransitionError, ValidationError } from "../../infra/errors";
import { auditEvent } from "../../infra/audit";

export interface CredentialSummary {
  readonly total: number;
  readonly outstanding: number;
  readonly amountCents: number;
  readonly byStatus: Record<CredentialStatus, number>;
}

/** Business rules for the stored credential lifecycle. */
export class CredentialService {
  constructor(private readonly repository: CredentialRepository) {}

  create(tenantId: string, id: string, reference: string, amountCents: number): Credential {
    const draft = withCredentialAmount(newCredential(id, tenantId, reference), amountCents);
    const saved = this.repository.insert(draft);
    auditEvent("credential.created", { tenantId, id });
    return saved;
  }

  get(tenantId: string, id: string): Credential {
    return this.repository.require(tenantId, id);
  }

  transition(tenantId: string, id: string, next: CredentialStatus): Credential {
    const current = this.repository.require(tenantId, id);
    if (isCredentialTerminal(current)) {
      throw new IllegalTransitionError(`credential ${id} is terminal`);
    }
    if (!canCredentialTransition(current.status, next)) {
      throw new IllegalTransitionError(`credential ${id}: ${current.status} -> ${next}`);
    }
    const saved = this.repository.save(applyCredentialTransition(current, next));
    auditEvent("credential.transitioned", { tenantId, id, next });
    return saved;
  }

  adjustAmount(tenantId: string, id: string, deltaCents: number): Credential {
    const current = this.repository.require(tenantId, id);
    if (isCredentialTerminal(current)) {
      throw new IllegalTransitionError(`credential ${id} is terminal`);
    }
    const amountCents = current.amountCents + deltaCents;
    if (amountCents < 0) {
      throw new ValidationError(`credential ${id} cannot fall below zero`);
    }
    return this.repository.save(withCredentialAmount(current, amountCents));
  }

  label(tenantId: string, id: string, label: string): Credential {
    return this.repository.save(withCredentialLabel(this.repository.require(tenantId, id), label));
  }

  cancelAllDrafts(tenantId: string): number {
    const drafts = this.repository.listByStatus(tenantId, "draft");
    for (const draft of drafts) {
      this.repository.save(applyCredentialTransition(draft, "cancelled"));
    }
    if (drafts.length > 0) {
      auditEvent("credential.drafts_cancelled", { tenantId, count: drafts.length });
    }
    return drafts.length;
  }

  outstanding(tenantId: string): Credential[] {
    return [
      ...this.repository.listByStatus(tenantId, "draft"),
      ...this.repository.listByStatus(tenantId, "active"),
    ];
  }

  page(tenantId: string, offset: number, limit: number): CredentialPage {
    return this.repository.page(tenantId, offset, limit);
  }

  summary(tenantId: string): CredentialSummary {
    const all = this.repository.all(tenantId);
    return {
      total: all.length,
      outstanding: this.outstanding(tenantId).length,
      amountCents: all.reduce((sum, item) => sum + item.amountCents, 0),
      byStatus: credentialStatusCounts(all),
    };
  }

  discard(tenantId: string, id: string): void {
    const current = this.repository.require(tenantId, id);
    if (!isCredentialTerminal(current)) {
      throw new IllegalTransitionError(`credential ${id} must reach a terminal status first`);
    }
    this.repository.remove(tenantId, id);
    auditEvent("credential.discarded", { tenantId, id });
  }
}
