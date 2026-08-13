import { z } from "zod";

/**
 * Zod schemas shared by the route handlers. Routes parse raw payloads with
 * these; the service layer receives only the inferred (already-defaulted)
 * types, so validation policy lives in exactly one place.
 */

export const taskStatusSchema = z.enum(["todo", "doing", "done"]);
export const taskPrioritySchema = z.enum(["low", "medium", "high"]);
export const userRoleSchema = z.enum(["admin", "member"]);

export const taskCreateSchema = z.object({
  title: z.string().trim().min(1).max(200),
  description: z.string().max(2000).default(""),
  assigneeId: z.string().startsWith("user_").optional(),
  priority: taskPrioritySchema.default("medium"),
});

export const taskUpdateSchema = z
  .object({
    title: z.string().trim().min(1).max(200),
    description: z.string().max(2000),
    assigneeId: z.string().startsWith("user_").nullable(),
    priority: taskPrioritySchema,
  })
  .partial()
  .refine((patch) => Object.keys(patch).length > 0, {
    message: "patch must set at least one field",
  });

export const taskTransitionSchema = z.object({
  status: taskStatusSchema,
});

export const taskListQuerySchema = z.object({
  status: taskStatusSchema.optional(),
  assigneeId: z.string().startsWith("user_").optional(),
});

export const userCreateSchema = z.object({
  name: z.string().trim().min(1).max(120),
  email: z.string().email().toLowerCase(),
  role: userRoleSchema.default("member"),
});

export type TaskStatus = z.infer<typeof taskStatusSchema>;
export type TaskPriority = z.infer<typeof taskPrioritySchema>;
export type TaskCreateInput = z.infer<typeof taskCreateSchema>;
export type TaskUpdateInput = z.infer<typeof taskUpdateSchema>;
export type TaskListQuery = z.infer<typeof taskListQuerySchema>;
export type UserRole = z.infer<typeof userRoleSchema>;
export type UserCreateInput = z.infer<typeof userCreateSchema>;
