import { Router } from "express";
import * as userService from "../services/userService";
import { userCreateSchema } from "../services/validation";

/**
 * User registry endpoints. Deletion is a soft-delete (deactivation) because
 * tasks keep referencing assignee ids; see userService for the rationale.
 */
export function usersRouter(): Router {
  const router = Router();

  router.get("/", (_req, res) => {
    res.json({ users: userService.listUsers() });
  });

  router.post("/", (req, res, next) => {
    try {
      const input = userCreateSchema.parse(req.body);
      res.status(201).json(userService.createUser(input));
    } catch (err) {
      next(err);
    }
  });

  router.get("/:id", (req, res, next) => {
    try {
      res.json(userService.getUser(req.params.id));
    } catch (err) {
      next(err);
    }
  });

  router.delete("/:id", (req, res, next) => {
    try {
      res.json(userService.deactivateUser(req.params.id));
    } catch (err) {
      next(err);
    }
  });

  return router;
}
