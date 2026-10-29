import type { AgentTraceRun } from "../contracts";

export function RunSnapshotPanel({ run }: { run: AgentTraceRun }) {
  const snapshot = run.harness_snapshot;
  if (!snapshot) return <p className="text-muted-foreground text-sm">Snapshot provenance was not recorded for this historical run. Current registry values are not substituted.</p>;
  const fields = [
    ["Harness", snapshot.name || snapshot.harness_id || "Not recorded"],
    ["Harness version ID", snapshot.harness_version_id || "Not recorded"],
    ["Harness revision", snapshot.revision ?? run.harness_revision ?? "Not recorded"],
    ["Content digest", snapshot.content_digest || "Not recorded"],
    ["Runtime", snapshot.runtime_id],
    ["Allowed runtimes", snapshot.runtime_ids?.join(", ") || "Not recorded"],
    ["Model", run.model ? [run.model.provider, run.model.name, run.model.reasoning_effort].filter(Boolean).join(" / ") : "Not recorded"],
    ["Environment", snapshot.environment_id || run.environment_id || "Not recorded"],
    ["Profile generation", run.profile_generation ?? "Not recorded"],
    ["Skill version IDs", snapshot.skill_version_ids.join(", ") || "None recorded"],
    ["MCP binding IDs", snapshot.mcp_binding_ids.join(", ") || "None recorded"],
  ] as const;
  return <details className="rounded-md border p-3 text-sm">
    <summary>Immutable execution snapshot</summary>
    <p className="text-muted-foreground mt-3 text-xs">Persisted at run creation. Registry changes do not rewrite this provenance; retries retain the source snapshot.</p>
    <dl className="mt-3 grid gap-3 sm:grid-cols-2">
      {fields.map(([name, value]) => <div key={name}><dt className="text-muted-foreground text-xs">{name}</dt><dd className="mt-1 font-mono text-xs break-all">{value}</dd></div>)}
    </dl>
    {snapshot.skills?.length ? <div className="mt-4 border-t pt-3"><h4 className="font-bold">Skill provenance</h4><dl className="mt-2 flex flex-col gap-2">{snapshot.skills.map((skill, index) => <div key={`${skill.version_id ?? skill.name}:${index}`}><dt className="text-muted-foreground text-xs">{skill.name}</dt><dd className="font-mono text-xs break-all">{skill.version_id || "Version not recorded"} / {skill.content_digest || "Digest not recorded"}</dd></div>)}</dl></div> : null}
    {snapshot.mcp && Object.keys(snapshot.mcp).length ? <div className="mt-4 border-t pt-3"><h4 className="font-bold">MCP capability provenance</h4><dl className="mt-2 flex flex-col gap-2">{Object.entries(snapshot.mcp).map(([name, capability]) => <div key={name}><dt className="text-muted-foreground text-xs">{name} / {capability.capability}</dt><dd className="flex flex-wrap gap-2 font-mono text-xs break-all"><span>{capability.version_id || "Version not recorded"}</span><span>{capability.content_digest || "Digest not recorded"}</span></dd></div>)}</dl></div> : null}
  </details>;
}
