import { ForbiddenError } from "../../infra/errors";
import { hasPermission } from "../../infra/permissions";

export type TicketAction = "read" | "write" | "administer";

const required: Record<TicketAction, readonly string[]> = {
  read: ["ticket:read"],
  write: ["ticket:read", "ticket:write"],
  administer: ["ticket:read", "ticket:write", "ticket:admin"],
};

/** Tenant-scoped authorization for support ticket operations. */
export function assertTicketAccess(tenantId: string, action: TicketAction): void {
  if (tenantId.trim().length === 0) {
    throw new ForbiddenError("ticket access requires a tenant header");
  }
  for (const permission of required[action]) {
    if (!hasPermission(tenantId, permission)) {
      throw new ForbiddenError(`ticket ${action} is not granted for tenant ${tenantId}`);
    }
  }
}

export function mayTicket(tenantId: string, action: TicketAction): boolean {
  try {
    assertTicketAccess(tenantId, action);
    return true;
  } catch {
    return false;
  }
}

export function grantedActions(tenantId: string): TicketAction[] {
  const actions: TicketAction[] = ["read", "write", "administer"];
  return actions.filter((action) => mayTicket(tenantId, action));
}

export function assertSameTenant(tenantId: string, resourceTenantId: string): void {
  if (tenantId !== resourceTenantId) {
    throw new ForbiddenError("ticket belongs to a different tenant");
  }
}
