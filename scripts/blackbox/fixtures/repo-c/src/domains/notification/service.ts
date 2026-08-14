import {
  Notification,
  NotificationStatus,
  applyNotificationTransition,
  canNotificationTransition,
  isNotificationTerminal,
  newNotification,
  withNotificationAmount,
  withNotificationLabel,
  notificationStatusCounts,
} from "./model";
import { NotificationPage, NotificationRepository } from "./repository";
import { IllegalTransitionError, ValidationError } from "../../infra/errors";
import { auditEvent } from "../../infra/audit";

export interface NotificationSummary {
  readonly total: number;
  readonly outstanding: number;
  readonly amountCents: number;
  readonly byStatus: Record<NotificationStatus, number>;
}

/** Business rules for the outbound notification lifecycle. */
export class NotificationService {
  constructor(private readonly repository: NotificationRepository) {}

  create(tenantId: string, id: string, reference: string, amountCents: number): Notification {
    const draft = withNotificationAmount(newNotification(id, tenantId, reference), amountCents);
    const saved = this.repository.insert(draft);
    auditEvent("notification.created", { tenantId, id });
    return saved;
  }

  get(tenantId: string, id: string): Notification {
    return this.repository.require(tenantId, id);
  }

  transition(tenantId: string, id: string, next: NotificationStatus): Notification {
    const current = this.repository.require(tenantId, id);
    if (isNotificationTerminal(current)) {
      throw new IllegalTransitionError(`notification ${id} is terminal`);
    }
    if (!canNotificationTransition(current.status, next)) {
      throw new IllegalTransitionError(`notification ${id}: ${current.status} -> ${next}`);
    }
    const saved = this.repository.save(applyNotificationTransition(current, next));
    auditEvent("notification.transitioned", { tenantId, id, next });
    return saved;
  }

  adjustAmount(tenantId: string, id: string, deltaCents: number): Notification {
    const current = this.repository.require(tenantId, id);
    if (isNotificationTerminal(current)) {
      throw new IllegalTransitionError(`notification ${id} is terminal`);
    }
    const amountCents = current.amountCents + deltaCents;
    if (amountCents < 0) {
      throw new ValidationError(`notification ${id} cannot fall below zero`);
    }
    return this.repository.save(withNotificationAmount(current, amountCents));
  }

  label(tenantId: string, id: string, label: string): Notification {
    return this.repository.save(withNotificationLabel(this.repository.require(tenantId, id), label));
  }

  cancelAllDrafts(tenantId: string): number {
    const drafts = this.repository.listByStatus(tenantId, "draft");
    for (const draft of drafts) {
      this.repository.save(applyNotificationTransition(draft, "cancelled"));
    }
    if (drafts.length > 0) {
      auditEvent("notification.drafts_cancelled", { tenantId, count: drafts.length });
    }
    return drafts.length;
  }

  outstanding(tenantId: string): Notification[] {
    return [
      ...this.repository.listByStatus(tenantId, "draft"),
      ...this.repository.listByStatus(tenantId, "active"),
    ];
  }

  page(tenantId: string, offset: number, limit: number): NotificationPage {
    return this.repository.page(tenantId, offset, limit);
  }

  summary(tenantId: string): NotificationSummary {
    const all = this.repository.all(tenantId);
    return {
      total: all.length,
      outstanding: this.outstanding(tenantId).length,
      amountCents: all.reduce((sum, item) => sum + item.amountCents, 0),
      byStatus: notificationStatusCounts(all),
    };
  }

  discard(tenantId: string, id: string): void {
    const current = this.repository.require(tenantId, id);
    if (!isNotificationTerminal(current)) {
      throw new IllegalTransitionError(`notification ${id} must reach a terminal status first`);
    }
    this.repository.remove(tenantId, id);
    auditEvent("notification.discarded", { tenantId, id });
  }
}
