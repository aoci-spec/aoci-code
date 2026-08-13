import { describe, expect, it } from "vitest";
import {
  taskCreateSchema,
  taskListQuerySchema,
  taskTransitionSchema,
  taskUpdateSchema,
  userCreateSchema,
} from "../src/services/validation";

describe("taskCreateSchema", () => {
  it("applies defaults for description and priority", () => {
    const parsed = taskCreateSchema.parse({ title: "triage inbox" });
    expect(parsed.description).toBe("");
    expect(parsed.priority).toBe("medium");
  });

  it("trims the title and rejects one that trims to empty", () => {
    expect(taskCreateSchema.parse({ title: "  ship it  " }).title).toBe("ship it");
    expect(() => taskCreateSchema.parse({ title: "   " })).toThrow();
  });

  it("rejects assignee ids without the user_ prefix", () => {
    expect(() =>
      taskCreateSchema.parse({ title: "x", assigneeId: "task_notauser" }),
    ).toThrow();
  });
});

describe("taskUpdateSchema", () => {
  it("rejects an empty patch object", () => {
    expect(() => taskUpdateSchema.parse({})).toThrow();
  });

  it("accepts clearing the assignee with null", () => {
    expect(taskUpdateSchema.parse({ assigneeId: null }).assigneeId).toBeNull();
  });
});

describe("taskTransitionSchema", () => {
  it("only admits the three known statuses", () => {
    expect(taskTransitionSchema.parse({ status: "doing" }).status).toBe("doing");
    expect(() => taskTransitionSchema.parse({ status: "archived" })).toThrow();
  });
});

describe("taskListQuerySchema", () => {
  it("accepts an empty query and preserves given filters", () => {
    expect(taskListQuerySchema.parse({})).toEqual({});
    const parsed = taskListQuerySchema.parse({ status: "done" });
    expect(parsed.status).toBe("done");
  });
});

describe("userCreateSchema", () => {
  it("folds emails to lower case and defaults the role", () => {
    const parsed = userCreateSchema.parse({ name: "Ada", email: "Ada@Example.COM" });
    expect(parsed.email).toBe("ada@example.com");
    expect(parsed.role).toBe("member");
  });

  it("rejects malformed emails", () => {
    expect(() => userCreateSchema.parse({ name: "Ada", email: "not-an-email" })).toThrow();
  });
});
