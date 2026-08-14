import { ForbiddenError } from "../../infra/errors";
import { hasPermission } from "../../infra/permissions";

export type MessageAction = "read" | "write" | "administer";

const required: Record<MessageAction, readonly string[]> = {
  read: ["message:read"],
  write: ["message:read", "message:write"],
  administer: ["message:read", "message:write", "message:admin"],
};

/** Tenant-scoped authorization for customer message operations. */
export function assertMessageAccess(tenantId: string, action: MessageAction): void {
  if (tenantId.trim().length === 0) {
    throw new ForbiddenError("message access requires a tenant header");
  }
  for (const permission of required[action]) {
    if (!hasPermission(tenantId, permission)) {
      throw new ForbiddenError(`message ${action} is not granted for tenant ${tenantId}`);
    }
  }
}

export function mayMessage(tenantId: string, action: MessageAction): boolean {
  try {
    assertMessageAccess(tenantId, action);
    return true;
  } catch {
    return false;
  }
}

export function grantedActions(tenantId: string): MessageAction[] {
  const actions: MessageAction[] = ["read", "write", "administer"];
  return actions.filter((action) => mayMessage(tenantId, action));
}

export function assertSameTenant(tenantId: string, resourceTenantId: string): void {
  if (tenantId !== resourceTenantId) {
    throw new ForbiddenError("message belongs to a different tenant");
  }
}
