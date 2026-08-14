import {
  Ledger,
  LedgerStatus,
  applyLedgerTransition,
  canLedgerTransition,
  isLedgerTerminal,
  newLedger,
  withLedgerAmount,
  withLedgerLabel,
  ledgerStatusCounts,
} from "./model";
import { LedgerPage, LedgerRepository } from "./repository";
import { IllegalTransitionError, ValidationError } from "../../infra/errors";
import { auditEvent } from "../../infra/audit";

export interface LedgerSummary {
  readonly total: number;
  readonly outstanding: number;
  readonly amountCents: number;
  readonly byStatus: Record<LedgerStatus, number>;
}

/** Business rules for the accounting ledger entry lifecycle. */
export class LedgerService {
  constructor(private readonly repository: LedgerRepository) {}

  create(tenantId: string, id: string, reference: string, amountCents: number): Ledger {
    const draft = withLedgerAmount(newLedger(id, tenantId, reference), amountCents);
    const saved = this.repository.insert(draft);
    auditEvent("ledger.created", { tenantId, id });
    return saved;
  }

  get(tenantId: string, id: string): Ledger {
    return this.repository.require(tenantId, id);
  }

  transition(tenantId: string, id: string, next: LedgerStatus): Ledger {
    const current = this.repository.require(tenantId, id);
    if (isLedgerTerminal(current)) {
      throw new IllegalTransitionError(`ledger ${id} is terminal`);
    }
    if (!canLedgerTransition(current.status, next)) {
      throw new IllegalTransitionError(`ledger ${id}: ${current.status} -> ${next}`);
    }
    const saved = this.repository.save(applyLedgerTransition(current, next));
    auditEvent("ledger.transitioned", { tenantId, id, next });
    return saved;
  }

  adjustAmount(tenantId: string, id: string, deltaCents: number): Ledger {
    const current = this.repository.require(tenantId, id);
    if (isLedgerTerminal(current)) {
      throw new IllegalTransitionError(`ledger ${id} is terminal`);
    }
    const amountCents = current.amountCents + deltaCents;
    if (amountCents < 0) {
      throw new ValidationError(`ledger ${id} cannot fall below zero`);
    }
    return this.repository.save(withLedgerAmount(current, amountCents));
  }

  label(tenantId: string, id: string, label: string): Ledger {
    return this.repository.save(withLedgerLabel(this.repository.require(tenantId, id), label));
  }

  cancelAllDrafts(tenantId: string): number {
    const drafts = this.repository.listByStatus(tenantId, "draft");
    for (const draft of drafts) {
      this.repository.save(applyLedgerTransition(draft, "cancelled"));
    }
    if (drafts.length > 0) {
      auditEvent("ledger.drafts_cancelled", { tenantId, count: drafts.length });
    }
    return drafts.length;
  }

  outstanding(tenantId: string): Ledger[] {
    return [
      ...this.repository.listByStatus(tenantId, "draft"),
      ...this.repository.listByStatus(tenantId, "active"),
    ];
  }

  page(tenantId: string, offset: number, limit: number): LedgerPage {
    return this.repository.page(tenantId, offset, limit);
  }

  summary(tenantId: string): LedgerSummary {
    const all = this.repository.all(tenantId);
    return {
      total: all.length,
      outstanding: this.outstanding(tenantId).length,
      amountCents: all.reduce((sum, item) => sum + item.amountCents, 0),
      byStatus: ledgerStatusCounts(all),
    };
  }

  discard(tenantId: string, id: string): void {
    const current = this.repository.require(tenantId, id);
    if (!isLedgerTerminal(current)) {
      throw new IllegalTransitionError(`ledger ${id} must reach a terminal status first`);
    }
    this.repository.remove(tenantId, id);
    auditEvent("ledger.discarded", { tenantId, id });
  }
}
