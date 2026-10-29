import { useId, useState } from "react";
import type { z } from "zod/v4";
import { Button } from "@/src/components/ui/button";
import { Input } from "@/src/components/ui/input";
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from "@/src/components/ui/card";
import {
  Tabs,
  TabsList,
  TabsTrigger,
  TabsContent,
} from "@/src/components/ui/tabs";
import { Badge } from "@/src/components/ui/badge";
import { HarnessManager } from "./HarnessManager";
import {
  SkillUploadSchema,
  SharedSkillNameSchema,
  McpBindingInputSchema,
  type SkillSchema,
  type McpDefinitionSchema,
  type McpBindingSchema,
} from "../management-contracts";
import type { HarnessDefinition, HarnessSnapshot } from "../contracts";

type Props = {
  skills: z.infer<typeof SkillSchema>[];
  definitions: z.infer<typeof McpDefinitionSchema>[];
  bindings: z.infer<typeof McpBindingSchema>[];
  harnesses: HarnessSnapshot[];
  canManage: boolean;
  pending?: boolean;
  error?: string;
  onUpload: (
    upload: z.infer<typeof SkillUploadSchema>,
    onSuccess: () => void,
  ) => void;
  onPublish: (skillId: string, versionId: string) => void;
  onRevoke: (versionId: string) => void;
  onBinding: (
    id: string | undefined,
    binding: z.infer<typeof McpBindingInputSchema>,
    onSuccess: () => void,
  ) => void;
  onHarness: (
    id: string | undefined,
    definition: HarnessDefinition,
    onSuccess: () => void,
  ) => void;
};
export function RegistryPanel(props: Props) {
  const [previewId, setPreviewId] = useState<string>();
  const [revokeId, setRevokeId] = useState<string>();
  const [editingId, setEditingId] = useState<string>();
  const versions = props.skills.flatMap((skill) => skill.versions);
  const preview = versions.find((item) => item.id === previewId);
  const editing = props.bindings.find((item) => item.id === editingId);
  return (
    <Tabs defaultValue="skills" className="flex flex-col gap-3">
      <TabsList className="self-start">
        <TabsTrigger value="skills">Skills</TabsTrigger>
        <TabsTrigger value="mcp">MCP bindings</TabsTrigger>
        <TabsTrigger value="harnesses">Harnesses</TabsTrigger>
      </TabsList>
      {props.error ? (
        <p role="alert" className="text-destructive text-sm">
          {props.error}
        </p>
      ) : null}
      <TabsContent value="skills" className="flex flex-col gap-4">
        <Card>
          <CardHeader>
            <CardTitle>Skill packages</CardTitle>
          </CardHeader>
          <CardContent className="flex flex-col gap-3">
            <p className="text-muted-foreground text-sm">
              Upload a folder or ZIP with root SKILL.md. Preview drafts before
              publishing. Revoked versions cannot be selected for new Harnesses.
            </p>
            {props.canManage ? (
              <SkillUpload pending={props.pending} onUpload={props.onUpload} />
            ) : null}
            {!versions.length ? (
              <p className="text-sm">No skill versions yet.</p>
            ) : null}
            {versions.map((item) => (
              <div
                key={item.id}
                className="flex flex-wrap items-center gap-2 rounded-md border p-3 text-sm"
              >
                <span className="flex-1">
                  {item.name} v{item.version}
                </span>
                <Badge variant="secondary">{item.status}</Badge>
                <Button
                  variant="outline"
                  size="sm"
                  aria-label={`Preview ${item.name} v${item.version}`}
                  onClick={() => setPreviewId(item.id)}
                >
                  Preview
                </Button>
                {props.canManage && item.status === "draft" ? (
                  <Button
                    size="sm"
                    disabled={props.pending}
                    aria-label={`Publish ${item.name} v${item.version}`}
                    onClick={() => props.onPublish(item.skill_id, item.id)}
                  >
                    Publish
                  </Button>
                ) : null}
                {props.canManage && item.status === "published" ? (
                  <Button
                    variant="outline"
                    size="sm"
                    disabled={props.pending}
                    aria-label={`Revoke ${item.name} v${item.version}`}
                    onClick={() => setRevokeId(item.id)}
                  >
                    Revoke
                  </Button>
                ) : null}
              </div>
            ))}
            {props.canManage && revokeId ? (
              <div className="flex flex-wrap items-center gap-2 text-sm">
                <p>
                  Revoke this version? New runs cannot use it; existing
                  snapshots remain.
                </p>
                <Button
                  variant="destructive"
                  disabled={props.pending}
                  onClick={() => {
                    props.onRevoke(revokeId);
                    setRevokeId(undefined);
                  }}
                >
                  Confirm revoke
                </Button>
                <Button
                  variant="outline"
                  onClick={() => setRevokeId(undefined)}
                >
                  Keep version
                </Button>
              </div>
            ) : null}
            {preview ? (
              <div className="flex flex-col gap-3 rounded-md border p-3">
                <p className="font-mono text-xs break-all">
                  Digest: {preview.content_digest}
                </p>
                {preview.files.map((file) => (
                  <details key={file.path} open={file.path === "SKILL.md"}>
                    <summary className="text-sm">{file.path}</summary>
                    <pre className="bg-muted mt-2 max-h-96 overflow-auto rounded-md p-3 text-xs whitespace-pre-wrap">
                      {file.content}
                    </pre>
                  </details>
                ))}
              </div>
            ) : null}
          </CardContent>
        </Card>
      </TabsContent>
      <TabsContent value="mcp">
        <Card>
          <CardHeader>
            <CardTitle>Project MCP bindings</CardTitle>
          </CardHeader>
          <CardContent className="flex flex-col gap-4">
            <p className="text-muted-foreground text-sm">
              Bind approved platform versions. Configuration is schema-driven;
              credentials are secret references, never raw values. Platform
              definitions are maintained by system admins.
            </p>
            {props.bindings.map((binding) => (
              <div
                key={binding.id}
                className="flex flex-wrap items-center gap-2 rounded-md border p-3 text-sm"
              >
                <span className="flex-1">
                  {props.definitions.find(
                    (item) => item.id === binding.mcp_version_id,
                  )?.name ?? binding.mcp_version_id}
                </span>
                <Badge variant="secondary">
                  {binding.enabled ? "Enabled" : "Disabled"}
                </Badge>
                {props.canManage ? (
                  <Button
                    variant="outline"
                    disabled={props.pending}
                    onClick={() => setEditingId(binding.id)}
                  >
                    Edit binding
                  </Button>
                ) : null}
              </div>
            ))}
            {props.canManage ? (
              <>
                <Button
                  variant="outline"
                  onClick={() => setEditingId(undefined)}
                >
                  New binding
                </Button>
                <BindingForm
                  key={editing?.id ?? "new"}
                  initial={editing}
                  definitions={props.definitions}
                  pending={props.pending}
                  onSave={props.onBinding}
                />
              </>
            ) : null}
          </CardContent>
        </Card>
      </TabsContent>
      <TabsContent value="harnesses">
        <Card>
          <CardHeader>
            <CardTitle>Immutable Harness versions</CardTitle>
          </CardHeader>
          <CardContent className="flex flex-col gap-3">
            <HarnessManager
              harnesses={props.harnesses}
              skills={versions.filter((item) => item.status === "published")}
              bindings={props.bindings
                .filter((item) => item.enabled)
                .map((item) => ({
                  id: item.id,
                  name:
                    props.definitions.find(
                      (definition) => definition.id === item.mcp_version_id,
                    )?.name ?? item.id,
                }))}
              canManage={props.canManage}
              pending={props.pending}
              onSave={props.onHarness}
            />
            {props.harnesses.map((item) => (
              <div key={item.id} className="rounded-md border p-3 text-sm">
                <p>
                  {item.name} v{item.version}
                </p>
                <p className="text-muted-foreground text-xs">
                  {item.runtime_ids.join(", ")} ·{" "}
                  {item.skill_version_ids.length} skills ·{" "}
                  {item.mcp_binding_ids.length} MCP bindings
                </p>
                <p className="mt-2 font-mono text-xs break-all">
                  {item.id} / {item.content_digest}
                </p>
              </div>
            ))}
          </CardContent>
        </Card>
      </TabsContent>
    </Tabs>
  );
}
function SkillUpload({
  pending,
  onUpload,
}: Pick<Props, "pending" | "onUpload">) {
  const [name, setName] = useState("");
  const [upload, setUpload] =
    useState<Omit<z.infer<typeof SkillUploadSchema>, "name">>();
  const [reading, setReading] = useState(false);
  const [error, setError] = useState<string>();
  const nameId = useId();
  const nameValidation = SharedSkillNameSchema.safeParse(name);
  const nameError =
    name && !nameValidation.success
      ? nameValidation.error.issues[0]?.message
      : undefined;
  const read = async (files: FileList | null) => {
    if (!files?.length) return;
    setReading(true);
    setError(undefined);
    setUpload(undefined);
    try {
      const selected = Array.from(files);
      if (
        selected.length > 100 ||
        selected.reduce((sum, file) => sum + file.size, 0) > 2 * 1024 * 1024
      )
        throw new Error("Upload exceeds 2 MiB or 100 files.");
      if (
        selected.length === 1 &&
        selected[0]!.name.toLowerCase().endsWith(".zip")
      ) {
        const archive = await new Promise<string>((resolve, reject) => {
          const reader = new FileReader();
          reader.onload = () =>
            resolve(String(reader.result).split(",")[1] ?? "");
          reader.onerror = reject;
          reader.readAsDataURL(selected[0]!);
        });
        setUpload({ files: [], archive_base64: archive });
      } else {
        const contents = await Promise.all(
          selected.map(async (file) => ({
            path: file.webkitRelativePath
              ? file.webkitRelativePath.split("/").slice(1).join("/")
              : file.name,
            content: await file.text(),
          })),
        );
        const parsed = SkillUploadSchema.safeParse({
          name: "preview",
          files: contents,
        });
        if (!parsed.success)
          throw new Error(
            "Use a root SKILL.md, unique safe paths and at most 2 MiB.",
          );
        setUpload({ files: parsed.data.files });
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "Unable to read package.");
    } finally {
      setReading(false);
    }
  };
  return (
    <div className="flex flex-col gap-3 rounded-md border p-3">
      <label className="flex flex-col gap-1 text-sm">
        Skill name
        <Input
          value={name}
          maxLength={128}
          aria-invalid={Boolean(nameError)}
          aria-describedby={`${nameId}-help${nameError ? ` ${nameId}-error` : ""}`}
          onChange={(event) => setName(event.target.value)}
          disabled={pending || reading}
        />
      </label>
      <p id={`${nameId}-help`} className="text-muted-foreground text-xs">
        {SharedSkillNameSchema.description}
      </p>
      {nameError ? (
        <p
          id={`${nameId}-error`}
          role="alert"
          className="text-destructive text-sm"
        >
          {nameError}
        </p>
      ) : null}
      <div className="flex flex-wrap gap-3">
        <label className="text-sm">
          Skill folder
          <input
            type="file"
            multiple
            {...{ webkitdirectory: "" }}
            disabled={pending || reading}
            onChange={(event) => void read(event.target.files)}
            className="mt-1 block max-w-full text-xs"
          />
        </label>
        <label className="text-sm">
          Skill ZIP
          <input
            type="file"
            accept=".zip"
            disabled={pending || reading}
            onChange={(event) => void read(event.target.files)}
            className="mt-1 block max-w-full text-xs"
          />
        </label>
      </div>
      {upload ? (
        <p className="text-muted-foreground text-xs">
          {upload.archive_base64
            ? "ZIP ready; server validation and draft preview follow upload."
            : upload.files.map((file) => file.path).join(", ")}
        </p>
      ) : null}
      <Button
        className="self-start"
        disabled={pending || reading || !upload || !nameValidation.success}
        onClick={() =>
          upload &&
          nameValidation.success &&
          onUpload({ ...upload, name: nameValidation.data }, () => {
            setName("");
            setUpload(undefined);
          })
        }
      >
        Upload draft
      </Button>
      {error ? (
        <p role="alert" className="text-destructive text-sm">
          {error}
        </p>
      ) : null}
    </div>
  );
}
function BindingForm({
  initial,
  definitions,
  pending,
  onSave,
}: {
  initial?: Props["bindings"][number];
  definitions: Props["definitions"];
  pending?: boolean;
  onSave: Props["onBinding"];
}) {
  const [version, setVersion] = useState(initial?.mcp_version_id ?? "");
  const [config, setConfig] = useState<
    z.infer<typeof McpBindingInputSchema>["config"]
  >(initial?.config ?? {});
  const [secrets, setSecrets] = useState<Record<string, string>>(
    initial?.secret_refs ?? {},
  );
  const [enabled, setEnabled] = useState(initial?.enabled ?? true);
  const [error, setError] = useState<string>();
  const definition = definitions.find(
    (item) => item.id === version && !item.revoked_at,
  );
  const reserved = [
    "command",
    "args",
    "env",
    "executable_ref",
    "__proto__",
    "constructor",
    "prototype",
  ];
  return (
    <div className="flex flex-col gap-3 rounded-md border p-3">
      <label className="flex flex-col gap-1 text-sm">
        MCP version
        <select
          className="bg-background h-9 rounded-md border px-3"
          value={version}
          disabled={pending}
          onChange={(event) => {
            setVersion(event.target.value);
            setConfig({});
            setSecrets({});
          }}
        >
          <option value="">Select approved version</option>
          {definitions
            .filter((item) => !item.revoked_at)
            .map((item) => (
              <option key={item.id} value={item.id}>
                {item.name} v{item.version}
              </option>
            ))}
        </select>
      </label>
      {Object.entries(definition?.config_schema.properties ?? {})
        .filter(([name]) => !reserved.includes(name))
        .map(([name, field]) => (
          <label key={name} className="flex flex-col gap-1 text-sm">
            {name}
            {field.description ? (
              <span className="text-muted-foreground text-xs">
                {field.description}
              </span>
            ) : null}
            <Input
              type={
                field.type === "number" || field.type === "integer"
                  ? "number"
                  : "text"
              }
              value={String(config[name] ?? "")}
              disabled={pending}
              onChange={(event) =>
                setConfig({
                  ...config,
                  [name]:
                    field.type === "boolean"
                      ? event.target.value === "true"
                      : field.type === "number" || field.type === "integer"
                        ? Number(event.target.value)
                        : event.target.value,
                })
              }
            />
          </label>
        ))}
      {Object.keys(definition?.secret_schema.properties ?? {})
        .filter((name) => !reserved.includes(name))
        .map((name) => (
          <label key={name} className="flex flex-col gap-1 text-sm">
            {name} secret reference
            <Input
              placeholder="secret://project/name"
              value={secrets[name] ?? ""}
              disabled={pending}
              onChange={(event) =>
                setSecrets({ ...secrets, [name]: event.target.value })
              }
            />
          </label>
        ))}
      <label className="text-sm">
        <input
          type="checkbox"
          checked={enabled}
          disabled={pending}
          onChange={(event) => setEnabled(event.target.checked)}
        />{" "}
        Enabled
      </label>
      <Button
        disabled={pending || !definition}
        className="self-start"
        onClick={() => {
          const parsed = McpBindingInputSchema.safeParse({
            mcp_version_id: version,
            config,
            secret_refs: secrets,
            enabled,
          });
          if (
            !parsed.success ||
            definition?.config_schema.required.some(
              (name) => config[name] === undefined || config[name] === "",
            ) ||
            definition?.secret_schema.required.some((name) => !secrets[name])
          ) {
            setError(
              "Complete required schema fields and use secret:// references.",
            );
            return;
          }
          onSave(initial?.id, parsed.data, () => setError(undefined));
        }}
      >
        Save binding
      </Button>
      {error ? (
        <p role="alert" className="text-destructive text-sm">
          {error}
        </p>
      ) : null}
    </div>
  );
}
