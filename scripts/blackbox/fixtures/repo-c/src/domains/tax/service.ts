import {
  Tax,
  TaxStatus,
  applyTaxTransition,
  canTaxTransition,
  isTaxTerminal,
  newTax,
  withTaxAmount,
  withTaxLabel,
  taxStatusCounts,
} from "./model";
import { TaxPage, TaxRepository } from "./repository";
import { IllegalTransitionError, ValidationError } from "../../infra/errors";
import { auditEvent } from "../../infra/audit";

export interface TaxSummary {
  readonly total: number;
  readonly outstanding: number;
  readonly amountCents: number;
  readonly byStatus: Record<TaxStatus, number>;
}

/** Business rules for the tax determination lifecycle. */
export class TaxService {
  constructor(private readonly repository: TaxRepository) {}

  create(tenantId: string, id: string, reference: string, amountCents: number): Tax {
    const draft = withTaxAmount(newTax(id, tenantId, reference), amountCents);
    const saved = this.repository.insert(draft);
    auditEvent("tax.created", { tenantId, id });
    return saved;
  }

  get(tenantId: string, id: string): Tax {
    return this.repository.require(tenantId, id);
  }

  transition(tenantId: string, id: string, next: TaxStatus): Tax {
    const current = this.repository.require(tenantId, id);
    if (isTaxTerminal(current)) {
      throw new IllegalTransitionError(`tax ${id} is terminal`);
    }
    if (!canTaxTransition(current.status, next)) {
      throw new IllegalTransitionError(`tax ${id}: ${current.status} -> ${next}`);
    }
    const saved = this.repository.save(applyTaxTransition(current, next));
    auditEvent("tax.transitioned", { tenantId, id, next });
    return saved;
  }

  adjustAmount(tenantId: string, id: string, deltaCents: number): Tax {
    const current = this.repository.require(tenantId, id);
    if (isTaxTerminal(current)) {
      throw new IllegalTransitionError(`tax ${id} is terminal`);
    }
    const amountCents = current.amountCents + deltaCents;
    if (amountCents < 0) {
      throw new ValidationError(`tax ${id} cannot fall below zero`);
    }
    return this.repository.save(withTaxAmount(current, amountCents));
  }

  label(tenantId: string, id: string, label: string): Tax {
    return this.repository.save(withTaxLabel(this.repository.require(tenantId, id), label));
  }

  cancelAllDrafts(tenantId: string): number {
    const drafts = this.repository.listByStatus(tenantId, "draft");
    for (const draft of drafts) {
      this.repository.save(applyTaxTransition(draft, "cancelled"));
    }
    if (drafts.length > 0) {
      auditEvent("tax.drafts_cancelled", { tenantId, count: drafts.length });
    }
    return drafts.length;
  }

  outstanding(tenantId: string): Tax[] {
    return [
      ...this.repository.listByStatus(tenantId, "draft"),
      ...this.repository.listByStatus(tenantId, "active"),
    ];
  }

  page(tenantId: string, offset: number, limit: number): TaxPage {
    return this.repository.page(tenantId, offset, limit);
  }

  summary(tenantId: string): TaxSummary {
    const all = this.repository.all(tenantId);
    return {
      total: all.length,
      outstanding: this.outstanding(tenantId).length,
      amountCents: all.reduce((sum, item) => sum + item.amountCents, 0),
      byStatus: taxStatusCounts(all),
    };
  }

  discard(tenantId: string, id: string): void {
    const current = this.repository.require(tenantId, id);
    if (!isTaxTerminal(current)) {
      throw new IllegalTransitionError(`tax ${id} must reach a terminal status first`);
    }
    this.repository.remove(tenantId, id);
    auditEvent("tax.discarded", { tenantId, id });
  }
}
