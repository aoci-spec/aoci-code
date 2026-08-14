import { NextFunction, Request, Response } from "express";
import { UsageService } from "./service";
import {
  parseUsageCreate,
  parseUsageLabel,
  parseUsagePage,
  parseUsageTransition,
} from "./validator";
import { toUsagePagePayload, toUsagePayload, toUsageSummaryPayload } from "./mapper";
import { assertUsageAccess } from "./policy";

function tenantOf(request: Request): string {
  return String(request.header("x-tenant") ?? "");
}

/** HTTP surface for metered usage record resources. */
export function makeUsageHandlers(service: UsageService) {
  return {
    create(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertUsageAccess(tenantId, "write");
        const input = parseUsageCreate(request.body);
        const created = service.create(tenantId, input.id, input.reference, input.amountCents);
        response.status(201).json(toUsagePayload(created));
      } catch (error) {
        next(error);
      }
    },

    get(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertUsageAccess(tenantId, "read");
        response.json(toUsagePayload(service.get(tenantId, String(request.params.id))));
      } catch (error) {
        next(error);
      }
    },

    transition(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertUsageAccess(tenantId, "write");
        const input = parseUsageTransition(request.body);
        const updated = service.transition(tenantId, String(request.params.id), input.status);
        response.json(toUsagePayload(updated));
      } catch (error) {
        next(error);
      }
    },

    label(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertUsageAccess(tenantId, "write");
        const input = parseUsageLabel(request.body);
        response.json(toUsagePayload(service.label(tenantId, String(request.params.id), input.label)));
      } catch (error) {
        next(error);
      }
    },

    list(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertUsageAccess(tenantId, "read");
        const page = parseUsagePage(request.query);
        response.json(toUsagePagePayload(service.page(tenantId, page.offset, page.limit)));
      } catch (error) {
        next(error);
      }
    },

    outstanding(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertUsageAccess(tenantId, "read");
        response.json(service.outstanding(tenantId).map(toUsagePayload));
      } catch (error) {
        next(error);
      }
    },

    summary(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertUsageAccess(tenantId, "read");
        response.json(toUsageSummaryPayload(service.summary(tenantId)));
      } catch (error) {
        next(error);
      }
    },

    discard(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertUsageAccess(tenantId, "write");
        service.discard(tenantId, String(request.params.id));
        response.status(204).end();
      } catch (error) {
        next(error);
      }
    },
  };
}
