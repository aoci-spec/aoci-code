import { Router } from "express";
import * as taskService from "../services/taskService";
import {
  taskCreateSchema,
  taskListQuerySchema,
  taskTransitionSchema,
  taskUpdateSchema,
} from "../services/validation";

/**
 * CRUD plus lifecycle transitions for tasks. Every payload goes through the
 * shared zod schemas; service-layer errors (not found, illegal transition)
 * propagate to the error middleware via `next`.
 */
export function tasksRouter(): Router {
  const router = Router();

  router.get("/", (req, res, next) => {
    try {
      const query = taskListQuerySchema.parse(req.query);
      res.json({ tasks: taskService.listTasks(query) });
    } catch (err) {
      next(err);
    }
  });

  router.post("/", (req, res, next) => {
    try {
      const input = taskCreateSchema.parse(req.body);
      res.status(201).json(taskService.createTask(input));
    } catch (err) {
      next(err);
    }
  });

  router.get("/:id", (req, res, next) => {
    try {
      res.json(taskService.getTask(req.params.id));
    } catch (err) {
      next(err);
    }
  });

  router.patch("/:id", (req, res, next) => {
    try {
      const patch = taskUpdateSchema.parse(req.body);
      res.json(taskService.updateTask(req.params.id, patch));
    } catch (err) {
      next(err);
    }
  });

  router.post("/:id/transition", (req, res, next) => {
    try {
      const { status } = taskTransitionSchema.parse(req.body);
      res.json(taskService.transitionTask(req.params.id, status));
    } catch (err) {
      next(err);
    }
  });

  router.delete("/:id", (req, res, next) => {
    try {
      taskService.deleteTask(req.params.id);
      res.status(204).end();
    } catch (err) {
      next(err);
    }
  });

  return router;
}
