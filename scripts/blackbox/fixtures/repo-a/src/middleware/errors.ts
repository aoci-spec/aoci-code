import type { ErrorRequestHandler } from "express";
import type { Logger } from "pino";
import { ZodError } from "zod";

/** Base class for every error the API intentionally surfaces to clients. */
export class HttpError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    message: string,
  ) {
    super(message);
    this.name = new.target.name;
  }
}

export class BadRequestError extends HttpError {
  constructor(message: string) {
    super(400, "bad_request", message);
  }
}

export class UnauthorizedError extends HttpError {
  constructor(message: string) {
    super(401, "unauthorized", message);
  }
}

export class NotFoundError extends HttpError {
  constructor(message: string) {
    super(404, "not_found", message);
  }
}

export class ConflictError extends HttpError {
  constructor(message: string) {
    super(409, "conflict", message);
  }
}

/** Raised by the task state machine when a status change is not legal. */
export class IllegalTransitionError extends HttpError {
  constructor(message: string) {
    super(409, "illegal_transition", message);
  }
}

/**
 * Terminal error middleware: maps typed errors (and zod parse failures) onto
 * a stable `{ error: { code, message } }` envelope. Anything unrecognized is
 * logged and answered with an opaque 500.
 */
export function errorHandler(logger: Logger): ErrorRequestHandler {
  return (err, req, res, _next) => {
    if (err instanceof ZodError) {
      const detail = err.issues
        .map((issue) => `${issue.path.join(".") || "body"}: ${issue.message}`)
        .join("; ");
      res.status(400).json({ error: { code: "validation_failed", message: detail } });
      return;
    }
    if (err instanceof HttpError) {
      res.status(err.status).json({ error: { code: err.code, message: err.message } });
      return;
    }
    logger.error({ err, method: req.method, path: req.path }, "unhandled error");
    res.status(500).json({ error: { code: "internal", message: "internal server error" } });
  };
}
