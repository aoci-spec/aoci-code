import {
  Currency,
  CurrencyStatus,
  applyCurrencyTransition,
  canCurrencyTransition,
  isCurrencyTerminal,
  newCurrency,
  withCurrencyAmount,
  withCurrencyLabel,
  currencyStatusCounts,
} from "./model";
import { CurrencyPage, CurrencyRepository } from "./repository";
import { IllegalTransitionError, ValidationError } from "../../infra/errors";
import { auditEvent } from "../../infra/audit";

export interface CurrencySummary {
  readonly total: number;
  readonly outstanding: number;
  readonly amountCents: number;
  readonly byStatus: Record<CurrencyStatus, number>;
}

/** Business rules for the currency rate lifecycle. */
export class CurrencyService {
  constructor(private readonly repository: CurrencyRepository) {}

  create(tenantId: string, id: string, reference: string, amountCents: number): Currency {
    const draft = withCurrencyAmount(newCurrency(id, tenantId, reference), amountCents);
    const saved = this.repository.insert(draft);
    auditEvent("currency.created", { tenantId, id });
    return saved;
  }

  get(tenantId: string, id: string): Currency {
    return this.repository.require(tenantId, id);
  }

  transition(tenantId: string, id: string, next: CurrencyStatus): Currency {
    const current = this.repository.require(tenantId, id);
    if (isCurrencyTerminal(current)) {
      throw new IllegalTransitionError(`currency ${id} is terminal`);
    }
    if (!canCurrencyTransition(current.status, next)) {
      throw new IllegalTransitionError(`currency ${id}: ${current.status} -> ${next}`);
    }
    const saved = this.repository.save(applyCurrencyTransition(current, next));
    auditEvent("currency.transitioned", { tenantId, id, next });
    return saved;
  }

  adjustAmount(tenantId: string, id: string, deltaCents: number): Currency {
    const current = this.repository.require(tenantId, id);
    if (isCurrencyTerminal(current)) {
      throw new IllegalTransitionError(`currency ${id} is terminal`);
    }
    const amountCents = current.amountCents + deltaCents;
    if (amountCents < 0) {
      throw new ValidationError(`currency ${id} cannot fall below zero`);
    }
    return this.repository.save(withCurrencyAmount(current, amountCents));
  }

  label(tenantId: string, id: string, label: string): Currency {
    return this.repository.save(withCurrencyLabel(this.repository.require(tenantId, id), label));
  }

  cancelAllDrafts(tenantId: string): number {
    const drafts = this.repository.listByStatus(tenantId, "draft");
    for (const draft of drafts) {
      this.repository.save(applyCurrencyTransition(draft, "cancelled"));
    }
    if (drafts.length > 0) {
      auditEvent("currency.drafts_cancelled", { tenantId, count: drafts.length });
    }
    return drafts.length;
  }

  outstanding(tenantId: string): Currency[] {
    return [
      ...this.repository.listByStatus(tenantId, "draft"),
      ...this.repository.listByStatus(tenantId, "active"),
    ];
  }

  page(tenantId: string, offset: number, limit: number): CurrencyPage {
    return this.repository.page(tenantId, offset, limit);
  }

  summary(tenantId: string): CurrencySummary {
    const all = this.repository.all(tenantId);
    return {
      total: all.length,
      outstanding: this.outstanding(tenantId).length,
      amountCents: all.reduce((sum, item) => sum + item.amountCents, 0),
      byStatus: currencyStatusCounts(all),
    };
  }

  discard(tenantId: string, id: string): void {
    const current = this.repository.require(tenantId, id);
    if (!isCurrencyTerminal(current)) {
      throw new IllegalTransitionError(`currency ${id} must reach a terminal status first`);
    }
    this.repository.remove(tenantId, id);
    auditEvent("currency.discarded", { tenantId, id });
  }
}
