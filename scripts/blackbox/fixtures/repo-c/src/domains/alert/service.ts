import {
  Alert,
  AlertStatus,
  applyAlertTransition,
  canAlertTransition,
  isAlertTerminal,
  newAlert,
  withAlertAmount,
  withAlertLabel,
  alertStatusCounts,
} from "./model";
import { AlertPage, AlertRepository } from "./repository";
import { IllegalTransitionError, ValidationError } from "../../infra/errors";
import { auditEvent } from "../../infra/audit";

export interface AlertSummary {
  readonly total: number;
  readonly outstanding: number;
  readonly amountCents: number;
  readonly byStatus: Record<AlertStatus, number>;
}

/** Business rules for the operational alert lifecycle. */
export class AlertService {
  constructor(private readonly repository: AlertRepository) {}

  create(tenantId: string, id: string, reference: string, amountCents: number): Alert {
    const draft = withAlertAmount(newAlert(id, tenantId, reference), amountCents);
    const saved = this.repository.insert(draft);
    auditEvent("alert.created", { tenantId, id });
    return saved;
  }

  get(tenantId: string, id: string): Alert {
    return this.repository.require(tenantId, id);
  }

  transition(tenantId: string, id: string, next: AlertStatus): Alert {
    const current = this.repository.require(tenantId, id);
    if (isAlertTerminal(current)) {
      throw new IllegalTransitionError(`alert ${id} is terminal`);
    }
    if (!canAlertTransition(current.status, next)) {
      throw new IllegalTransitionError(`alert ${id}: ${current.status} -> ${next}`);
    }
    const saved = this.repository.save(applyAlertTransition(current, next));
    auditEvent("alert.transitioned", { tenantId, id, next });
    return saved;
  }

  adjustAmount(tenantId: string, id: string, deltaCents: number): Alert {
    const current = this.repository.require(tenantId, id);
    if (isAlertTerminal(current)) {
      throw new IllegalTransitionError(`alert ${id} is terminal`);
    }
    const amountCents = current.amountCents + deltaCents;
    if (amountCents < 0) {
      throw new ValidationError(`alert ${id} cannot fall below zero`);
    }
    return this.repository.save(withAlertAmount(current, amountCents));
  }

  label(tenantId: string, id: string, label: string): Alert {
    return this.repository.save(withAlertLabel(this.repository.require(tenantId, id), label));
  }

  cancelAllDrafts(tenantId: string): number {
    const drafts = this.repository.listByStatus(tenantId, "draft");
    for (const draft of drafts) {
      this.repository.save(applyAlertTransition(draft, "cancelled"));
    }
    if (drafts.length > 0) {
      auditEvent("alert.drafts_cancelled", { tenantId, count: drafts.length });
    }
    return drafts.length;
  }

  outstanding(tenantId: string): Alert[] {
    return [
      ...this.repository.listByStatus(tenantId, "draft"),
      ...this.repository.listByStatus(tenantId, "active"),
    ];
  }

  page(tenantId: string, offset: number, limit: number): AlertPage {
    return this.repository.page(tenantId, offset, limit);
  }

  summary(tenantId: string): AlertSummary {
    const all = this.repository.all(tenantId);
    return {
      total: all.length,
      outstanding: this.outstanding(tenantId).length,
      amountCents: all.reduce((sum, item) => sum + item.amountCents, 0),
      byStatus: alertStatusCounts(all),
    };
  }

  discard(tenantId: string, id: string): void {
    const current = this.repository.require(tenantId, id);
    if (!isAlertTerminal(current)) {
      throw new IllegalTransitionError(`alert ${id} must reach a terminal status first`);
    }
    this.repository.remove(tenantId, id);
    auditEvent("alert.discarded", { tenantId, id });
  }
}
