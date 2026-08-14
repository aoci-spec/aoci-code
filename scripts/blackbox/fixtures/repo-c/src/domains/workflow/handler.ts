import { NextFunction, Request, Response } from "express";
import { WorkflowService } from "./service";
import {
  parseWorkflowCreate,
  parseWorkflowLabel,
  parseWorkflowPage,
  parseWorkflowTransition,
} from "./validator";
import { toWorkflowPagePayload, toWorkflowPayload, toWorkflowSummaryPayload } from "./mapper";
import { assertWorkflowAccess } from "./policy";

function tenantOf(request: Request): string {
  return String(request.header("x-tenant") ?? "");
}

/** HTTP surface for workflow instance resources. */
export function makeWorkflowHandlers(service: WorkflowService) {
  return {
    create(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertWorkflowAccess(tenantId, "write");
        const input = parseWorkflowCreate(request.body);
        const created = service.create(tenantId, input.id, input.reference, input.amountCents);
        response.status(201).json(toWorkflowPayload(created));
      } catch (error) {
        next(error);
      }
    },

    get(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertWorkflowAccess(tenantId, "read");
        response.json(toWorkflowPayload(service.get(tenantId, String(request.params.id))));
      } catch (error) {
        next(error);
      }
    },

    transition(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertWorkflowAccess(tenantId, "write");
        const input = parseWorkflowTransition(request.body);
        const updated = service.transition(tenantId, String(request.params.id), input.status);
        response.json(toWorkflowPayload(updated));
      } catch (error) {
        next(error);
      }
    },

    label(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertWorkflowAccess(tenantId, "write");
        const input = parseWorkflowLabel(request.body);
        response.json(toWorkflowPayload(service.label(tenantId, String(request.params.id), input.label)));
      } catch (error) {
        next(error);
      }
    },

    list(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertWorkflowAccess(tenantId, "read");
        const page = parseWorkflowPage(request.query);
        response.json(toWorkflowPagePayload(service.page(tenantId, page.offset, page.limit)));
      } catch (error) {
        next(error);
      }
    },

    outstanding(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertWorkflowAccess(tenantId, "read");
        response.json(service.outstanding(tenantId).map(toWorkflowPayload));
      } catch (error) {
        next(error);
      }
    },

    summary(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertWorkflowAccess(tenantId, "read");
        response.json(toWorkflowSummaryPayload(service.summary(tenantId)));
      } catch (error) {
        next(error);
      }
    },

    discard(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertWorkflowAccess(tenantId, "write");
        service.discard(tenantId, String(request.params.id));
        response.status(204).end();
      } catch (error) {
        next(error);
      }
    },
  };
}
