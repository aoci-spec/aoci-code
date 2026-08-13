import { randomBytes } from "node:crypto";

/** Crockford base32 alphabet as used by ULIDs (no I, L, O, U). */
const ALPHABET = "0123456789ABCDEFGHJKMNPQRSTVWXYZ";
const TIME_LENGTH = 10;
const RANDOM_LENGTH = 16;

export type IdPrefix = "task" | "user" | "req";

function encodeTime(epochMs: number): string {
  let remaining = epochMs;
  let out = "";
  for (let i = 0; i < TIME_LENGTH; i += 1) {
    out = ALPHABET[remaining % 32] + out;
    remaining = Math.floor(remaining / 32);
  }
  return out;
}

function encodeRandom(): string {
  const bytes = randomBytes(RANDOM_LENGTH);
  let out = "";
  for (let i = 0; i < RANDOM_LENGTH; i += 1) {
    out += ALPHABET[bytes[i] % 32];
  }
  return out;
}

/**
 * Builds a prefixed, ULID-ish identifier such as `task_01J1GZ4Q0RVAHT3M8W2E9XKCPD`.
 * The leading time component keeps ids of the same prefix lexically sortable
 * by creation instant.
 */
export function newId(prefix: IdPrefix): string {
  return `${prefix}_${encodeTime(Date.now())}${encodeRandom()}`;
}

const ID_PATTERN = new RegExp(`^(task|user|req)_[${ALPHABET}]{${TIME_LENGTH + RANDOM_LENGTH}}$`);

/** Structural check; pass `prefix` to also pin the expected entity kind. */
export function isWellFormedId(value: string, prefix?: IdPrefix): boolean {
  if (!ID_PATTERN.test(value)) {
    return false;
  }
  return prefix === undefined || value.startsWith(`${prefix}_`);
}
