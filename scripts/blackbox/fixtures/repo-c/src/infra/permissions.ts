const grants = new Map<string, Set<string>>();

/** Grants are seeded at boot; an unknown tenant has no permissions at all. */
export function grantPermission(tenantId: string, permission: string): void {
  const set = grants.get(tenantId) ?? new Set<string>();
  set.add(permission);
  grants.set(tenantId, set);
}

export function hasPermission(tenantId: string, permission: string): boolean {
  return grants.get(tenantId)?.has(permission) ?? false;
}

export function revokeTenant(tenantId: string): void {
  grants.delete(tenantId);
}
