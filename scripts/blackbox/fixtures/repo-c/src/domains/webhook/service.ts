import {
  Webhook,
  WebhookStatus,
  applyWebhookTransition,
  canWebhookTransition,
  isWebhookTerminal,
  newWebhook,
  withWebhookAmount,
  withWebhookLabel,
  webhookStatusCounts,
} from "./model";
import { WebhookPage, WebhookRepository } from "./repository";
import { IllegalTransitionError, ValidationError } from "../../infra/errors";
import { auditEvent } from "../../infra/audit";

export interface WebhookSummary {
  readonly total: number;
  readonly outstanding: number;
  readonly amountCents: number;
  readonly byStatus: Record<WebhookStatus, number>;
}

/** Business rules for the outbound webhook lifecycle. */
export class WebhookService {
  constructor(private readonly repository: WebhookRepository) {}

  create(tenantId: string, id: string, reference: string, amountCents: number): Webhook {
    const draft = withWebhookAmount(newWebhook(id, tenantId, reference), amountCents);
    const saved = this.repository.insert(draft);
    auditEvent("webhook.created", { tenantId, id });
    return saved;
  }

  get(tenantId: string, id: string): Webhook {
    return this.repository.require(tenantId, id);
  }

  transition(tenantId: string, id: string, next: WebhookStatus): Webhook {
    const current = this.repository.require(tenantId, id);
    if (isWebhookTerminal(current)) {
      throw new IllegalTransitionError(`webhook ${id} is terminal`);
    }
    if (!canWebhookTransition(current.status, next)) {
      throw new IllegalTransitionError(`webhook ${id}: ${current.status} -> ${next}`);
    }
    const saved = this.repository.save(applyWebhookTransition(current, next));
    auditEvent("webhook.transitioned", { tenantId, id, next });
    return saved;
  }

  adjustAmount(tenantId: string, id: string, deltaCents: number): Webhook {
    const current = this.repository.require(tenantId, id);
    if (isWebhookTerminal(current)) {
      throw new IllegalTransitionError(`webhook ${id} is terminal`);
    }
    const amountCents = current.amountCents + deltaCents;
    if (amountCents < 0) {
      throw new ValidationError(`webhook ${id} cannot fall below zero`);
    }
    return this.repository.save(withWebhookAmount(current, amountCents));
  }

  label(tenantId: string, id: string, label: string): Webhook {
    return this.repository.save(withWebhookLabel(this.repository.require(tenantId, id), label));
  }

  cancelAllDrafts(tenantId: string): number {
    const drafts = this.repository.listByStatus(tenantId, "draft");
    for (const draft of drafts) {
      this.repository.save(applyWebhookTransition(draft, "cancelled"));
    }
    if (drafts.length > 0) {
      auditEvent("webhook.drafts_cancelled", { tenantId, count: drafts.length });
    }
    return drafts.length;
  }

  outstanding(tenantId: string): Webhook[] {
    return [
      ...this.repository.listByStatus(tenantId, "draft"),
      ...this.repository.listByStatus(tenantId, "active"),
    ];
  }

  page(tenantId: string, offset: number, limit: number): WebhookPage {
    return this.repository.page(tenantId, offset, limit);
  }

  summary(tenantId: string): WebhookSummary {
    const all = this.repository.all(tenantId);
    return {
      total: all.length,
      outstanding: this.outstanding(tenantId).length,
      amountCents: all.reduce((sum, item) => sum + item.amountCents, 0),
      byStatus: webhookStatusCounts(all),
    };
  }

  discard(tenantId: string, id: string): void {
    const current = this.repository.require(tenantId, id);
    if (!isWebhookTerminal(current)) {
      throw new IllegalTransitionError(`webhook ${id} must reach a terminal status first`);
    }
    this.repository.remove(tenantId, id);
    auditEvent("webhook.discarded", { tenantId, id });
  }
}
