import { z } from "zod";

/**
 * Environment-driven configuration, validated once at boot. The schema is the
 * single source of truth for defaults; anything invalid throws a descriptive
 * error so the process can fail fast before binding a port.
 */

const LOG_LEVELS = ["fatal", "error", "warn", "info", "debug", "trace"] as const;

const envSchema = z.object({
  PORT: z.coerce.number().int().min(1).max(65535).default(3000),
  LOG_LEVEL: z.enum(LOG_LEVELS).default("info"),
  AUTH_TOKEN_SECRET: z.string().min(16, "must be at least 16 characters"),
});

export type LogLevel = (typeof LOG_LEVELS)[number];

export interface AppConfig {
  readonly port: number;
  readonly logLevel: LogLevel;
  readonly authTokenSecret: string;
}

export function loadConfig(env: NodeJS.ProcessEnv = process.env): AppConfig {
  const parsed = envSchema.safeParse(env);
  if (!parsed.success) {
    const detail = parsed.error.issues
      .map((issue) => `${issue.path.join(".") || "env"}: ${issue.message}`)
      .join("; ");
    throw new Error(`invalid environment: ${detail}`);
  }
  return {
    port: parsed.data.PORT,
    logLevel: parsed.data.LOG_LEVEL,
    authTokenSecret: parsed.data.AUTH_TOKEN_SECRET,
  };
}
