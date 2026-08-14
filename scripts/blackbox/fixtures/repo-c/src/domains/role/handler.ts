import { NextFunction, Request, Response } from "express";
import { RoleService } from "./service";
import {
  parseRoleCreate,
  parseRoleLabel,
  parseRolePage,
  parseRoleTransition,
} from "./validator";
import { toRolePagePayload, toRolePayload, toRoleSummaryPayload } from "./mapper";
import { assertRoleAccess } from "./policy";

function tenantOf(request: Request): string {
  return String(request.header("x-tenant") ?? "");
}

/** HTTP surface for authorization role resources. */
export function makeRoleHandlers(service: RoleService) {
  return {
    create(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertRoleAccess(tenantId, "write");
        const input = parseRoleCreate(request.body);
        const created = service.create(tenantId, input.id, input.reference, input.amountCents);
        response.status(201).json(toRolePayload(created));
      } catch (error) {
        next(error);
      }
    },

    get(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertRoleAccess(tenantId, "read");
        response.json(toRolePayload(service.get(tenantId, String(request.params.id))));
      } catch (error) {
        next(error);
      }
    },

    transition(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertRoleAccess(tenantId, "write");
        const input = parseRoleTransition(request.body);
        const updated = service.transition(tenantId, String(request.params.id), input.status);
        response.json(toRolePayload(updated));
      } catch (error) {
        next(error);
      }
    },

    label(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertRoleAccess(tenantId, "write");
        const input = parseRoleLabel(request.body);
        response.json(toRolePayload(service.label(tenantId, String(request.params.id), input.label)));
      } catch (error) {
        next(error);
      }
    },

    list(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertRoleAccess(tenantId, "read");
        const page = parseRolePage(request.query);
        response.json(toRolePagePayload(service.page(tenantId, page.offset, page.limit)));
      } catch (error) {
        next(error);
      }
    },

    outstanding(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertRoleAccess(tenantId, "read");
        response.json(service.outstanding(tenantId).map(toRolePayload));
      } catch (error) {
        next(error);
      }
    },

    summary(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertRoleAccess(tenantId, "read");
        response.json(toRoleSummaryPayload(service.summary(tenantId)));
      } catch (error) {
        next(error);
      }
    },

    discard(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertRoleAccess(tenantId, "write");
        service.discard(tenantId, String(request.params.id));
        response.status(204).end();
      } catch (error) {
        next(error);
      }
    },
  };
}
