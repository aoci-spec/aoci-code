import { Inventory, InventoryStatus, compareInventory, touchInventory, validateInventory } from "./model";
import { ConflictError, NotFoundError } from "../../infra/errors";

export interface InventoryPage {
  readonly items: readonly Inventory[];
  readonly total: number;
  readonly offset: number;
}

/** In-memory store for stock position records, keyed by tenant and id. */
export class InventoryRepository {
  private readonly byTenant = new Map<string, Map<string, Inventory>>();
  private readonly byReference = new Map<string, string>();

  private tenantMap(tenantId: string): Map<string, Inventory> {
    const existing = this.byTenant.get(tenantId);
    if (existing) {
      return existing;
    }
    const created = new Map<string, Inventory>();
    this.byTenant.set(tenantId, created);
    return created;
  }

  private referenceKey(tenantId: string, reference: string): string {
    return `${tenantId}::${reference}`;
  }

  insert(value: Inventory): Inventory {
    validateInventory(value);
    const tenant = this.tenantMap(value.tenantId);
    if (tenant.has(value.id)) {
      throw new ConflictError(`inventory ${value.id} already exists`);
    }
    const referenceKey = this.referenceKey(value.tenantId, value.reference);
    if (this.byReference.has(referenceKey)) {
      throw new ConflictError(`inventory reference ${value.reference} is already used`);
    }
    tenant.set(value.id, value);
    this.byReference.set(referenceKey, value.id);
    return value;
  }

  save(value: Inventory): Inventory {
    validateInventory(value);
    const tenant = this.tenantMap(value.tenantId);
    const previous = tenant.get(value.id);
    if (previous && previous.reference !== value.reference) {
      this.byReference.delete(this.referenceKey(value.tenantId, previous.reference));
      this.byReference.set(this.referenceKey(value.tenantId, value.reference), value.id);
    }
    const stored = touchInventory(value);
    tenant.set(stored.id, stored);
    return stored;
  }

  find(tenantId: string, id: string): Inventory | undefined {
    return this.byTenant.get(tenantId)?.get(id);
  }

  findByReference(tenantId: string, reference: string): Inventory | undefined {
    const id = this.byReference.get(this.referenceKey(tenantId, reference));
    return id === undefined ? undefined : this.find(tenantId, id);
  }

  require(tenantId: string, id: string): Inventory {
    const found = this.find(tenantId, id);
    if (!found) {
      throw new NotFoundError(`inventory ${id} not found for tenant ${tenantId}`);
    }
    return found;
  }

  listByStatus(tenantId: string, status: InventoryStatus): Inventory[] {
    return this.all(tenantId).filter((item) => item.status === status);
  }

  listByLabel(tenantId: string, label: string): Inventory[] {
    return this.all(tenantId).filter((item) => item.labels.includes(label));
  }

  page(tenantId: string, offset: number, limit: number): InventoryPage {
    const all = this.all(tenantId);
    const start = Math.max(0, offset);
    return { items: all.slice(start, start + Math.max(0, limit)), total: all.length, offset: start };
  }

  all(tenantId: string): Inventory[] {
    return [...(this.byTenant.get(tenantId)?.values() ?? [])].sort(compareInventory);
  }

  remove(tenantId: string, id: string): boolean {
    const found = this.find(tenantId, id);
    if (!found) {
      return false;
    }
    this.byReference.delete(this.referenceKey(tenantId, found.reference));
    return this.byTenant.get(tenantId)?.delete(id) ?? false;
  }

  count(tenantId: string): number {
    return this.byTenant.get(tenantId)?.size ?? 0;
  }

  clearTenant(tenantId: string): void {
    for (const value of this.all(tenantId)) {
      this.byReference.delete(this.referenceKey(tenantId, value.reference));
    }
    this.byTenant.delete(tenantId);
  }
}
