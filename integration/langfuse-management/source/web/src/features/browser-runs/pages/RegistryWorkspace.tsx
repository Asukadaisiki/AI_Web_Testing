import { api } from "@/src/utils/api";
import { RegistryPanel } from "../components/RegistryPanel";
import { Button } from "@/src/components/ui/button";

export function RegistryWorkspace({ projectId }: { projectId: string }) {
  const utils = api.useUtils();
  const skills = api.browserRuns.skills.useQuery(
    { projectId },
    { retry: false },
  );
  const definitions = api.browserRuns.mcpDefinitions.useQuery(
    { projectId },
    { retry: false },
  );
  const bindings = api.browserRuns.mcpBindings.useQuery(
    { projectId },
    { retry: false },
  );
  const harnesses = api.browserRuns.harnesses.useQuery(
    { projectId },
    { retry: false },
  );
  const refresh = () =>
    Promise.all([
      utils.browserRuns.skills.invalidate({ projectId }),
      utils.browserRuns.mcpBindings.invalidate({ projectId }),
      utils.browserRuns.harnesses.invalidate({ projectId }),
    ]);
  const upload = api.browserRuns.uploadSkill.useMutation({
    onSuccess: refresh,
  });
  const publish = api.browserRuns.publishSkill.useMutation({
    onSuccess: refresh,
  });
  const revoke = api.browserRuns.revokeSkill.useMutation({
    onSuccess: refresh,
  });
  const binding = api.browserRuns.saveMcpBinding.useMutation({
    onSuccess: refresh,
  });
  const createHarness = api.browserRuns.createHarness.useMutation({
    onSuccess: refresh,
  });
  const updateHarness = api.browserRuns.updateHarness.useMutation({
    onSuccess: refresh,
  });
  const catalogError =
    skills.isError ||
    definitions.isError ||
    bindings.isError ||
    harnesses.isError;
  const pending =
    upload.isPending ||
    publish.isPending ||
    revoke.isPending ||
    binding.isPending ||
    createHarness.isPending ||
    updateHarness.isPending;
  return (
    <>
      <RegistryPanel
        skills={skills.data ?? []}
        definitions={definitions.data ?? []}
        bindings={bindings.data ?? []}
        harnesses={harnesses.data ?? []}
        canManage={
          !catalogError &&
          skills.isSuccess &&
          definitions.isSuccess &&
          bindings.isSuccess &&
          harnesses.isSuccess
        }
        pending={pending}
        error={
          catalogError
            ? "Registry unavailable. Check project binding and refresh."
            : upload.isError ||
                publish.isError ||
                revoke.isError ||
                binding.isError ||
                createHarness.isError ||
                updateHarness.isError
              ? "Registry action failed. Check package validation and approved references, then retry."
              : undefined
        }
        onUpload={(input, onSuccess) =>
          upload.mutate({ projectId, upload: input }, { onSuccess })
        }
        onPublish={(skillId, versionId) =>
          publish.mutate({ projectId, skillId, versionId })
        }
        onRevoke={(versionId) => revoke.mutate({ projectId, versionId })}
        onBinding={(bindingId, input, onSuccess) =>
          binding.mutate(
            { projectId, bindingId, binding: input },
            { onSuccess },
          )
        }
        onHarness={(harnessId, definition, onSuccess) =>
          harnessId
            ? updateHarness.mutate(
                { projectId, harnessId, definition },
                { onSuccess },
              )
            : createHarness.mutate({ projectId, definition }, { onSuccess })
        }
      />
      {catalogError ? (
        <Button
          variant="outline"
          className="mt-3"
          onClick={() =>
            void Promise.all([
              skills.refetch(),
              definitions.refetch(),
              bindings.refetch(),
              harnesses.refetch(),
            ])
          }
        >
          Refresh registry
        </Button>
      ) : null}
    </>
  );
}
