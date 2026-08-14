import { ForbiddenError } from "../../infra/errors";
import { hasPermission } from "../../infra/permissions";

export type AttachmentAction = "read" | "write" | "administer";

const required: Record<AttachmentAction, readonly string[]> = {
  read: ["attachment:read"],
  write: ["attachment:read", "attachment:write"],
  administer: ["attachment:read", "attachment:write", "attachment:admin"],
};

/** Tenant-scoped authorization for file attachment operations. */
export function assertAttachmentAccess(tenantId: string, action: AttachmentAction): void {
  if (tenantId.trim().length === 0) {
    throw new ForbiddenError("attachment access requires a tenant header");
  }
  for (const permission of required[action]) {
    if (!hasPermission(tenantId, permission)) {
      throw new ForbiddenError(`attachment ${action} is not granted for tenant ${tenantId}`);
    }
  }
}

export function mayAttachment(tenantId: string, action: AttachmentAction): boolean {
  try {
    assertAttachmentAccess(tenantId, action);
    return true;
  } catch {
    return false;
  }
}

export function grantedActions(tenantId: string): AttachmentAction[] {
  const actions: AttachmentAction[] = ["read", "write", "administer"];
  return actions.filter((action) => mayAttachment(tenantId, action));
}

export function assertSameTenant(tenantId: string, resourceTenantId: string): void {
  if (tenantId !== resourceTenantId) {
    throw new ForbiddenError("attachment belongs to a different tenant");
  }
}
