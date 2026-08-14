import { ForbiddenError } from "../../infra/errors";
import { hasPermission } from "../../infra/permissions";

export type ShipmentAction = "read" | "write" | "administer";

const required: Record<ShipmentAction, readonly string[]> = {
  read: ["shipment:read"],
  write: ["shipment:read", "shipment:write"],
  administer: ["shipment:read", "shipment:write", "shipment:admin"],
};

/** Tenant-scoped authorization for outbound shipment operations. */
export function assertShipmentAccess(tenantId: string, action: ShipmentAction): void {
  if (tenantId.trim().length === 0) {
    throw new ForbiddenError("shipment access requires a tenant header");
  }
  for (const permission of required[action]) {
    if (!hasPermission(tenantId, permission)) {
      throw new ForbiddenError(`shipment ${action} is not granted for tenant ${tenantId}`);
    }
  }
}

export function mayShipment(tenantId: string, action: ShipmentAction): boolean {
  try {
    assertShipmentAccess(tenantId, action);
    return true;
  } catch {
    return false;
  }
}

export function grantedActions(tenantId: string): ShipmentAction[] {
  const actions: ShipmentAction[] = ["read", "write", "administer"];
  return actions.filter((action) => mayShipment(tenantId, action));
}

export function assertSameTenant(tenantId: string, resourceTenantId: string): void {
  if (tenantId !== resourceTenantId) {
    throw new ForbiddenError("shipment belongs to a different tenant");
  }
}
