import {
  Customer,
  CustomerStatus,
  applyCustomerTransition,
  canCustomerTransition,
  isCustomerTerminal,
  newCustomer,
  withCustomerAmount,
  withCustomerLabel,
  customerStatusCounts,
} from "./model";
import { CustomerPage, CustomerRepository } from "./repository";
import { IllegalTransitionError, ValidationError } from "../../infra/errors";
import { auditEvent } from "../../infra/audit";

export interface CustomerSummary {
  readonly total: number;
  readonly outstanding: number;
  readonly amountCents: number;
  readonly byStatus: Record<CustomerStatus, number>;
}

/** Business rules for the customer account lifecycle. */
export class CustomerService {
  constructor(private readonly repository: CustomerRepository) {}

  create(tenantId: string, id: string, reference: string, amountCents: number): Customer {
    const draft = withCustomerAmount(newCustomer(id, tenantId, reference), amountCents);
    const saved = this.repository.insert(draft);
    auditEvent("customer.created", { tenantId, id });
    return saved;
  }

  get(tenantId: string, id: string): Customer {
    return this.repository.require(tenantId, id);
  }

  transition(tenantId: string, id: string, next: CustomerStatus): Customer {
    const current = this.repository.require(tenantId, id);
    if (isCustomerTerminal(current)) {
      throw new IllegalTransitionError(`customer ${id} is terminal`);
    }
    if (!canCustomerTransition(current.status, next)) {
      throw new IllegalTransitionError(`customer ${id}: ${current.status} -> ${next}`);
    }
    const saved = this.repository.save(applyCustomerTransition(current, next));
    auditEvent("customer.transitioned", { tenantId, id, next });
    return saved;
  }

  adjustAmount(tenantId: string, id: string, deltaCents: number): Customer {
    const current = this.repository.require(tenantId, id);
    if (isCustomerTerminal(current)) {
      throw new IllegalTransitionError(`customer ${id} is terminal`);
    }
    const amountCents = current.amountCents + deltaCents;
    if (amountCents < 0) {
      throw new ValidationError(`customer ${id} cannot fall below zero`);
    }
    return this.repository.save(withCustomerAmount(current, amountCents));
  }

  label(tenantId: string, id: string, label: string): Customer {
    return this.repository.save(withCustomerLabel(this.repository.require(tenantId, id), label));
  }

  cancelAllDrafts(tenantId: string): number {
    const drafts = this.repository.listByStatus(tenantId, "draft");
    for (const draft of drafts) {
      this.repository.save(applyCustomerTransition(draft, "cancelled"));
    }
    if (drafts.length > 0) {
      auditEvent("customer.drafts_cancelled", { tenantId, count: drafts.length });
    }
    return drafts.length;
  }

  outstanding(tenantId: string): Customer[] {
    return [
      ...this.repository.listByStatus(tenantId, "draft"),
      ...this.repository.listByStatus(tenantId, "active"),
    ];
  }

  page(tenantId: string, offset: number, limit: number): CustomerPage {
    return this.repository.page(tenantId, offset, limit);
  }

  summary(tenantId: string): CustomerSummary {
    const all = this.repository.all(tenantId);
    return {
      total: all.length,
      outstanding: this.outstanding(tenantId).length,
      amountCents: all.reduce((sum, item) => sum + item.amountCents, 0),
      byStatus: customerStatusCounts(all),
    };
  }

  discard(tenantId: string, id: string): void {
    const current = this.repository.require(tenantId, id);
    if (!isCustomerTerminal(current)) {
      throw new IllegalTransitionError(`customer ${id} must reach a terminal status first`);
    }
    this.repository.remove(tenantId, id);
    auditEvent("customer.discarded", { tenantId, id });
  }
}
