import { NextFunction, Request, Response } from "express";
import { TaskService } from "./service";
import {
  parseTaskCreate,
  parseTaskLabel,
  parseTaskPage,
  parseTaskTransition,
} from "./validator";
import { toTaskPagePayload, toTaskPayload, toTaskSummaryPayload } from "./mapper";
import { assertTaskAccess } from "./policy";

function tenantOf(request: Request): string {
  return String(request.header("x-tenant") ?? "");
}

/** HTTP surface for workflow task resources. */
export function makeTaskHandlers(service: TaskService) {
  return {
    create(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertTaskAccess(tenantId, "write");
        const input = parseTaskCreate(request.body);
        const created = service.create(tenantId, input.id, input.reference, input.amountCents);
        response.status(201).json(toTaskPayload(created));
      } catch (error) {
        next(error);
      }
    },

    get(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertTaskAccess(tenantId, "read");
        response.json(toTaskPayload(service.get(tenantId, String(request.params.id))));
      } catch (error) {
        next(error);
      }
    },

    transition(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertTaskAccess(tenantId, "write");
        const input = parseTaskTransition(request.body);
        const updated = service.transition(tenantId, String(request.params.id), input.status);
        response.json(toTaskPayload(updated));
      } catch (error) {
        next(error);
      }
    },

    label(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertTaskAccess(tenantId, "write");
        const input = parseTaskLabel(request.body);
        response.json(toTaskPayload(service.label(tenantId, String(request.params.id), input.label)));
      } catch (error) {
        next(error);
      }
    },

    list(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertTaskAccess(tenantId, "read");
        const page = parseTaskPage(request.query);
        response.json(toTaskPagePayload(service.page(tenantId, page.offset, page.limit)));
      } catch (error) {
        next(error);
      }
    },

    outstanding(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertTaskAccess(tenantId, "read");
        response.json(service.outstanding(tenantId).map(toTaskPayload));
      } catch (error) {
        next(error);
      }
    },

    summary(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertTaskAccess(tenantId, "read");
        response.json(toTaskSummaryPayload(service.summary(tenantId)));
      } catch (error) {
        next(error);
      }
    },

    discard(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertTaskAccess(tenantId, "write");
        service.discard(tenantId, String(request.params.id));
        response.status(204).end();
      } catch (error) {
        next(error);
      }
    },
  };
}
