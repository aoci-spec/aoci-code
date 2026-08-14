import { NextFunction, Request, Response } from "express";
import { ScheduleService } from "./service";
import {
  parseScheduleCreate,
  parseScheduleLabel,
  parseSchedulePage,
  parseScheduleTransition,
} from "./validator";
import { toSchedulePagePayload, toSchedulePayload, toScheduleSummaryPayload } from "./mapper";
import { assertScheduleAccess } from "./policy";

function tenantOf(request: Request): string {
  return String(request.header("x-tenant") ?? "");
}

/** HTTP surface for scheduled run resources. */
export function makeScheduleHandlers(service: ScheduleService) {
  return {
    create(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertScheduleAccess(tenantId, "write");
        const input = parseScheduleCreate(request.body);
        const created = service.create(tenantId, input.id, input.reference, input.amountCents);
        response.status(201).json(toSchedulePayload(created));
      } catch (error) {
        next(error);
      }
    },

    get(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertScheduleAccess(tenantId, "read");
        response.json(toSchedulePayload(service.get(tenantId, String(request.params.id))));
      } catch (error) {
        next(error);
      }
    },

    transition(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertScheduleAccess(tenantId, "write");
        const input = parseScheduleTransition(request.body);
        const updated = service.transition(tenantId, String(request.params.id), input.status);
        response.json(toSchedulePayload(updated));
      } catch (error) {
        next(error);
      }
    },

    label(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertScheduleAccess(tenantId, "write");
        const input = parseScheduleLabel(request.body);
        response.json(toSchedulePayload(service.label(tenantId, String(request.params.id), input.label)));
      } catch (error) {
        next(error);
      }
    },

    list(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertScheduleAccess(tenantId, "read");
        const page = parseSchedulePage(request.query);
        response.json(toSchedulePagePayload(service.page(tenantId, page.offset, page.limit)));
      } catch (error) {
        next(error);
      }
    },

    outstanding(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertScheduleAccess(tenantId, "read");
        response.json(service.outstanding(tenantId).map(toSchedulePayload));
      } catch (error) {
        next(error);
      }
    },

    summary(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertScheduleAccess(tenantId, "read");
        response.json(toScheduleSummaryPayload(service.summary(tenantId)));
      } catch (error) {
        next(error);
      }
    },

    discard(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertScheduleAccess(tenantId, "write");
        service.discard(tenantId, String(request.params.id));
        response.status(204).end();
      } catch (error) {
        next(error);
      }
    },
  };
}
