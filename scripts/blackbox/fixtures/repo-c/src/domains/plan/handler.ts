import { NextFunction, Request, Response } from "express";
import { PlanService } from "./service";
import {
  parsePlanCreate,
  parsePlanLabel,
  parsePlanPage,
  parsePlanTransition,
} from "./validator";
import { toPlanPagePayload, toPlanPayload, toPlanSummaryPayload } from "./mapper";
import { assertPlanAccess } from "./policy";

function tenantOf(request: Request): string {
  return String(request.header("x-tenant") ?? "");
}

/** HTTP surface for subscription plan resources. */
export function makePlanHandlers(service: PlanService) {
  return {
    create(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertPlanAccess(tenantId, "write");
        const input = parsePlanCreate(request.body);
        const created = service.create(tenantId, input.id, input.reference, input.amountCents);
        response.status(201).json(toPlanPayload(created));
      } catch (error) {
        next(error);
      }
    },

    get(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertPlanAccess(tenantId, "read");
        response.json(toPlanPayload(service.get(tenantId, String(request.params.id))));
      } catch (error) {
        next(error);
      }
    },

    transition(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertPlanAccess(tenantId, "write");
        const input = parsePlanTransition(request.body);
        const updated = service.transition(tenantId, String(request.params.id), input.status);
        response.json(toPlanPayload(updated));
      } catch (error) {
        next(error);
      }
    },

    label(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertPlanAccess(tenantId, "write");
        const input = parsePlanLabel(request.body);
        response.json(toPlanPayload(service.label(tenantId, String(request.params.id), input.label)));
      } catch (error) {
        next(error);
      }
    },

    list(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertPlanAccess(tenantId, "read");
        const page = parsePlanPage(request.query);
        response.json(toPlanPagePayload(service.page(tenantId, page.offset, page.limit)));
      } catch (error) {
        next(error);
      }
    },

    outstanding(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertPlanAccess(tenantId, "read");
        response.json(service.outstanding(tenantId).map(toPlanPayload));
      } catch (error) {
        next(error);
      }
    },

    summary(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertPlanAccess(tenantId, "read");
        response.json(toPlanSummaryPayload(service.summary(tenantId)));
      } catch (error) {
        next(error);
      }
    },

    discard(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertPlanAccess(tenantId, "write");
        service.discard(tenantId, String(request.params.id));
        response.status(204).end();
      } catch (error) {
        next(error);
      }
    },
  };
}
