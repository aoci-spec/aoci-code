/** Deterministic timestamp helpers shared by every domain model. */
export function isoTimestamp(at: Date = new Date()): string {
  return at.toISOString();
}

export function durationMillis(from: string, to: string): number {
  return Date.parse(to) - Date.parse(from);
}

export function isBefore(left: string, right: string): boolean {
  return Date.parse(left) < Date.parse(right);
}
