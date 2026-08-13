import type { RequestHandler } from "express";
import type { Logger } from "pino";
import { newId } from "../utils/id";
import { startTimer } from "../utils/time";

/** Header names whose values must never reach the log stream. */
const REDACTED_HEADERS = new Set(["authorization", "cookie", "x-api-key"]);

/** Copies a header map, replacing sensitive values with a placeholder. */
export function redactHeaders(headers: Record<string, unknown>): Record<string, unknown> {
  const safe: Record<string, unknown> = {};
  for (const [key, value] of Object.entries(headers)) {
    safe[key] = REDACTED_HEADERS.has(key.toLowerCase()) ? "[redacted]" : value;
  }
  return safe;
}

/**
 * Per-request pino logging: assigns an `x-request-id`, then emits one
 * structured line when the response finishes, including the monotonic
 * duration and a redacted copy of the inbound headers.
 */
export function requestLogger(logger: Logger): RequestHandler {
  return (req, res, next) => {
    const requestId = newId("req");
    const elapsed = startTimer();
    res.setHeader("x-request-id", requestId);
    res.on("finish", () => {
      logger.info(
        {
          requestId,
          method: req.method,
          path: req.path,
          status: res.statusCode,
          durationMs: Math.round(elapsed() * 1000) / 1000,
          headers: redactHeaders(req.headers as Record<string, unknown>),
        },
        "request completed",
      );
    });
    next();
  };
}
