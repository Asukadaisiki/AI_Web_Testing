import { useState } from "react";
import { Button } from "@/src/components/ui/button";
import {
  Dialog,
  DialogBody,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/src/components/ui/dialog";
import { Input } from "@/src/components/ui/input";
import { Textarea } from "@/src/components/ui/textarea";
import {
  HarnessDefinitionSchema,
  type HarnessDefinition,
  type HarnessSnapshot,
  type RuntimeId,
} from "../contracts";

type Props = {
  harnesses: HarnessSnapshot[];
  canManage: boolean;
  pending?: boolean;
  error?: string;
  skills?: { id: string; name: string; version: number }[];
  bindings?: { id: string; name: string }[];
  onSave: (
    id: string | undefined,
    definition: HarnessDefinition,
    onSuccess: () => void,
  ) => void;
};
export function HarnessManager(props: Props) {
  const [open, setOpen] = useState(false);
  const [editingId, setEditingId] = useState<string>();
  const [creating, setCreating] = useState(false);
  const selected = props.harnesses.find((item) => item.id === editingId);
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <Button variant="outline" onClick={() => setOpen(true)}>
        Manage Harnesses
      </Button>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Harness versions</DialogTitle>
        </DialogHeader>
        <DialogBody className="flex flex-col gap-4">
          <p className="text-muted-foreground text-sm">
            Compose published capabilities. Each save publishes an immutable
            version; historical Runs retain their snapshot.
          </p>
          {!props.canManage ? (
            <p className="text-sm">
              Project admin access is required to configure Harnesses.
            </p>
          ) : null}
          {props.harnesses.map((item) => (
            <div key={item.id} className="flex items-center gap-2 text-sm">
              <span className="flex-1">
                {item.name} (version {item.version})
                {item.revoked_at ? " - revoked" : ""}
              </span>
              {props.canManage && !item.revoked_at ? (
                <Button
                  size="sm"
                  variant="outline"
                  disabled={props.pending}
                  aria-label={`Edit ${item.name}`}
                  onClick={() => {
                    setEditingId(item.id);
                    setCreating(false);
                  }}
                >
                  Edit
                </Button>
              ) : null}
            </div>
          ))}
          {!props.harnesses.length ? (
            <p className="text-sm">No Harnesses configured.</p>
          ) : null}
          {props.canManage ? (
            <Button
              variant="outline"
              disabled={props.pending}
              onClick={() => {
                setCreating(true);
                setEditingId(undefined);
              }}
            >
              New Harness
            </Button>
          ) : null}
          {props.canManage && (creating || selected) ? (
            <HarnessForm
              key={selected ? `${selected.id}:${selected.version}` : "new"}
              initial={selected}
              {...props}
              onSuccess={() => {
                setCreating(false);
                setEditingId(undefined);
              }}
            />
          ) : null}
          {props.error ? (
            <p role="alert" className="text-destructive text-sm">
              {props.error}
            </p>
          ) : null}
        </DialogBody>
      </DialogContent>
    </Dialog>
  );
}
function HarnessForm({
  initial,
  pending,
  skills = [],
  bindings = [],
  onSave,
  onSuccess,
}: Props & { initial?: HarnessSnapshot; onSuccess: () => void }) {
  const [name, setName] = useState(initial?.name ?? "");
  const [prompt, setPrompt] = useState(initial?.system_prompt ?? "");
  const [skillIds, setSkillIds] = useState(initial?.skill_version_ids ?? []);
  const [bindingIds, setBindingIds] = useState(initial?.mcp_binding_ids ?? []);
  const [runtimeIds, setRuntimeIds] = useState<RuntimeId[]>(
    initial?.runtime_ids ?? ["pi", "claude-code", "codex"],
  );
  const [error, setError] = useState<string>();
  const toggle = <T extends string>(ids: T[], id: T) =>
    ids.includes(id) ? ids.filter((value) => value !== id) : [...ids, id];
  return (
    <div className="flex flex-col gap-3">
      <label className="flex flex-col gap-1 text-sm">
        Name
        <Input
          value={name}
          disabled={pending}
          onChange={(event) => setName(event.target.value)}
        />
      </label>
      <label className="flex flex-col gap-1 text-sm">
        System prompt
        <Textarea
          value={prompt}
          disabled={pending}
          onChange={(event) => setPrompt(event.target.value)}
        />
      </label>
      <fieldset className="flex flex-wrap gap-3 text-sm">
        <legend className="mb-2">Allowed runtimes</legend>
        {(["pi", "claude-code", "codex"] as const).map((id) => (
          <label key={id}>
            <input
              type="checkbox"
              checked={runtimeIds.includes(id)}
              disabled={pending}
              onChange={() => setRuntimeIds(toggle(runtimeIds, id))}
            />{" "}
            {id}
          </label>
        ))}
      </fieldset>
      <fieldset className="flex flex-col gap-2 text-sm">
        <legend className="mb-2">Published skill versions</legend>
        {skills.map((item) => (
          <label key={item.id}>
            <input
              type="checkbox"
              checked={skillIds.includes(item.id)}
              disabled={pending}
              onChange={() => setSkillIds(toggle(skillIds, item.id))}
            />{" "}
            {item.name} v{item.version}
          </label>
        ))}
        {skillIds
          .filter((id) => !skills.some((item) => item.id === id))
          .map((id) => (
            <label key={id}>
              <input
                type="checkbox"
                checked
                disabled={pending}
                onChange={() => setSkillIds(toggle(skillIds, id))}
              />{" "}
              Unavailable reference: {id}
            </label>
          ))}
      </fieldset>
      <fieldset className="flex flex-col gap-2 text-sm">
        <legend className="mb-2">Enabled MCP bindings</legend>
        {bindings.map((item) => (
          <label key={item.id}>
            <input
              type="checkbox"
              checked={bindingIds.includes(item.id)}
              disabled={pending}
              onChange={() => setBindingIds(toggle(bindingIds, item.id))}
            />{" "}
            {item.name}
          </label>
        ))}
        {bindingIds
          .filter((id) => !bindings.some((item) => item.id === id))
          .map((id) => (
            <label key={id}>
              <input
                type="checkbox"
                checked
                disabled={pending}
                onChange={() => setBindingIds(toggle(bindingIds, id))}
              />{" "}
              Unavailable reference: {id}
            </label>
          ))}
      </fieldset>
      {error ? (
        <p role="alert" className="text-destructive text-sm">
          {error}
        </p>
      ) : null}
      <Button
        aria-label="Save Harness"
        disabled={pending}
        onClick={() => {
          const result = HarnessDefinitionSchema.safeParse({
            name,
            system_prompt: prompt,
            skill_version_ids: skillIds,
            mcp_binding_ids: bindingIds,
            runtime_ids: runtimeIds,
          });
          if (!result.success) {
            setError("Enter a name and select at least one runtime.");
            return;
          }
          onSave(initial?.harness_id, result.data, onSuccess);
        }}
      >
        Save Harness
      </Button>
    </div>
  );
}
