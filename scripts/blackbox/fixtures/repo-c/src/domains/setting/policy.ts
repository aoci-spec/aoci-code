import { ForbiddenError } from "../../infra/errors";
import { hasPermission } from "../../infra/permissions";

export type SettingAction = "read" | "write" | "administer";

const required: Record<SettingAction, readonly string[]> = {
  read: ["setting:read"],
  write: ["setting:read", "setting:write"],
  administer: ["setting:read", "setting:write", "setting:admin"],
};

/** Tenant-scoped authorization for tenant setting operations. */
export function assertSettingAccess(tenantId: string, action: SettingAction): void {
  if (tenantId.trim().length === 0) {
    throw new ForbiddenError("setting access requires a tenant header");
  }
  for (const permission of required[action]) {
    if (!hasPermission(tenantId, permission)) {
      throw new ForbiddenError(`setting ${action} is not granted for tenant ${tenantId}`);
    }
  }
}

export function maySetting(tenantId: string, action: SettingAction): boolean {
  try {
    assertSettingAccess(tenantId, action);
    return true;
  } catch {
    return false;
  }
}

export function grantedActions(tenantId: string): SettingAction[] {
  const actions: SettingAction[] = ["read", "write", "administer"];
  return actions.filter((action) => maySetting(tenantId, action));
}

export function assertSameTenant(tenantId: string, resourceTenantId: string): void {
  if (tenantId !== resourceTenantId) {
    throw new ForbiddenError("setting belongs to a different tenant");
  }
}
