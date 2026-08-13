import express from "express";
import pino from "pino";
import { loadConfig, type AppConfig } from "./config";
import { requireAuth } from "./middleware/auth";
import { errorHandler, NotFoundError } from "./middleware/errors";
import { requestLogger } from "./middleware/logging";
import { healthRouter } from "./routes/health";
import { tasksRouter } from "./routes/tasks";
import { usersRouter } from "./routes/users";

/** Fail fast: a misconfigured process should die before it binds a port. */
function mustLoadConfig(): AppConfig {
  try {
    return loadConfig();
  } catch (err) {
    console.error(`[taskboard-api] fatal: ${(err as Error).message}`);
    process.exit(1);
  }
}

function main(): void {
  const config = mustLoadConfig();
  const logger = pino({ level: config.logLevel });

  const app = express();
  app.use(express.json({ limit: "256kb" }));
  app.use(requestLogger(logger));

  app.use("/health", healthRouter());
  app.use("/api/tasks", requireAuth(config.authTokenSecret), tasksRouter());
  app.use("/api/users", requireAuth(config.authTokenSecret), usersRouter());

  app.use((req, _res, next) => {
    next(new NotFoundError(`no route for ${req.method} ${req.path}`));
  });
  app.use(errorHandler(logger));

  const server = app.listen(config.port, () => {
    logger.info({ port: config.port }, "taskboard-api listening");
  });

  const shutdown = (signal: NodeJS.Signals): void => {
    logger.info({ signal }, "shutting down");
    server.close((err) => {
      if (err) {
        logger.error({ err }, "error while closing server");
        process.exitCode = 1;
      }
      logger.info("shutdown complete");
    });
    // Give in-flight requests a grace period, then force-exit.
    setTimeout(() => process.exit(1), 10_000).unref();
  };
  process.on("SIGINT", shutdown);
  process.on("SIGTERM", shutdown);
}

main();
