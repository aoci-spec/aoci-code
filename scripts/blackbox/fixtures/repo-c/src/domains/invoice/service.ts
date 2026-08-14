import {
  Invoice,
  InvoiceStatus,
  applyInvoiceTransition,
  canInvoiceTransition,
  isInvoiceTerminal,
  newInvoice,
  withInvoiceAmount,
  withInvoiceLabel,
  invoiceStatusCounts,
} from "./model";
import { InvoicePage, InvoiceRepository } from "./repository";
import { IllegalTransitionError, ValidationError } from "../../infra/errors";
import { auditEvent } from "../../infra/audit";

export interface InvoiceSummary {
  readonly total: number;
  readonly outstanding: number;
  readonly amountCents: number;
  readonly byStatus: Record<InvoiceStatus, number>;
}

/** Business rules for the billing invoice lifecycle. */
export class InvoiceService {
  constructor(private readonly repository: InvoiceRepository) {}

  create(tenantId: string, id: string, reference: string, amountCents: number): Invoice {
    const draft = withInvoiceAmount(newInvoice(id, tenantId, reference), amountCents);
    const saved = this.repository.insert(draft);
    auditEvent("invoice.created", { tenantId, id });
    return saved;
  }

  get(tenantId: string, id: string): Invoice {
    return this.repository.require(tenantId, id);
  }

  transition(tenantId: string, id: string, next: InvoiceStatus): Invoice {
    const current = this.repository.require(tenantId, id);
    if (isInvoiceTerminal(current)) {
      throw new IllegalTransitionError(`invoice ${id} is terminal`);
    }
    if (!canInvoiceTransition(current.status, next)) {
      throw new IllegalTransitionError(`invoice ${id}: ${current.status} -> ${next}`);
    }
    const saved = this.repository.save(applyInvoiceTransition(current, next));
    auditEvent("invoice.transitioned", { tenantId, id, next });
    return saved;
  }

  adjustAmount(tenantId: string, id: string, deltaCents: number): Invoice {
    const current = this.repository.require(tenantId, id);
    if (isInvoiceTerminal(current)) {
      throw new IllegalTransitionError(`invoice ${id} is terminal`);
    }
    const amountCents = current.amountCents + deltaCents;
    if (amountCents < 0) {
      throw new ValidationError(`invoice ${id} cannot fall below zero`);
    }
    return this.repository.save(withInvoiceAmount(current, amountCents));
  }

  label(tenantId: string, id: string, label: string): Invoice {
    return this.repository.save(withInvoiceLabel(this.repository.require(tenantId, id), label));
  }

  cancelAllDrafts(tenantId: string): number {
    const drafts = this.repository.listByStatus(tenantId, "draft");
    for (const draft of drafts) {
      this.repository.save(applyInvoiceTransition(draft, "cancelled"));
    }
    if (drafts.length > 0) {
      auditEvent("invoice.drafts_cancelled", { tenantId, count: drafts.length });
    }
    return drafts.length;
  }

  outstanding(tenantId: string): Invoice[] {
    return [
      ...this.repository.listByStatus(tenantId, "draft"),
      ...this.repository.listByStatus(tenantId, "active"),
    ];
  }

  page(tenantId: string, offset: number, limit: number): InvoicePage {
    return this.repository.page(tenantId, offset, limit);
  }

  summary(tenantId: string): InvoiceSummary {
    const all = this.repository.all(tenantId);
    return {
      total: all.length,
      outstanding: this.outstanding(tenantId).length,
      amountCents: all.reduce((sum, item) => sum + item.amountCents, 0),
      byStatus: invoiceStatusCounts(all),
    };
  }

  discard(tenantId: string, id: string): void {
    const current = this.repository.require(tenantId, id);
    if (!isInvoiceTerminal(current)) {
      throw new IllegalTransitionError(`invoice ${id} must reach a terminal status first`);
    }
    this.repository.remove(tenantId, id);
    auditEvent("invoice.discarded", { tenantId, id });
  }
}
