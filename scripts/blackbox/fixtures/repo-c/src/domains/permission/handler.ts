import { NextFunction, Request, Response } from "express";
import { PermissionService } from "./service";
import {
  parsePermissionCreate,
  parsePermissionLabel,
  parsePermissionPage,
  parsePermissionTransition,
} from "./validator";
import { toPermissionPagePayload, toPermissionPayload, toPermissionSummaryPayload } from "./mapper";
import { assertPermissionAccess } from "./policy";

function tenantOf(request: Request): string {
  return String(request.header("x-tenant") ?? "");
}

/** HTTP surface for permission grant resources. */
export function makePermissionHandlers(service: PermissionService) {
  return {
    create(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertPermissionAccess(tenantId, "write");
        const input = parsePermissionCreate(request.body);
        const created = service.create(tenantId, input.id, input.reference, input.amountCents);
        response.status(201).json(toPermissionPayload(created));
      } catch (error) {
        next(error);
      }
    },

    get(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertPermissionAccess(tenantId, "read");
        response.json(toPermissionPayload(service.get(tenantId, String(request.params.id))));
      } catch (error) {
        next(error);
      }
    },

    transition(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertPermissionAccess(tenantId, "write");
        const input = parsePermissionTransition(request.body);
        const updated = service.transition(tenantId, String(request.params.id), input.status);
        response.json(toPermissionPayload(updated));
      } catch (error) {
        next(error);
      }
    },

    label(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertPermissionAccess(tenantId, "write");
        const input = parsePermissionLabel(request.body);
        response.json(toPermissionPayload(service.label(tenantId, String(request.params.id), input.label)));
      } catch (error) {
        next(error);
      }
    },

    list(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertPermissionAccess(tenantId, "read");
        const page = parsePermissionPage(request.query);
        response.json(toPermissionPagePayload(service.page(tenantId, page.offset, page.limit)));
      } catch (error) {
        next(error);
      }
    },

    outstanding(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertPermissionAccess(tenantId, "read");
        response.json(service.outstanding(tenantId).map(toPermissionPayload));
      } catch (error) {
        next(error);
      }
    },

    summary(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertPermissionAccess(tenantId, "read");
        response.json(toPermissionSummaryPayload(service.summary(tenantId)));
      } catch (error) {
        next(error);
      }
    },

    discard(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertPermissionAccess(tenantId, "write");
        service.discard(tenantId, String(request.params.id));
        response.status(204).end();
      } catch (error) {
        next(error);
      }
    },
  };
}
