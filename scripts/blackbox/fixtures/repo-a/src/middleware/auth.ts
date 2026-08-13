import { createHmac, timingSafeEqual } from "node:crypto";
import type { RequestHandler } from "express";
import { UnauthorizedError } from "./errors";

/**
 * Stateless bearer-token auth. A token is `<clientId>.<hexHmac>` where the
 * HMAC is SHA-256 over the clientId keyed by AUTH_TOKEN_SECRET, so tokens can
 * be verified without any token store.
 */

export function signToken(clientId: string, secret: string): string {
  const mac = createHmac("sha256", secret).update(clientId).digest("hex");
  return `${clientId}.${mac}`;
}

/** Returns the embedded clientId when the signature checks out, else null. */
export function verifyToken(token: string, secret: string): string | null {
  const dot = token.lastIndexOf(".");
  if (dot <= 0 || dot === token.length - 1) {
    return null;
  }
  const clientId = token.slice(0, dot);
  const given = Buffer.from(token.slice(dot + 1), "utf8");
  const expected = Buffer.from(
    createHmac("sha256", secret).update(clientId).digest("hex"),
    "utf8",
  );
  if (given.length !== expected.length || !timingSafeEqual(given, expected)) {
    return null;
  }
  return clientId;
}

/** Express guard: rejects the request unless a valid bearer token is present. */
export function requireAuth(secret: string): RequestHandler {
  return (req, res, next) => {
    const header = req.header("authorization");
    if (!header || !header.toLowerCase().startsWith("bearer ")) {
      next(new UnauthorizedError("missing bearer token"));
      return;
    }
    const clientId = verifyToken(header.slice("bearer ".length).trim(), secret);
    if (clientId === null) {
      next(new UnauthorizedError("invalid bearer token"));
      return;
    }
    res.locals.clientId = clientId;
    next();
  };
}
