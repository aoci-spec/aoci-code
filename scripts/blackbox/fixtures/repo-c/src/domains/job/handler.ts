import { NextFunction, Request, Response } from "express";
import { JobService } from "./service";
import {
  parseJobCreate,
  parseJobLabel,
  parseJobPage,
  parseJobTransition,
} from "./validator";
import { toJobPagePayload, toJobPayload, toJobSummaryPayload } from "./mapper";
import { assertJobAccess } from "./policy";

function tenantOf(request: Request): string {
  return String(request.header("x-tenant") ?? "");
}

/** HTTP surface for background job resources. */
export function makeJobHandlers(service: JobService) {
  return {
    create(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertJobAccess(tenantId, "write");
        const input = parseJobCreate(request.body);
        const created = service.create(tenantId, input.id, input.reference, input.amountCents);
        response.status(201).json(toJobPayload(created));
      } catch (error) {
        next(error);
      }
    },

    get(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertJobAccess(tenantId, "read");
        response.json(toJobPayload(service.get(tenantId, String(request.params.id))));
      } catch (error) {
        next(error);
      }
    },

    transition(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertJobAccess(tenantId, "write");
        const input = parseJobTransition(request.body);
        const updated = service.transition(tenantId, String(request.params.id), input.status);
        response.json(toJobPayload(updated));
      } catch (error) {
        next(error);
      }
    },

    label(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertJobAccess(tenantId, "write");
        const input = parseJobLabel(request.body);
        response.json(toJobPayload(service.label(tenantId, String(request.params.id), input.label)));
      } catch (error) {
        next(error);
      }
    },

    list(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertJobAccess(tenantId, "read");
        const page = parseJobPage(request.query);
        response.json(toJobPagePayload(service.page(tenantId, page.offset, page.limit)));
      } catch (error) {
        next(error);
      }
    },

    outstanding(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertJobAccess(tenantId, "read");
        response.json(service.outstanding(tenantId).map(toJobPayload));
      } catch (error) {
        next(error);
      }
    },

    summary(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertJobAccess(tenantId, "read");
        response.json(toJobSummaryPayload(service.summary(tenantId)));
      } catch (error) {
        next(error);
      }
    },

    discard(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertJobAccess(tenantId, "write");
        service.discard(tenantId, String(request.params.id));
        response.status(204).end();
      } catch (error) {
        next(error);
      }
    },
  };
}
