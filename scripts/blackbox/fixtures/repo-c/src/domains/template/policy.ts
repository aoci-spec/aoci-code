import { ForbiddenError } from "../../infra/errors";
import { hasPermission } from "../../infra/permissions";

export type TemplateAction = "read" | "write" | "administer";

const required: Record<TemplateAction, readonly string[]> = {
  read: ["template:read"],
  write: ["template:read", "template:write"],
  administer: ["template:read", "template:write", "template:admin"],
};

/** Tenant-scoped authorization for message template operations. */
export function assertTemplateAccess(tenantId: string, action: TemplateAction): void {
  if (tenantId.trim().length === 0) {
    throw new ForbiddenError("template access requires a tenant header");
  }
  for (const permission of required[action]) {
    if (!hasPermission(tenantId, permission)) {
      throw new ForbiddenError(`template ${action} is not granted for tenant ${tenantId}`);
    }
  }
}

export function mayTemplate(tenantId: string, action: TemplateAction): boolean {
  try {
    assertTemplateAccess(tenantId, action);
    return true;
  } catch {
    return false;
  }
}

export function grantedActions(tenantId: string): TemplateAction[] {
  const actions: TemplateAction[] = ["read", "write", "administer"];
  return actions.filter((action) => mayTemplate(tenantId, action));
}

export function assertSameTenant(tenantId: string, resourceTenantId: string): void {
  if (tenantId !== resourceTenantId) {
    throw new ForbiddenError("template belongs to a different tenant");
  }
}
