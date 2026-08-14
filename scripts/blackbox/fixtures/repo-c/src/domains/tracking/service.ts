import {
  Tracking,
  TrackingStatus,
  applyTrackingTransition,
  canTrackingTransition,
  isTrackingTerminal,
  newTracking,
  withTrackingAmount,
  withTrackingLabel,
  trackingStatusCounts,
} from "./model";
import { TrackingPage, TrackingRepository } from "./repository";
import { IllegalTransitionError, ValidationError } from "../../infra/errors";
import { auditEvent } from "../../infra/audit";

export interface TrackingSummary {
  readonly total: number;
  readonly outstanding: number;
  readonly amountCents: number;
  readonly byStatus: Record<TrackingStatus, number>;
}

/** Business rules for the tracking event lifecycle. */
export class TrackingService {
  constructor(private readonly repository: TrackingRepository) {}

  create(tenantId: string, id: string, reference: string, amountCents: number): Tracking {
    const draft = withTrackingAmount(newTracking(id, tenantId, reference), amountCents);
    const saved = this.repository.insert(draft);
    auditEvent("tracking.created", { tenantId, id });
    return saved;
  }

  get(tenantId: string, id: string): Tracking {
    return this.repository.require(tenantId, id);
  }

  transition(tenantId: string, id: string, next: TrackingStatus): Tracking {
    const current = this.repository.require(tenantId, id);
    if (isTrackingTerminal(current)) {
      throw new IllegalTransitionError(`tracking ${id} is terminal`);
    }
    if (!canTrackingTransition(current.status, next)) {
      throw new IllegalTransitionError(`tracking ${id}: ${current.status} -> ${next}`);
    }
    const saved = this.repository.save(applyTrackingTransition(current, next));
    auditEvent("tracking.transitioned", { tenantId, id, next });
    return saved;
  }

  adjustAmount(tenantId: string, id: string, deltaCents: number): Tracking {
    const current = this.repository.require(tenantId, id);
    if (isTrackingTerminal(current)) {
      throw new IllegalTransitionError(`tracking ${id} is terminal`);
    }
    const amountCents = current.amountCents + deltaCents;
    if (amountCents < 0) {
      throw new ValidationError(`tracking ${id} cannot fall below zero`);
    }
    return this.repository.save(withTrackingAmount(current, amountCents));
  }

  label(tenantId: string, id: string, label: string): Tracking {
    return this.repository.save(withTrackingLabel(this.repository.require(tenantId, id), label));
  }

  cancelAllDrafts(tenantId: string): number {
    const drafts = this.repository.listByStatus(tenantId, "draft");
    for (const draft of drafts) {
      this.repository.save(applyTrackingTransition(draft, "cancelled"));
    }
    if (drafts.length > 0) {
      auditEvent("tracking.drafts_cancelled", { tenantId, count: drafts.length });
    }
    return drafts.length;
  }

  outstanding(tenantId: string): Tracking[] {
    return [
      ...this.repository.listByStatus(tenantId, "draft"),
      ...this.repository.listByStatus(tenantId, "active"),
    ];
  }

  page(tenantId: string, offset: number, limit: number): TrackingPage {
    return this.repository.page(tenantId, offset, limit);
  }

  summary(tenantId: string): TrackingSummary {
    const all = this.repository.all(tenantId);
    return {
      total: all.length,
      outstanding: this.outstanding(tenantId).length,
      amountCents: all.reduce((sum, item) => sum + item.amountCents, 0),
      byStatus: trackingStatusCounts(all),
    };
  }

  discard(tenantId: string, id: string): void {
    const current = this.repository.require(tenantId, id);
    if (!isTrackingTerminal(current)) {
      throw new IllegalTransitionError(`tracking ${id} must reach a terminal status first`);
    }
    this.repository.remove(tenantId, id);
    auditEvent("tracking.discarded", { tenantId, id });
  }
}
