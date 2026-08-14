import {
  Ticket,
  TicketStatus,
  applyTicketTransition,
  canTicketTransition,
  isTicketTerminal,
  newTicket,
  withTicketAmount,
  withTicketLabel,
  ticketStatusCounts,
} from "./model";
import { TicketPage, TicketRepository } from "./repository";
import { IllegalTransitionError, ValidationError } from "../../infra/errors";
import { auditEvent } from "../../infra/audit";

export interface TicketSummary {
  readonly total: number;
  readonly outstanding: number;
  readonly amountCents: number;
  readonly byStatus: Record<TicketStatus, number>;
}

/** Business rules for the support ticket lifecycle. */
export class TicketService {
  constructor(private readonly repository: TicketRepository) {}

  create(tenantId: string, id: string, reference: string, amountCents: number): Ticket {
    const draft = withTicketAmount(newTicket(id, tenantId, reference), amountCents);
    const saved = this.repository.insert(draft);
    auditEvent("ticket.created", { tenantId, id });
    return saved;
  }

  get(tenantId: string, id: string): Ticket {
    return this.repository.require(tenantId, id);
  }

  transition(tenantId: string, id: string, next: TicketStatus): Ticket {
    const current = this.repository.require(tenantId, id);
    if (isTicketTerminal(current)) {
      throw new IllegalTransitionError(`ticket ${id} is terminal`);
    }
    if (!canTicketTransition(current.status, next)) {
      throw new IllegalTransitionError(`ticket ${id}: ${current.status} -> ${next}`);
    }
    const saved = this.repository.save(applyTicketTransition(current, next));
    auditEvent("ticket.transitioned", { tenantId, id, next });
    return saved;
  }

  adjustAmount(tenantId: string, id: string, deltaCents: number): Ticket {
    const current = this.repository.require(tenantId, id);
    if (isTicketTerminal(current)) {
      throw new IllegalTransitionError(`ticket ${id} is terminal`);
    }
    const amountCents = current.amountCents + deltaCents;
    if (amountCents < 0) {
      throw new ValidationError(`ticket ${id} cannot fall below zero`);
    }
    return this.repository.save(withTicketAmount(current, amountCents));
  }

  label(tenantId: string, id: string, label: string): Ticket {
    return this.repository.save(withTicketLabel(this.repository.require(tenantId, id), label));
  }

  cancelAllDrafts(tenantId: string): number {
    const drafts = this.repository.listByStatus(tenantId, "draft");
    for (const draft of drafts) {
      this.repository.save(applyTicketTransition(draft, "cancelled"));
    }
    if (drafts.length > 0) {
      auditEvent("ticket.drafts_cancelled", { tenantId, count: drafts.length });
    }
    return drafts.length;
  }

  outstanding(tenantId: string): Ticket[] {
    return [
      ...this.repository.listByStatus(tenantId, "draft"),
      ...this.repository.listByStatus(tenantId, "active"),
    ];
  }

  page(tenantId: string, offset: number, limit: number): TicketPage {
    return this.repository.page(tenantId, offset, limit);
  }

  summary(tenantId: string): TicketSummary {
    const all = this.repository.all(tenantId);
    return {
      total: all.length,
      outstanding: this.outstanding(tenantId).length,
      amountCents: all.reduce((sum, item) => sum + item.amountCents, 0),
      byStatus: ticketStatusCounts(all),
    };
  }

  discard(tenantId: string, id: string): void {
    const current = this.repository.require(tenantId, id);
    if (!isTicketTerminal(current)) {
      throw new IllegalTransitionError(`ticket ${id} must reach a terminal status first`);
    }
    this.repository.remove(tenantId, id);
    auditEvent("ticket.discarded", { tenantId, id });
  }
}
