/** Typed failures mapped to HTTP status by the error middleware. */
export class ValidationError extends Error {}
export class NotFoundError extends Error {}
export class ForbiddenError extends Error {}
export class IllegalTransitionError extends Error {}
export class ConflictError extends Error {}

export function statusFor(error: unknown): number {
  if (error instanceof ValidationError) return 400;
  if (error instanceof ForbiddenError) return 403;
  if (error instanceof NotFoundError) return 404;
  if (error instanceof ConflictError) return 409;
  if (error instanceof IllegalTransitionError) return 422;
  return 500;
}
