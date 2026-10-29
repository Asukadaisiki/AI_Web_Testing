import { TRPCError } from "@trpc/server";
import { z } from "zod/v4";
import {
  HarnessDefinitionSchema,
  HarnessSnapshotSchema,
  type HarnessDefinition,
} from "../contracts";
import {
  ConversationSchema,
  EnvironmentSchema,
  SkillSchema,
  SkillUploadSchema,
  SkillVersionSchema,
  McpBindingSchema,
  McpBindingInputSchema,
  McpDefinitionSchema,
  ServicesSchema,
  WorkerSchema,
  ControlSessionSchema,
} from "../management-contracts";
import { ControlApi } from "./control-api";

const body = (data: unknown, method = "POST") => ({
  method,
  body: JSON.stringify(data),
});
const enc = encodeURIComponent;

export class ManagementClient extends ControlApi {
  async listConversations() {
    const result = await this.request(
      this.projectPath("conversations"),
      z.object({ conversations: z.array(ConversationSchema) }),
    );
    result.conversations.forEach((item) => this.assertProject(item));
    return result.conversations;
  }
  async createConversation(name: string) {
    const result = await this.request(
      this.projectPath("conversations"),
      ConversationSchema,
      body({ title: name }),
    );
    this.assertProject(result);
    return result;
  }
  async updateConversation(
    conversationId: string,
    status: "active" | "archived",
  ) {
    const result = await this.request(
      `/api/v1/conversations/${enc(conversationId)}`,
      ConversationSchema,
      body({ state: status }, "PATCH"),
    );
    this.assertProject(result);
    if (result.id !== conversationId)
      throw new TRPCError({ code: "NOT_FOUND" });
    return result;
  }
  async deleteConversation(conversationId: string) {
    await this.request(
      `/api/v1/conversations/${enc(conversationId)}`,
      z.unknown(),
      { method: "DELETE" },
      true,
    );
  }
  async getEnvironment(conversationId: string) {
    const result = await this.request(
      `/api/v1/conversations/${enc(conversationId)}/environment`,
      EnvironmentSchema,
    );
    this.assertProject(result);
    if (result.conversation_id !== conversationId)
      throw new TRPCError({ code: "NOT_FOUND" });
    return result;
  }
  async environmentAction(
    conversationId: string,
    environmentId: string,
    action: "reset" | "review",
  ) {
    await this.assertEnvironment(conversationId, environmentId);
    const result = await this.request(
      `/api/v1/environments/${enc(environmentId)}/${action}`,
      EnvironmentSchema,
      body({}),
    );
    this.assertProject(result);
    if (
      result.id !== environmentId ||
      result.conversation_id !== conversationId
    )
      throw new TRPCError({ code: "NOT_FOUND" });
    return result;
  }
  private async assertEnvironment(
    conversationId: string,
    environmentId: string,
  ) {
    const environment = await this.getEnvironment(conversationId);
    if (environment.id !== environmentId)
      throw new TRPCError({ code: "NOT_FOUND" });
  }
  async startControl(conversationId: string, environmentId: string) {
    await this.assertEnvironment(conversationId, environmentId);
    const session = await this.request(
      `/api/v1/environments/${enc(environmentId)}/control-session`,
      ControlSessionSchema,
      body({}),
    );
    if (session.project_id)
      this.assertProject({ project_id: session.project_id });
    if (session.environment_id !== environmentId)
      throw new TRPCError({ code: "NOT_FOUND" });
    // Never follow a provider-supplied URL. Consume its one-time token server-side.
    const controlUrl = session.control_url;
    if (!controlUrl?.startsWith("/") || controlUrl.startsWith("//"))
      throw new TRPCError({ code: "INTERNAL_SERVER_ERROR" });
    const token =
      session.token ??
      new URL(controlUrl, this.config.apiUrl).searchParams.get("token");
    if (!token || token.length > 128)
      throw new TRPCError({ code: "INTERNAL_SERVER_ERROR" });
    const consumed = await this.request(
      `/api/v1/control-sessions/${enc(session.id)}/consume`,
      ControlSessionSchema,
      { ...body({ token }), signal: AbortSignal.timeout(45000) },
    );
    if (consumed.project_id)
      this.assertProject({ project_id: consumed.project_id });
    if (consumed.id !== session.id || consumed.environment_id !== environmentId)
      throw new TRPCError({ code: "NOT_FOUND" });
    return {
      id: session.id,
      expires_at: session.expires_at,
      url: `/api/agenttrace/control/${enc(session.id)}?projectId=${enc(this.config.langfuseProjectId)}`,
    };
  }
  async stopControl(controlSessionId: string) {
    await this.request(
      `/api/v1/control-sessions/${enc(controlSessionId)}`,
      z.unknown(),
      { method: "DELETE", signal: AbortSignal.timeout(45000) },
      true,
    );
  }
  async listSkills() {
    const result = await this.request(
      this.projectPath("skills"),
      z.object({
        skills: z.array(
          z.union([
            SkillSchema,
            SkillVersionSchema.extend({ project_id: z.string() }),
          ]),
        ),
      }),
    );
    const grouped = new Map<string, z.infer<typeof SkillSchema>>();
    for (const item of result.skills) {
      this.assertProject(item);
      if ("versions" in item) grouped.set(item.id, item);
      else {
        const skill = grouped.get(item.skill_id) ?? {
          id: item.skill_id,
          project_id: item.project_id,
          name: item.name,
          versions: [],
        };
        skill.versions.push(item);
        grouped.set(item.skill_id, skill);
      }
    }
    return [...grouped.values()];
  }
  async uploadSkill(input: z.infer<typeof SkillUploadSchema>) {
    return this.request(
      this.projectPath("skills/uploads"),
      SkillVersionSchema,
      body(SkillUploadSchema.parse(input)),
    );
  }
  async publishSkill(skillId: string, versionId: string) {
    return this.request(
      this.projectPath(`skills/${enc(skillId)}/versions`),
      SkillVersionSchema,
      body({ version_id: versionId }),
    );
  }
  async revokeSkill(versionId: string) {
    await this.request(
      this.projectPath(`skill-versions/${enc(versionId)}/revoke`),
      z.unknown(),
      body({}),
      true,
    );
  }
  async listMcpDefinitions() {
    return (
      await this.request(
        "/api/v1/mcp-definitions",
        z.object({ mcp_versions: z.array(McpDefinitionSchema) }),
      )
    ).mcp_versions;
  }
  async listMcpBindings() {
    const result = await this.request(
      this.projectPath("mcp-bindings"),
      z.union([
        z
          .object({ mcp_bindings: z.array(McpBindingSchema) })
          .transform(({ mcp_bindings }) => mcp_bindings),
        z
          .object({ bindings: z.array(McpBindingSchema) })
          .transform(({ bindings }) => bindings),
      ]),
    );
    result.forEach((item) => this.assertProject(item));
    return result;
  }
  async saveMcpBinding(
    input: z.infer<typeof McpBindingInputSchema>,
    bindingId?: string,
  ) {
    const result = await this.request(
      this.projectPath(`mcp-bindings${bindingId ? `/${enc(bindingId)}` : ""}`),
      McpBindingSchema,
      body(McpBindingInputSchema.parse(input), bindingId ? "PATCH" : "POST"),
    );
    this.assertProject(result);
    if (bindingId && result.id !== bindingId)
      throw new TRPCError({ code: "NOT_FOUND" });
    return result;
  }
  async listHarnesses() {
    const result = await this.request(
      this.projectPath("harnesses"),
      z.object({ harnesses: z.array(HarnessSnapshotSchema) }),
    );
    result.harnesses.forEach((item) => this.assertProject(item));
    return result.harnesses;
  }
  async createHarness(definition: HarnessDefinition) {
    const result = await this.request(
      this.projectPath("harnesses"),
      HarnessSnapshotSchema,
      body(HarnessDefinitionSchema.parse(definition)),
    );
    this.assertProject(result);
    return result;
  }
  async updateHarness(harnessId: string, definition: HarnessDefinition) {
    const result = await this.request(
      this.projectPath(`harnesses/${enc(harnessId)}/versions`),
      HarnessSnapshotSchema,
      body(HarnessDefinitionSchema.parse(definition)),
    );
    this.assertProject(result);
    return result;
  }
  async services() {
    const result = await this.request(
      this.projectPath("services"),
      ServicesSchema,
    );
    result.workers.forEach((worker) => {
      if (worker.project_id)
        this.assertProject({ project_id: worker.project_id });
    });
    return result;
  }
  async drainWorker(workerId: string, drain: boolean) {
    const result = await this.request(
      this.projectPath(`workers/${enc(workerId)}`),
      WorkerSchema,
      body({ draining: drain }, "PATCH"),
    );
    if (result.id !== workerId) throw new TRPCError({ code: "NOT_FOUND" });
    return result;
  }
}
