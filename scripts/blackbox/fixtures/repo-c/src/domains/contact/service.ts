import {
  Contact,
  ContactStatus,
  applyContactTransition,
  canContactTransition,
  isContactTerminal,
  newContact,
  withContactAmount,
  withContactLabel,
  contactStatusCounts,
} from "./model";
import { ContactPage, ContactRepository } from "./repository";
import { IllegalTransitionError, ValidationError } from "../../infra/errors";
import { auditEvent } from "../../infra/audit";

export interface ContactSummary {
  readonly total: number;
  readonly outstanding: number;
  readonly amountCents: number;
  readonly byStatus: Record<ContactStatus, number>;
}

/** Business rules for the contact person lifecycle. */
export class ContactService {
  constructor(private readonly repository: ContactRepository) {}

  create(tenantId: string, id: string, reference: string, amountCents: number): Contact {
    const draft = withContactAmount(newContact(id, tenantId, reference), amountCents);
    const saved = this.repository.insert(draft);
    auditEvent("contact.created", { tenantId, id });
    return saved;
  }

  get(tenantId: string, id: string): Contact {
    return this.repository.require(tenantId, id);
  }

  transition(tenantId: string, id: string, next: ContactStatus): Contact {
    const current = this.repository.require(tenantId, id);
    if (isContactTerminal(current)) {
      throw new IllegalTransitionError(`contact ${id} is terminal`);
    }
    if (!canContactTransition(current.status, next)) {
      throw new IllegalTransitionError(`contact ${id}: ${current.status} -> ${next}`);
    }
    const saved = this.repository.save(applyContactTransition(current, next));
    auditEvent("contact.transitioned", { tenantId, id, next });
    return saved;
  }

  adjustAmount(tenantId: string, id: string, deltaCents: number): Contact {
    const current = this.repository.require(tenantId, id);
    if (isContactTerminal(current)) {
      throw new IllegalTransitionError(`contact ${id} is terminal`);
    }
    const amountCents = current.amountCents + deltaCents;
    if (amountCents < 0) {
      throw new ValidationError(`contact ${id} cannot fall below zero`);
    }
    return this.repository.save(withContactAmount(current, amountCents));
  }

  label(tenantId: string, id: string, label: string): Contact {
    return this.repository.save(withContactLabel(this.repository.require(tenantId, id), label));
  }

  cancelAllDrafts(tenantId: string): number {
    const drafts = this.repository.listByStatus(tenantId, "draft");
    for (const draft of drafts) {
      this.repository.save(applyContactTransition(draft, "cancelled"));
    }
    if (drafts.length > 0) {
      auditEvent("contact.drafts_cancelled", { tenantId, count: drafts.length });
    }
    return drafts.length;
  }

  outstanding(tenantId: string): Contact[] {
    return [
      ...this.repository.listByStatus(tenantId, "draft"),
      ...this.repository.listByStatus(tenantId, "active"),
    ];
  }

  page(tenantId: string, offset: number, limit: number): ContactPage {
    return this.repository.page(tenantId, offset, limit);
  }

  summary(tenantId: string): ContactSummary {
    const all = this.repository.all(tenantId);
    return {
      total: all.length,
      outstanding: this.outstanding(tenantId).length,
      amountCents: all.reduce((sum, item) => sum + item.amountCents, 0),
      byStatus: contactStatusCounts(all),
    };
  }

  discard(tenantId: string, id: string): void {
    const current = this.repository.require(tenantId, id);
    if (!isContactTerminal(current)) {
      throw new IllegalTransitionError(`contact ${id} must reach a terminal status first`);
    }
    this.repository.remove(tenantId, id);
    auditEvent("contact.discarded", { tenantId, id });
  }
}
