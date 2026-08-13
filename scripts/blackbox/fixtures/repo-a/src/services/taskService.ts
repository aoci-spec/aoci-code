import { IllegalTransitionError, NotFoundError } from "../middleware/errors";
import { newId } from "../utils/id";
import { nowIso } from "../utils/time";
import type {
  TaskCreateInput,
  TaskListQuery,
  TaskPriority,
  TaskStatus,
  TaskUpdateInput,
} from "./validation";

export interface Task {
  readonly id: string;
  title: string;
  description: string;
  status: TaskStatus;
  priority: TaskPriority;
  assigneeId: string | null;
  readonly createdAt: string;
  updatedAt: string;
}

/**
 * The legal state machine: work moves forward `todo -> doing -> done`, and
 * `doing -> todo` is the only step back. `done` is terminal.
 */
const LEGAL_TRANSITIONS: Record<TaskStatus, readonly TaskStatus[]> = {
  todo: ["doing"],
  doing: ["done", "todo"],
  done: [],
};

const store = new Map<string, Task>();

export function createTask(input: TaskCreateInput): Task {
  const timestamp = nowIso();
  const task: Task = {
    id: newId("task"),
    title: input.title,
    description: input.description,
    status: "todo",
    priority: input.priority,
    assigneeId: input.assigneeId ?? null,
    createdAt: timestamp,
    updatedAt: timestamp,
  };
  store.set(task.id, task);
  return task;
}

export function getTask(id: string): Task {
  const task = store.get(id);
  if (!task) {
    throw new NotFoundError(`task ${id} does not exist`);
  }
  return task;
}

/** Lists tasks matching the query, ordered by id (i.e. by creation instant). */
export function listTasks(query: TaskListQuery = {}): Task[] {
  const matches = [...store.values()].filter((task) => {
    if (query.status !== undefined && task.status !== query.status) {
      return false;
    }
    if (query.assigneeId !== undefined && task.assigneeId !== query.assigneeId) {
      return false;
    }
    return true;
  });
  return matches.sort((a, b) => a.id.localeCompare(b.id));
}

export function updateTask(id: string, patch: TaskUpdateInput): Task {
  const task = getTask(id);
  if (patch.title !== undefined) task.title = patch.title;
  if (patch.description !== undefined) task.description = patch.description;
  if (patch.priority !== undefined) task.priority = patch.priority;
  if (patch.assigneeId !== undefined) task.assigneeId = patch.assigneeId;
  task.updatedAt = nowIso();
  return task;
}

export function canTransition(from: TaskStatus, to: TaskStatus): boolean {
  return LEGAL_TRANSITIONS[from].includes(to);
}

export function transitionTask(id: string, to: TaskStatus): Task {
  const task = getTask(id);
  if (!canTransition(task.status, to)) {
    throw new IllegalTransitionError(
      `cannot move task ${id} from "${task.status}" to "${to}"`,
    );
  }
  task.status = to;
  task.updatedAt = nowIso();
  return task;
}

export function deleteTask(id: string): void {
  if (!store.delete(id)) {
    throw new NotFoundError(`task ${id} does not exist`);
  }
}

/** Test hook: empties the store so each spec starts from a known state. */
export function resetTasks(): void {
  store.clear();
}
