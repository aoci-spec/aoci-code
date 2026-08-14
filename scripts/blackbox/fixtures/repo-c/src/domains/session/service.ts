import {
  Session,
  SessionStatus,
  applySessionTransition,
  canSessionTransition,
  isSessionTerminal,
  newSession,
  withSessionAmount,
  withSessionLabel,
  sessionStatusCounts,
} from "./model";
import { SessionPage, SessionRepository } from "./repository";
import { IllegalTransitionError, ValidationError } from "../../infra/errors";
import { auditEvent } from "../../infra/audit";

export interface SessionSummary {
  readonly total: number;
  readonly outstanding: number;
  readonly amountCents: number;
  readonly byStatus: Record<SessionStatus, number>;
}

/** Business rules for the authenticated session lifecycle. */
export class SessionService {
  constructor(private readonly repository: SessionRepository) {}

  create(tenantId: string, id: string, reference: string, amountCents: number): Session {
    const draft = withSessionAmount(newSession(id, tenantId, reference), amountCents);
    const saved = this.repository.insert(draft);
    auditEvent("session.created", { tenantId, id });
    return saved;
  }

  get(tenantId: string, id: string): Session {
    return this.repository.require(tenantId, id);
  }

  transition(tenantId: string, id: string, next: SessionStatus): Session {
    const current = this.repository.require(tenantId, id);
    if (isSessionTerminal(current)) {
      throw new IllegalTransitionError(`session ${id} is terminal`);
    }
    if (!canSessionTransition(current.status, next)) {
      throw new IllegalTransitionError(`session ${id}: ${current.status} -> ${next}`);
    }
    const saved = this.repository.save(applySessionTransition(current, next));
    auditEvent("session.transitioned", { tenantId, id, next });
    return saved;
  }

  adjustAmount(tenantId: string, id: string, deltaCents: number): Session {
    const current = this.repository.require(tenantId, id);
    if (isSessionTerminal(current)) {
      throw new IllegalTransitionError(`session ${id} is terminal`);
    }
    const amountCents = current.amountCents + deltaCents;
    if (amountCents < 0) {
      throw new ValidationError(`session ${id} cannot fall below zero`);
    }
    return this.repository.save(withSessionAmount(current, amountCents));
  }

  label(tenantId: string, id: string, label: string): Session {
    return this.repository.save(withSessionLabel(this.repository.require(tenantId, id), label));
  }

  cancelAllDrafts(tenantId: string): number {
    const drafts = this.repository.listByStatus(tenantId, "draft");
    for (const draft of drafts) {
      this.repository.save(applySessionTransition(draft, "cancelled"));
    }
    if (drafts.length > 0) {
      auditEvent("session.drafts_cancelled", { tenantId, count: drafts.length });
    }
    return drafts.length;
  }

  outstanding(tenantId: string): Session[] {
    return [
      ...this.repository.listByStatus(tenantId, "draft"),
      ...this.repository.listByStatus(tenantId, "active"),
    ];
  }

  page(tenantId: string, offset: number, limit: number): SessionPage {
    return this.repository.page(tenantId, offset, limit);
  }

  summary(tenantId: string): SessionSummary {
    const all = this.repository.all(tenantId);
    return {
      total: all.length,
      outstanding: this.outstanding(tenantId).length,
      amountCents: all.reduce((sum, item) => sum + item.amountCents, 0),
      byStatus: sessionStatusCounts(all),
    };
  }

  discard(tenantId: string, id: string): void {
    const current = this.repository.require(tenantId, id);
    if (!isSessionTerminal(current)) {
      throw new IllegalTransitionError(`session ${id} must reach a terminal status first`);
    }
    this.repository.remove(tenantId, id);
    auditEvent("session.discarded", { tenantId, id });
  }
}
