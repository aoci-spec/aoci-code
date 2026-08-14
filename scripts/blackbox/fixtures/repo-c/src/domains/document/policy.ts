import { ForbiddenError } from "../../infra/errors";
import { hasPermission } from "../../infra/permissions";

export type DocumentAction = "read" | "write" | "administer";

const required: Record<DocumentAction, readonly string[]> = {
  read: ["document:read"],
  write: ["document:read", "document:write"],
  administer: ["document:read", "document:write", "document:admin"],
};

/** Tenant-scoped authorization for stored document operations. */
export function assertDocumentAccess(tenantId: string, action: DocumentAction): void {
  if (tenantId.trim().length === 0) {
    throw new ForbiddenError("document access requires a tenant header");
  }
  for (const permission of required[action]) {
    if (!hasPermission(tenantId, permission)) {
      throw new ForbiddenError(`document ${action} is not granted for tenant ${tenantId}`);
    }
  }
}

export function mayDocument(tenantId: string, action: DocumentAction): boolean {
  try {
    assertDocumentAccess(tenantId, action);
    return true;
  } catch {
    return false;
  }
}

export function grantedActions(tenantId: string): DocumentAction[] {
  const actions: DocumentAction[] = ["read", "write", "administer"];
  return actions.filter((action) => mayDocument(tenantId, action));
}

export function assertSameTenant(tenantId: string, resourceTenantId: string): void {
  if (tenantId !== resourceTenantId) {
    throw new ForbiddenError("document belongs to a different tenant");
  }
}
