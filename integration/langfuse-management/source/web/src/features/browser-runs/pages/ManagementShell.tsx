import { useState } from "react";
import {
  Tabs,
  TabsList,
  TabsTrigger,
  TabsContent,
} from "@/src/components/ui/tabs";
import { Button } from "@/src/components/ui/button";
import { ConversationManager } from "../components/ConversationManager";
import { EnvironmentPanel } from "../components/EnvironmentPanel";
import { RunWorkspace } from "./BrowserRunsPage";
import { RegistryWorkspace } from "./RegistryWorkspace";
import { ServicesWorkspace } from "./ServicesWorkspace";
import { useHasProjectAccess } from "@/src/features/rbac/utils/checkProjectAccess";
import { api } from "@/src/utils/api";

export function ManagementShell({ projectId }: { projectId: string }) {
  const [selection, setSelection] = useState<string>();
  const [tab, setTab] = useState("runs");
  const canExecute = useHasProjectAccess({
    projectId,
    scope: "playground:execute",
  });
  const canManage = useHasProjectAccess({ projectId, scope: "project:update" });
  const utils = api.useUtils();
  const conversations = api.browserRuns.conversations.useQuery(
    { projectId },
    { retry: false, refetchInterval: 10000 },
  );
  const selected =
    conversations.data?.find((item) => item.id === selection) ??
    conversations.data?.find((item) => item.status === "active") ??
    conversations.data?.[0];
  const refresh = () =>
    utils.browserRuns.conversations.invalidate({ projectId });
  const create = api.browserRuns.createConversation.useMutation({
    onSuccess: (result) => {
      setSelection(result.id);
      void refresh();
    },
  });
  const update = api.browserRuns.updateConversation.useMutation({
    onSuccess: refresh,
  });
  const remove = api.browserRuns.deleteConversation.useMutation({
    onSuccess: refresh,
  });
  const pending = create.isPending || update.isPending || remove.isPending;
  return (
    <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-auto p-4">
      <div>
        <h2 className="text-lg font-bold">Agent runs</h2>
        <p className="text-muted-foreground text-sm">
          Project conversations, persistent browser environments and versioned
          runtime capabilities.
        </p>
      </div>
      <ConversationManager
        conversations={conversations.data ?? []}
        selectedId={selected?.id}
        canExecute={canExecute && conversations.isSuccess}
        pending={pending}
        onSelect={setSelection}
        onCreate={(name, onSuccess) =>
          create.mutate({ projectId, name }, { onSuccess })
        }
        onStatus={(conversationId, status) =>
          update.mutate({ projectId, conversationId, status })
        }
        onDelete={(conversationId) =>
          remove.mutate({ projectId, conversationId })
        }
        error={
          remove.isError
            ? "Cancel active runs and release the environment before deleting. Refresh and retry."
            : create.isError || update.isError
              ? "Conversation action failed. Refresh and retry."
              : conversations.isError
                ? "Project binding or conversations unavailable."
                : undefined
        }
      />
      {conversations.isError ? (
        <Button
          variant="outline"
          className="self-start"
          onClick={() => void conversations.refetch()}
        >
          Retry conversations
        </Button>
      ) : null}
      <Tabs
        value={
          !canManage && ["registry", "services"].includes(tab) ? "runs" : tab
        }
        onValueChange={setTab}
      >
        <TabsList>
          <TabsTrigger value="runs">Runs</TabsTrigger>
          <TabsTrigger value="environment">Environment</TabsTrigger>
          {canManage ? (
            <>
              <TabsTrigger value="registry">Registry</TabsTrigger>
              <TabsTrigger value="services">Services</TabsTrigger>
            </>
          ) : null}
        </TabsList>
        {selected ? (
          <ConversationWorkspace
            key={`${projectId}:${selected.id}`}
            projectId={projectId}
            conversationId={selected.id}
            active={selected.status === "active"}
            canExecute={canExecute}
          />
        ) : (
          <p role="status" className="text-muted-foreground mt-4 text-sm">
            {conversations.isPending
              ? "Loading conversations..."
              : "Create a conversation to provision a browser environment."}
          </p>
        )}
        {canManage ? (
          <>
            <TabsContent value="registry">
              <RegistryWorkspace projectId={projectId} />
            </TabsContent>
            <TabsContent value="services">
              <ServicesWorkspace projectId={projectId} />
            </TabsContent>
          </>
        ) : null}
      </Tabs>
    </div>
  );
}
function ConversationWorkspace({
  projectId,
  conversationId,
  active,
  canExecute,
}: {
  projectId: string;
  conversationId: string;
  active: boolean;
  canExecute: boolean;
}) {
  const utils = api.useUtils();
  const [control, setControl] = useState<{
    id: string;
    url: string;
    expires_at: string;
  }>();
  const environment = api.browserRuns.environment.useQuery(
    { projectId, conversationId },
    { retry: false, refetchInterval: 5000 },
  );
  const refresh = () =>
    utils.browserRuns.environment.invalidate({ projectId, conversationId });
  const reset = api.browserRuns.resetEnvironment.useMutation({
    onSuccess: refresh,
  });
  const review = api.browserRuns.reviewEnvironment.useMutation({
    onSuccess: refresh,
  });
  const takeover = api.browserRuns.startControl.useMutation({
    onSuccess: (session) => {
      setControl(session);
      void refresh();
    },
  });
  const stop = api.browserRuns.stopControl.useMutation({
    onSuccess: () => {
      setControl(undefined);
      void refresh();
    },
  });
  const pending =
    reset.isPending || review.isPending || takeover.isPending || stop.isPending;
  const envInput = {
    projectId,
    conversationId,
    environmentId: environment.data?.id ?? "",
  };
  return (
    <>
      <TabsContent value="runs">
        <RunWorkspace
          projectId={projectId}
          conversationId={conversationId}
          conversationActive={active}
          environmentReady={
            environment.isSuccess &&
            environment.data?.state === "ready" &&
            !control
          }
        />
      </TabsContent>
      <TabsContent value="environment">
        {!active ? (
          <p className="text-muted-foreground mb-3 text-sm">
            Archived conversations are read-only. Create an active conversation
            to manage a browser environment.
          </p>
        ) : null}
        <EnvironmentPanel
          environment={environment.data}
          canExecute={canExecute && active && environment.isSuccess}
          pending={pending}
          control={control}
          error={
            environment.isError
              ? "Environment unavailable. Refresh to retry."
              : reset.isError ||
                  review.isError ||
                  takeover.isError ||
                  stop.isError
                ? "Environment action failed. Refresh state; release active leases before retrying."
                : undefined
          }
          onReset={() => reset.mutate(envInput)}
          onReview={() => review.mutate(envInput)}
          onTakeover={() => takeover.mutate(envInput)}
          onStop={() =>
            control && stop.mutate({ projectId, controlSessionId: control.id })
          }
        />
        {environment.isError ? (
          <Button
            variant="outline"
            className="mt-3"
            onClick={() => void environment.refetch()}
          >
            Refresh environment
          </Button>
        ) : null}
      </TabsContent>
    </>
  );
}
