import { Router } from "express";
import { nowIso } from "../utils/time";

const startedAtMs = Date.now();

/**
 * Unauthenticated liveness/readiness probes. The store is in-memory, so
 * readiness is equivalent to liveness — there is no downstream dependency
 * to wait for.
 */
export function healthRouter(): Router {
  const router = Router();

  router.get("/", (_req, res) => {
    res.json({
      status: "ok",
      time: nowIso(),
      uptimeSeconds: Math.floor((Date.now() - startedAtMs) / 1000),
    });
  });

  router.get("/ready", (_req, res) => {
    res.json({ status: "ready" });
  });

  return router;
}
