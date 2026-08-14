import {
  Document,
  DocumentStatus,
  applyDocumentTransition,
  canDocumentTransition,
  isDocumentTerminal,
  newDocument,
  withDocumentAmount,
  withDocumentLabel,
  documentStatusCounts,
} from "./model";
import { DocumentPage, DocumentRepository } from "./repository";
import { IllegalTransitionError, ValidationError } from "../../infra/errors";
import { auditEvent } from "../../infra/audit";

export interface DocumentSummary {
  readonly total: number;
  readonly outstanding: number;
  readonly amountCents: number;
  readonly byStatus: Record<DocumentStatus, number>;
}

/** Business rules for the stored document lifecycle. */
export class DocumentService {
  constructor(private readonly repository: DocumentRepository) {}

  create(tenantId: string, id: string, reference: string, amountCents: number): Document {
    const draft = withDocumentAmount(newDocument(id, tenantId, reference), amountCents);
    const saved = this.repository.insert(draft);
    auditEvent("document.created", { tenantId, id });
    return saved;
  }

  get(tenantId: string, id: string): Document {
    return this.repository.require(tenantId, id);
  }

  transition(tenantId: string, id: string, next: DocumentStatus): Document {
    const current = this.repository.require(tenantId, id);
    if (isDocumentTerminal(current)) {
      throw new IllegalTransitionError(`document ${id} is terminal`);
    }
    if (!canDocumentTransition(current.status, next)) {
      throw new IllegalTransitionError(`document ${id}: ${current.status} -> ${next}`);
    }
    const saved = this.repository.save(applyDocumentTransition(current, next));
    auditEvent("document.transitioned", { tenantId, id, next });
    return saved;
  }

  adjustAmount(tenantId: string, id: string, deltaCents: number): Document {
    const current = this.repository.require(tenantId, id);
    if (isDocumentTerminal(current)) {
      throw new IllegalTransitionError(`document ${id} is terminal`);
    }
    const amountCents = current.amountCents + deltaCents;
    if (amountCents < 0) {
      throw new ValidationError(`document ${id} cannot fall below zero`);
    }
    return this.repository.save(withDocumentAmount(current, amountCents));
  }

  label(tenantId: string, id: string, label: string): Document {
    return this.repository.save(withDocumentLabel(this.repository.require(tenantId, id), label));
  }

  cancelAllDrafts(tenantId: string): number {
    const drafts = this.repository.listByStatus(tenantId, "draft");
    for (const draft of drafts) {
      this.repository.save(applyDocumentTransition(draft, "cancelled"));
    }
    if (drafts.length > 0) {
      auditEvent("document.drafts_cancelled", { tenantId, count: drafts.length });
    }
    return drafts.length;
  }

  outstanding(tenantId: string): Document[] {
    return [
      ...this.repository.listByStatus(tenantId, "draft"),
      ...this.repository.listByStatus(tenantId, "active"),
    ];
  }

  page(tenantId: string, offset: number, limit: number): DocumentPage {
    return this.repository.page(tenantId, offset, limit);
  }

  summary(tenantId: string): DocumentSummary {
    const all = this.repository.all(tenantId);
    return {
      total: all.length,
      outstanding: this.outstanding(tenantId).length,
      amountCents: all.reduce((sum, item) => sum + item.amountCents, 0),
      byStatus: documentStatusCounts(all),
    };
  }

  discard(tenantId: string, id: string): void {
    const current = this.repository.require(tenantId, id);
    if (!isDocumentTerminal(current)) {
      throw new IllegalTransitionError(`document ${id} must reach a terminal status first`);
    }
    this.repository.remove(tenantId, id);
    auditEvent("document.discarded", { tenantId, id });
  }
}
