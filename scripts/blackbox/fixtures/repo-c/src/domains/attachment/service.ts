import {
  Attachment,
  AttachmentStatus,
  applyAttachmentTransition,
  canAttachmentTransition,
  isAttachmentTerminal,
  newAttachment,
  withAttachmentAmount,
  withAttachmentLabel,
  attachmentStatusCounts,
} from "./model";
import { AttachmentPage, AttachmentRepository } from "./repository";
import { IllegalTransitionError, ValidationError } from "../../infra/errors";
import { auditEvent } from "../../infra/audit";

export interface AttachmentSummary {
  readonly total: number;
  readonly outstanding: number;
  readonly amountCents: number;
  readonly byStatus: Record<AttachmentStatus, number>;
}

/** Business rules for the file attachment lifecycle. */
export class AttachmentService {
  constructor(private readonly repository: AttachmentRepository) {}

  create(tenantId: string, id: string, reference: string, amountCents: number): Attachment {
    const draft = withAttachmentAmount(newAttachment(id, tenantId, reference), amountCents);
    const saved = this.repository.insert(draft);
    auditEvent("attachment.created", { tenantId, id });
    return saved;
  }

  get(tenantId: string, id: string): Attachment {
    return this.repository.require(tenantId, id);
  }

  transition(tenantId: string, id: string, next: AttachmentStatus): Attachment {
    const current = this.repository.require(tenantId, id);
    if (isAttachmentTerminal(current)) {
      throw new IllegalTransitionError(`attachment ${id} is terminal`);
    }
    if (!canAttachmentTransition(current.status, next)) {
      throw new IllegalTransitionError(`attachment ${id}: ${current.status} -> ${next}`);
    }
    const saved = this.repository.save(applyAttachmentTransition(current, next));
    auditEvent("attachment.transitioned", { tenantId, id, next });
    return saved;
  }

  adjustAmount(tenantId: string, id: string, deltaCents: number): Attachment {
    const current = this.repository.require(tenantId, id);
    if (isAttachmentTerminal(current)) {
      throw new IllegalTransitionError(`attachment ${id} is terminal`);
    }
    const amountCents = current.amountCents + deltaCents;
    if (amountCents < 0) {
      throw new ValidationError(`attachment ${id} cannot fall below zero`);
    }
    return this.repository.save(withAttachmentAmount(current, amountCents));
  }

  label(tenantId: string, id: string, label: string): Attachment {
    return this.repository.save(withAttachmentLabel(this.repository.require(tenantId, id), label));
  }

  cancelAllDrafts(tenantId: string): number {
    const drafts = this.repository.listByStatus(tenantId, "draft");
    for (const draft of drafts) {
      this.repository.save(applyAttachmentTransition(draft, "cancelled"));
    }
    if (drafts.length > 0) {
      auditEvent("attachment.drafts_cancelled", { tenantId, count: drafts.length });
    }
    return drafts.length;
  }

  outstanding(tenantId: string): Attachment[] {
    return [
      ...this.repository.listByStatus(tenantId, "draft"),
      ...this.repository.listByStatus(tenantId, "active"),
    ];
  }

  page(tenantId: string, offset: number, limit: number): AttachmentPage {
    return this.repository.page(tenantId, offset, limit);
  }

  summary(tenantId: string): AttachmentSummary {
    const all = this.repository.all(tenantId);
    return {
      total: all.length,
      outstanding: this.outstanding(tenantId).length,
      amountCents: all.reduce((sum, item) => sum + item.amountCents, 0),
      byStatus: attachmentStatusCounts(all),
    };
  }

  discard(tenantId: string, id: string): void {
    const current = this.repository.require(tenantId, id);
    if (!isAttachmentTerminal(current)) {
      throw new IllegalTransitionError(`attachment ${id} must reach a terminal status first`);
    }
    this.repository.remove(tenantId, id);
    auditEvent("attachment.discarded", { tenantId, id });
  }
}
