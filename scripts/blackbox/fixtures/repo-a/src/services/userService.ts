import { ConflictError, NotFoundError } from "../middleware/errors";
import { newId } from "../utils/id";
import { nowIso } from "../utils/time";
import type { UserCreateInput, UserRole } from "./validation";

export interface User {
  readonly id: string;
  name: string;
  email: string;
  role: UserRole;
  active: boolean;
  readonly createdAt: string;
}

const store = new Map<string, User>();

/** Registers a user; emails are unique across the board (case already folded by zod). */
export function createUser(input: UserCreateInput): User {
  const duplicate = [...store.values()].find((user) => user.email === input.email);
  if (duplicate) {
    throw new ConflictError(`email ${input.email} is already registered`);
  }
  const user: User = {
    id: newId("user"),
    name: input.name,
    email: input.email,
    role: input.role,
    active: true,
    createdAt: nowIso(),
  };
  store.set(user.id, user);
  return user;
}

export function getUser(id: string): User {
  const user = store.get(id);
  if (!user) {
    throw new NotFoundError(`user ${id} does not exist`);
  }
  return user;
}

export function listUsers(): User[] {
  return [...store.values()].sort((a, b) => a.name.localeCompare(b.name));
}

/**
 * Soft delete: tasks may still reference the user's id, so instead of removing
 * the record we mark it inactive and keep it resolvable.
 */
export function deactivateUser(id: string): User {
  const user = getUser(id);
  user.active = false;
  return user;
}

/** Test hook: empties the store so each spec starts from a known state. */
export function resetUsers(): void {
  store.clear();
}
