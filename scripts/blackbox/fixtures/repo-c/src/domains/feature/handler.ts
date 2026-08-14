import { NextFunction, Request, Response } from "express";
import { FeatureService } from "./service";
import {
  parseFeatureCreate,
  parseFeatureLabel,
  parseFeaturePage,
  parseFeatureTransition,
} from "./validator";
import { toFeaturePagePayload, toFeaturePayload, toFeatureSummaryPayload } from "./mapper";
import { assertFeatureAccess } from "./policy";

function tenantOf(request: Request): string {
  return String(request.header("x-tenant") ?? "");
}

/** HTTP surface for feature flag resources. */
export function makeFeatureHandlers(service: FeatureService) {
  return {
    create(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertFeatureAccess(tenantId, "write");
        const input = parseFeatureCreate(request.body);
        const created = service.create(tenantId, input.id, input.reference, input.amountCents);
        response.status(201).json(toFeaturePayload(created));
      } catch (error) {
        next(error);
      }
    },

    get(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertFeatureAccess(tenantId, "read");
        response.json(toFeaturePayload(service.get(tenantId, String(request.params.id))));
      } catch (error) {
        next(error);
      }
    },

    transition(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertFeatureAccess(tenantId, "write");
        const input = parseFeatureTransition(request.body);
        const updated = service.transition(tenantId, String(request.params.id), input.status);
        response.json(toFeaturePayload(updated));
      } catch (error) {
        next(error);
      }
    },

    label(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertFeatureAccess(tenantId, "write");
        const input = parseFeatureLabel(request.body);
        response.json(toFeaturePayload(service.label(tenantId, String(request.params.id), input.label)));
      } catch (error) {
        next(error);
      }
    },

    list(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertFeatureAccess(tenantId, "read");
        const page = parseFeaturePage(request.query);
        response.json(toFeaturePagePayload(service.page(tenantId, page.offset, page.limit)));
      } catch (error) {
        next(error);
      }
    },

    outstanding(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertFeatureAccess(tenantId, "read");
        response.json(service.outstanding(tenantId).map(toFeaturePayload));
      } catch (error) {
        next(error);
      }
    },

    summary(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertFeatureAccess(tenantId, "read");
        response.json(toFeatureSummaryPayload(service.summary(tenantId)));
      } catch (error) {
        next(error);
      }
    },

    discard(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertFeatureAccess(tenantId, "write");
        service.discard(tenantId, String(request.params.id));
        response.status(204).end();
      } catch (error) {
        next(error);
      }
    },
  };
}
