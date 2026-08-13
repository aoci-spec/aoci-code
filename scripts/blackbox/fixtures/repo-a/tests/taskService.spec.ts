import { beforeEach, describe, expect, it } from "vitest";
import { IllegalTransitionError, NotFoundError } from "../src/middleware/errors";
import {
  canTransition,
  createTask,
  deleteTask,
  getTask,
  listTasks,
  resetTasks,
  transitionTask,
} from "../src/services/taskService";
import { taskCreateSchema } from "../src/services/validation";

/** Builds a fully-defaulted create input the same way the route layer does. */
function input(overrides: Record<string, unknown> = {}) {
  return taskCreateSchema.parse({ title: "write release notes", ...overrides });
}

beforeEach(() => {
  resetTasks();
});

describe("createTask", () => {
  it("assigns a prefixed id, todo status, and matching timestamps", () => {
    const task = createTask(input());
    expect(task.id).toMatch(/^task_[0-9A-HJKMNP-TV-Z]{26}$/);
    expect(task.status).toBe("todo");
    expect(task.createdAt).toBe(task.updatedAt);
    expect(getTask(task.id)).toEqual(task);
  });
});

describe("transitionTask", () => {
  it("walks the happy path todo -> doing -> done", () => {
    const task = createTask(input());
    expect(transitionTask(task.id, "doing").status).toBe("doing");
    expect(transitionTask(task.id, "done").status).toBe("done");
  });

  it("rejects skipping straight from todo to done", () => {
    const task = createTask(input());
    expect(() => transitionTask(task.id, "done")).toThrow(IllegalTransitionError);
    expect(getTask(task.id).status).toBe("todo");
  });

  it("treats done as terminal", () => {
    const task = createTask(input());
    transitionTask(task.id, "doing");
    transitionTask(task.id, "done");
    expect(() => transitionTask(task.id, "doing")).toThrow(IllegalTransitionError);
  });

  it("allows stepping back from doing to todo only", () => {
    expect(canTransition("doing", "todo")).toBe(true);
    expect(canTransition("done", "todo")).toBe(false);
  });
});

describe("listTasks", () => {
  it("filters by status and assignee independently", () => {
    const mine = createTask(input({ assigneeId: "user_01J1GZ4Q0RVAHT3M8W2E9XKCPD" }));
    createTask(input({ title: "unassigned chore" }));
    transitionTask(mine.id, "doing");
    expect(listTasks({ status: "doing" }).map((t) => t.id)).toEqual([mine.id]);
    expect(listTasks({ assigneeId: "user_01J1GZ4Q0RVAHT3M8W2E9XKCPD" })).toHaveLength(1);
    expect(listTasks()).toHaveLength(2);
  });
});

describe("deleteTask", () => {
  it("removes the task and errors on unknown ids", () => {
    const task = createTask(input());
    deleteTask(task.id);
    expect(() => getTask(task.id)).toThrow(NotFoundError);
    expect(() => deleteTask(task.id)).toThrow(NotFoundError);
  });
});
