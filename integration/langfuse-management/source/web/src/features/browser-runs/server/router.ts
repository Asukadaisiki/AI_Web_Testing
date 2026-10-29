import type { Session } from "next-auth";
import { z } from "zod/v4";
import { throwIfNoProjectAccess } from "@/src/features/rbac/utils/checkProjectAccess";
import {
  createTRPCRouter,
  protectedProjectProcedure,
} from "@/src/server/api/trpc";
import { clientForProject } from "./project-client";
import { CreateRunSchema, HarnessDefinitionSchema } from "../contracts";
import {
  SkillUploadSchema,
  McpBindingInputSchema,
} from "../management-contracts";

const projectInput = z.object({ projectId: z.string() });
const conversationInput = projectInput.extend({
  conversationId: z.string().min(1),
});
const environmentInput = conversationInput.extend({
  environmentId: z.string().min(1),
});
const runInput = projectInput.extend({ runId: z.string().min(1) });
const rerunInput = runInput.extend({ idempotencyKey: z.string().min(1) });

const assertExecute = (session: Session | null, projectId: string) =>
  throwIfNoProjectAccess({
    session,
    projectId,
    scope: "playground:execute",
  });

const adminProcedure = protectedProjectProcedure
  .input(projectInput)
  .use(({ input, ctx, next }) => {
    throwIfNoProjectAccess({
      session: ctx.session,
      projectId: input.projectId,
      scope: "project:update",
    });
    return next();
  });
const executeProcedure = protectedProjectProcedure
  .input(projectInput)
  .use(({ input, ctx, next }) => {
    assertExecute(ctx.session, input.projectId);
    return next();
  });

export const browserRunsRouter = createTRPCRouter({
  conversations: protectedProjectProcedure
    .input(projectInput)
    .query(async ({ input, ctx }) =>
      (
        await clientForProject(input.projectId, ctx.session)
      ).listConversations(),
    ),
  createConversation: executeProcedure
    .input(z.object({ name: z.string().trim().min(1).max(200) }))
    .mutation(async ({ input, ctx }) =>
      (await clientForProject(input.projectId, ctx.session)).createConversation(
        input.name,
      ),
    ),
  updateConversation: executeProcedure
    .input(conversationInput.extend({ status: z.enum(["active", "archived"]) }))
    .mutation(async ({ input, ctx }) =>
      (await clientForProject(input.projectId, ctx.session)).updateConversation(
        input.conversationId,
        input.status,
      ),
    ),
  deleteConversation: executeProcedure
    .input(conversationInput)
    .mutation(async ({ input, ctx }) =>
      (await clientForProject(input.projectId, ctx.session)).deleteConversation(
        input.conversationId,
      ),
    ),
  environment: protectedProjectProcedure
    .input(conversationInput)
    .query(async ({ input, ctx }) =>
      (await clientForProject(input.projectId, ctx.session)).getEnvironment(
        input.conversationId,
      ),
    ),
  resetEnvironment: executeProcedure
    .input(environmentInput)
    .mutation(async ({ input, ctx }) =>
      (await clientForProject(input.projectId, ctx.session)).environmentAction(
        input.conversationId,
        input.environmentId,
        "reset",
      ),
    ),
  reviewEnvironment: executeProcedure
    .input(environmentInput)
    .mutation(async ({ input, ctx }) =>
      (await clientForProject(input.projectId, ctx.session)).environmentAction(
        input.conversationId,
        input.environmentId,
        "review",
      ),
    ),
  startControl: executeProcedure
    .input(environmentInput)
    .mutation(async ({ input, ctx }) =>
      (await clientForProject(input.projectId, ctx.session)).startControl(
        input.conversationId,
        input.environmentId,
      ),
    ),
  stopControl: executeProcedure
    .input(z.object({ controlSessionId: z.string().min(1) }))
    .mutation(async ({ input, ctx }) =>
      (await clientForProject(input.projectId, ctx.session)).stopControl(
        input.controlSessionId,
      ),
    ),
  skills: protectedProjectProcedure
    .input(projectInput)
    .query(async ({ input, ctx }) =>
      (await clientForProject(input.projectId, ctx.session)).listSkills(),
    ),
  uploadSkill: executeProcedure
    .input(z.object({ upload: SkillUploadSchema }))
    .mutation(async ({ input, ctx }) =>
      (await clientForProject(input.projectId, ctx.session)).uploadSkill(
        input.upload,
      ),
    ),
  publishSkill: adminProcedure
    .input(
      z.object({ skillId: z.string().min(1), versionId: z.string().min(1) }),
    )
    .mutation(async ({ input, ctx }) =>
      (await clientForProject(input.projectId, ctx.session)).publishSkill(
        input.skillId,
        input.versionId,
      ),
    ),
  revokeSkill: adminProcedure
    .input(z.object({ versionId: z.string().min(1) }))
    .mutation(async ({ input, ctx }) =>
      (await clientForProject(input.projectId, ctx.session)).revokeSkill(
        input.versionId,
      ),
    ),
  mcpDefinitions: adminProcedure.query(async ({ input, ctx }) =>
    (await clientForProject(input.projectId, ctx.session)).listMcpDefinitions(),
  ),
  mcpBindings: adminProcedure.query(async ({ input, ctx }) =>
    (await clientForProject(input.projectId, ctx.session)).listMcpBindings(),
  ),
  saveMcpBinding: adminProcedure
    .input(
      z.object({
        bindingId: z.string().min(1).optional(),
        binding: McpBindingInputSchema,
      }),
    )
    .mutation(async ({ input, ctx }) =>
      (await clientForProject(input.projectId, ctx.session)).saveMcpBinding(
        input.binding,
        input.bindingId,
      ),
    ),
  services: adminProcedure.query(async ({ input, ctx }) =>
    (await clientForProject(input.projectId, ctx.session)).services(),
  ),
  drainWorker: adminProcedure
    .input(z.object({ workerId: z.string().min(1), drain: z.boolean() }))
    .mutation(async ({ input, ctx }) =>
      (await clientForProject(input.projectId, ctx.session)).drainWorker(
        input.workerId,
        input.drain,
      ),
    ),
  list: protectedProjectProcedure
    .input(conversationInput)
    .query(async ({ input, ctx }) =>
      (await clientForProject(input.projectId, ctx.session)).listRuns(
        input.conversationId,
      ),
    ),

  runtimes: protectedProjectProcedure
    .input(projectInput)
    .query(async ({ input, ctx }) =>
      (await clientForProject(input.projectId, ctx.session)).listRuntimes(),
    ),

  harnesses: protectedProjectProcedure
    .input(projectInput)
    .query(async ({ input, ctx }) =>
      (await clientForProject(input.projectId, ctx.session)).listHarnesses(),
    ),

  createHarness: adminProcedure
    .input(projectInput.extend({ definition: HarnessDefinitionSchema }))
    .mutation(async ({ input, ctx }) => {
      throwIfNoProjectAccess({
        session: ctx.session,
        projectId: input.projectId,
        scope: "project:update",
      });
      return (
        await clientForProject(input.projectId, ctx.session)
      ).createHarness(input.definition);
    }),

  updateHarness: adminProcedure
    .input(
      projectInput.extend({
        harnessId: z.string().min(1),
        definition: HarnessDefinitionSchema,
      }),
    )
    .mutation(async ({ input, ctx }) => {
      throwIfNoProjectAccess({
        session: ctx.session,
        projectId: input.projectId,
        scope: "project:update",
      });
      return (
        await clientForProject(input.projectId, ctx.session)
      ).updateHarness(input.harnessId, input.definition);
    }),

  create: protectedProjectProcedure
    .input(CreateRunSchema)
    .mutation(async ({ input, ctx }) => {
      assertExecute(ctx.session, input.projectId);
      return (await clientForProject(input.projectId, ctx.session)).createRun(
        input,
      );
    }),

  get: protectedProjectProcedure
    .input(runInput)
    .query(async ({ input, ctx }) =>
      (await clientForProject(input.projectId, ctx.session)).getRun(
        input.runId,
      ),
    ),

  trace: protectedProjectProcedure
    .input(runInput)
    .query(async ({ input, ctx }) =>
      (await clientForProject(input.projectId, ctx.session)).getTrace(
        input.runId,
      ),
    ),

  cancel: protectedProjectProcedure
    .input(runInput)
    .mutation(async ({ input, ctx }) => {
      assertExecute(ctx.session, input.projectId);
      return (await clientForProject(input.projectId, ctx.session)).cancelRun(
        input.runId,
      );
    }),

  rerun: protectedProjectProcedure
    .input(rerunInput)
    .mutation(async ({ input, ctx }) => {
      assertExecute(ctx.session, input.projectId);
      return (await clientForProject(input.projectId, ctx.session)).rerun(
        input.runId,
        input.idempotencyKey,
      );
    }),

  authorizeEvents: protectedProjectProcedure
    .input(runInput)
    .query(async ({ input, ctx }) =>
      (await clientForProject(input.projectId, ctx.session)).authorizeEvents(
        input.runId,
      ),
    ),
});
