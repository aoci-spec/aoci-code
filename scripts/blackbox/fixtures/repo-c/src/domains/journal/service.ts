import {
  Journal,
  JournalStatus,
  applyJournalTransition,
  canJournalTransition,
  isJournalTerminal,
  newJournal,
  withJournalAmount,
  withJournalLabel,
  journalStatusCounts,
} from "./model";
import { JournalPage, JournalRepository } from "./repository";
import { IllegalTransitionError, ValidationError } from "../../infra/errors";
import { auditEvent } from "../../infra/audit";

export interface JournalSummary {
  readonly total: number;
  readonly outstanding: number;
  readonly amountCents: number;
  readonly byStatus: Record<JournalStatus, number>;
}

/** Business rules for the journal batch lifecycle. */
export class JournalService {
  constructor(private readonly repository: JournalRepository) {}

  create(tenantId: string, id: string, reference: string, amountCents: number): Journal {
    const draft = withJournalAmount(newJournal(id, tenantId, reference), amountCents);
    const saved = this.repository.insert(draft);
    auditEvent("journal.created", { tenantId, id });
    return saved;
  }

  get(tenantId: string, id: string): Journal {
    return this.repository.require(tenantId, id);
  }

  transition(tenantId: string, id: string, next: JournalStatus): Journal {
    const current = this.repository.require(tenantId, id);
    if (isJournalTerminal(current)) {
      throw new IllegalTransitionError(`journal ${id} is terminal`);
    }
    if (!canJournalTransition(current.status, next)) {
      throw new IllegalTransitionError(`journal ${id}: ${current.status} -> ${next}`);
    }
    const saved = this.repository.save(applyJournalTransition(current, next));
    auditEvent("journal.transitioned", { tenantId, id, next });
    return saved;
  }

  adjustAmount(tenantId: string, id: string, deltaCents: number): Journal {
    const current = this.repository.require(tenantId, id);
    if (isJournalTerminal(current)) {
      throw new IllegalTransitionError(`journal ${id} is terminal`);
    }
    const amountCents = current.amountCents + deltaCents;
    if (amountCents < 0) {
      throw new ValidationError(`journal ${id} cannot fall below zero`);
    }
    return this.repository.save(withJournalAmount(current, amountCents));
  }

  label(tenantId: string, id: string, label: string): Journal {
    return this.repository.save(withJournalLabel(this.repository.require(tenantId, id), label));
  }

  cancelAllDrafts(tenantId: string): number {
    const drafts = this.repository.listByStatus(tenantId, "draft");
    for (const draft of drafts) {
      this.repository.save(applyJournalTransition(draft, "cancelled"));
    }
    if (drafts.length > 0) {
      auditEvent("journal.drafts_cancelled", { tenantId, count: drafts.length });
    }
    return drafts.length;
  }

  outstanding(tenantId: string): Journal[] {
    return [
      ...this.repository.listByStatus(tenantId, "draft"),
      ...this.repository.listByStatus(tenantId, "active"),
    ];
  }

  page(tenantId: string, offset: number, limit: number): JournalPage {
    return this.repository.page(tenantId, offset, limit);
  }

  summary(tenantId: string): JournalSummary {
    const all = this.repository.all(tenantId);
    return {
      total: all.length,
      outstanding: this.outstanding(tenantId).length,
      amountCents: all.reduce((sum, item) => sum + item.amountCents, 0),
      byStatus: journalStatusCounts(all),
    };
  }

  discard(tenantId: string, id: string): void {
    const current = this.repository.require(tenantId, id);
    if (!isJournalTerminal(current)) {
      throw new IllegalTransitionError(`journal ${id} must reach a terminal status first`);
    }
    this.repository.remove(tenantId, id);
    auditEvent("journal.discarded", { tenantId, id });
  }
}
