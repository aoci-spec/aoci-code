import {
  Message,
  MessageStatus,
  applyMessageTransition,
  canMessageTransition,
  isMessageTerminal,
  newMessage,
  withMessageAmount,
  withMessageLabel,
  messageStatusCounts,
} from "./model";
import { MessagePage, MessageRepository } from "./repository";
import { IllegalTransitionError, ValidationError } from "../../infra/errors";
import { auditEvent } from "../../infra/audit";

export interface MessageSummary {
  readonly total: number;
  readonly outstanding: number;
  readonly amountCents: number;
  readonly byStatus: Record<MessageStatus, number>;
}

/** Business rules for the customer message lifecycle. */
export class MessageService {
  constructor(private readonly repository: MessageRepository) {}

  create(tenantId: string, id: string, reference: string, amountCents: number): Message {
    const draft = withMessageAmount(newMessage(id, tenantId, reference), amountCents);
    const saved = this.repository.insert(draft);
    auditEvent("message.created", { tenantId, id });
    return saved;
  }

  get(tenantId: string, id: string): Message {
    return this.repository.require(tenantId, id);
  }

  transition(tenantId: string, id: string, next: MessageStatus): Message {
    const current = this.repository.require(tenantId, id);
    if (isMessageTerminal(current)) {
      throw new IllegalTransitionError(`message ${id} is terminal`);
    }
    if (!canMessageTransition(current.status, next)) {
      throw new IllegalTransitionError(`message ${id}: ${current.status} -> ${next}`);
    }
    const saved = this.repository.save(applyMessageTransition(current, next));
    auditEvent("message.transitioned", { tenantId, id, next });
    return saved;
  }

  adjustAmount(tenantId: string, id: string, deltaCents: number): Message {
    const current = this.repository.require(tenantId, id);
    if (isMessageTerminal(current)) {
      throw new IllegalTransitionError(`message ${id} is terminal`);
    }
    const amountCents = current.amountCents + deltaCents;
    if (amountCents < 0) {
      throw new ValidationError(`message ${id} cannot fall below zero`);
    }
    return this.repository.save(withMessageAmount(current, amountCents));
  }

  label(tenantId: string, id: string, label: string): Message {
    return this.repository.save(withMessageLabel(this.repository.require(tenantId, id), label));
  }

  cancelAllDrafts(tenantId: string): number {
    const drafts = this.repository.listByStatus(tenantId, "draft");
    for (const draft of drafts) {
      this.repository.save(applyMessageTransition(draft, "cancelled"));
    }
    if (drafts.length > 0) {
      auditEvent("message.drafts_cancelled", { tenantId, count: drafts.length });
    }
    return drafts.length;
  }

  outstanding(tenantId: string): Message[] {
    return [
      ...this.repository.listByStatus(tenantId, "draft"),
      ...this.repository.listByStatus(tenantId, "active"),
    ];
  }

  page(tenantId: string, offset: number, limit: number): MessagePage {
    return this.repository.page(tenantId, offset, limit);
  }

  summary(tenantId: string): MessageSummary {
    const all = this.repository.all(tenantId);
    return {
      total: all.length,
      outstanding: this.outstanding(tenantId).length,
      amountCents: all.reduce((sum, item) => sum + item.amountCents, 0),
      byStatus: messageStatusCounts(all),
    };
  }

  discard(tenantId: string, id: string): void {
    const current = this.repository.require(tenantId, id);
    if (!isMessageTerminal(current)) {
      throw new IllegalTransitionError(`message ${id} must reach a terminal status first`);
    }
    this.repository.remove(tenantId, id);
    auditEvent("message.discarded", { tenantId, id });
  }
}
