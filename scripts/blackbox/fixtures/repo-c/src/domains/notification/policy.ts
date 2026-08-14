import { ForbiddenError } from "../../infra/errors";
import { hasPermission } from "../../infra/permissions";

export type NotificationAction = "read" | "write" | "administer";

const required: Record<NotificationAction, readonly string[]> = {
  read: ["notification:read"],
  write: ["notification:read", "notification:write"],
  administer: ["notification:read", "notification:write", "notification:admin"],
};

/** Tenant-scoped authorization for outbound notification operations. */
export function assertNotificationAccess(tenantId: string, action: NotificationAction): void {
  if (tenantId.trim().length === 0) {
    throw new ForbiddenError("notification access requires a tenant header");
  }
  for (const permission of required[action]) {
    if (!hasPermission(tenantId, permission)) {
      throw new ForbiddenError(`notification ${action} is not granted for tenant ${tenantId}`);
    }
  }
}

export function mayNotification(tenantId: string, action: NotificationAction): boolean {
  try {
    assertNotificationAccess(tenantId, action);
    return true;
  } catch {
    return false;
  }
}

export function grantedActions(tenantId: string): NotificationAction[] {
  const actions: NotificationAction[] = ["read", "write", "administer"];
  return actions.filter((action) => mayNotification(tenantId, action));
}

export function assertSameTenant(tenantId: string, resourceTenantId: string): void {
  if (tenantId !== resourceTenantId) {
    throw new ForbiddenError("notification belongs to a different tenant");
  }
}
