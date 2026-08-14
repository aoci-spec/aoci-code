import {
  Setting,
  SettingStatus,
  applySettingTransition,
  canSettingTransition,
  isSettingTerminal,
  newSetting,
  withSettingAmount,
  withSettingLabel,
  settingStatusCounts,
} from "./model";
import { SettingPage, SettingRepository } from "./repository";
import { IllegalTransitionError, ValidationError } from "../../infra/errors";
import { auditEvent } from "../../infra/audit";

export interface SettingSummary {
  readonly total: number;
  readonly outstanding: number;
  readonly amountCents: number;
  readonly byStatus: Record<SettingStatus, number>;
}

/** Business rules for the tenant setting lifecycle. */
export class SettingService {
  constructor(private readonly repository: SettingRepository) {}

  create(tenantId: string, id: string, reference: string, amountCents: number): Setting {
    const draft = withSettingAmount(newSetting(id, tenantId, reference), amountCents);
    const saved = this.repository.insert(draft);
    auditEvent("setting.created", { tenantId, id });
    return saved;
  }

  get(tenantId: string, id: string): Setting {
    return this.repository.require(tenantId, id);
  }

  transition(tenantId: string, id: string, next: SettingStatus): Setting {
    const current = this.repository.require(tenantId, id);
    if (isSettingTerminal(current)) {
      throw new IllegalTransitionError(`setting ${id} is terminal`);
    }
    if (!canSettingTransition(current.status, next)) {
      throw new IllegalTransitionError(`setting ${id}: ${current.status} -> ${next}`);
    }
    const saved = this.repository.save(applySettingTransition(current, next));
    auditEvent("setting.transitioned", { tenantId, id, next });
    return saved;
  }

  adjustAmount(tenantId: string, id: string, deltaCents: number): Setting {
    const current = this.repository.require(tenantId, id);
    if (isSettingTerminal(current)) {
      throw new IllegalTransitionError(`setting ${id} is terminal`);
    }
    const amountCents = current.amountCents + deltaCents;
    if (amountCents < 0) {
      throw new ValidationError(`setting ${id} cannot fall below zero`);
    }
    return this.repository.save(withSettingAmount(current, amountCents));
  }

  label(tenantId: string, id: string, label: string): Setting {
    return this.repository.save(withSettingLabel(this.repository.require(tenantId, id), label));
  }

  cancelAllDrafts(tenantId: string): number {
    const drafts = this.repository.listByStatus(tenantId, "draft");
    for (const draft of drafts) {
      this.repository.save(applySettingTransition(draft, "cancelled"));
    }
    if (drafts.length > 0) {
      auditEvent("setting.drafts_cancelled", { tenantId, count: drafts.length });
    }
    return drafts.length;
  }

  outstanding(tenantId: string): Setting[] {
    return [
      ...this.repository.listByStatus(tenantId, "draft"),
      ...this.repository.listByStatus(tenantId, "active"),
    ];
  }

  page(tenantId: string, offset: number, limit: number): SettingPage {
    return this.repository.page(tenantId, offset, limit);
  }

  summary(tenantId: string): SettingSummary {
    const all = this.repository.all(tenantId);
    return {
      total: all.length,
      outstanding: this.outstanding(tenantId).length,
      amountCents: all.reduce((sum, item) => sum + item.amountCents, 0),
      byStatus: settingStatusCounts(all),
    };
  }

  discard(tenantId: string, id: string): void {
    const current = this.repository.require(tenantId, id);
    if (!isSettingTerminal(current)) {
      throw new IllegalTransitionError(`setting ${id} must reach a terminal status first`);
    }
    this.repository.remove(tenantId, id);
    auditEvent("setting.discarded", { tenantId, id });
  }
}
