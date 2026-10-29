import { useRef, useState } from "react";
import { Button } from "@/src/components/ui/button";
import {
  Dialog,
  DialogBody,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/src/components/ui/dialog";
import { Input } from "@/src/components/ui/input";
import { Textarea } from "@/src/components/ui/textarea";
import { safeRandomUUID } from "@/src/utils/safe-random-uuid";
import {
  CreateRunSchema,
  type CreateRunInput,
  type Runtime,
  type HarnessSnapshot,
} from "../contracts";

export function NewRunDialog({
  conversationId,
  onCreate,
  isLoading,
  runtimes = [],
  harnesses = [],
  catalogLoading = false,
  catalogError,
  error,
  canExecute = true,
}: {
  conversationId: string;
  onCreate: (
    input: Omit<CreateRunInput, "projectId">,
    onSuccess: () => void,
  ) => void;
  isLoading: boolean;
  runtimes?: Runtime[];
  harnesses?: HarnessSnapshot[];
  catalogLoading?: boolean;
  catalogError?: string;
  error?: string;
  canExecute?: boolean;
}) {
  const [open, setOpen] = useState(false);
  const [url, setUrl] = useState("");
  const [goal, setGoal] = useState("");
  const [runtimeId, setRuntimeId] = useState("");
  const [harnessId, setHarnessId] = useState("");
  const runtime = runtimes.find((item) => item.id === runtimeId);
  const harness = harnesses.find((item) => item.id === harnessId);
  const validInput = CreateRunSchema.safeParse({
    projectId: "configured",
    conversationId,
    url,
    goal,
    runtimeId,
    harnessId,
    idempotencyKey: "draft",
  });
  const ready =
    canExecute &&
    !isLoading &&
    !catalogLoading &&
    !catalogError &&
    runtime?.available &&
    harness &&
    !harness.revoked_at &&
    harness.runtime_ids.includes(runtimeId as Runtime["id"]) &&
    validInput.success;
  const openRef = useRef(false);
  const draftGenerationRef = useRef(0);
  const submissionRef = useRef(0);
  const draftRef = useRef<{
    fingerprint: string;
    idempotencyKey: string;
  } | null>(null);

  const handleOpenChange = (nextOpen: boolean) => {
    if (nextOpen && !openRef.current) {
      draftGenerationRef.current += 1;
    }
    openRef.current = nextOpen;
    setOpen(nextOpen);
  };

  const submit = () => {
    if (!ready || !validInput.success || !harness) return;
    const normalizedUrl = url.trim();
    const normalizedGoal = goal.trim();
    const fingerprint = JSON.stringify([
      normalizedUrl,
      normalizedGoal,
      conversationId,
      runtimeId,
      harnessId,
      harness.version,
    ]);
    if (!draftRef.current || draftRef.current.fingerprint !== fingerprint) {
      draftRef.current = { fingerprint, idempotencyKey: safeRandomUUID() };
    }
    const draftGeneration = draftGenerationRef.current;
    const submission = submissionRef.current + 1;
    submissionRef.current = submission;
    onCreate(
      {
        conversationId,
        url: normalizedUrl || undefined,
        goal: normalizedGoal,
        runtimeId: validInput.data.runtimeId,
        harnessId,
        idempotencyKey: draftRef.current.idempotencyKey,
      },
      () => {
        if (
          openRef.current &&
          draftGenerationRef.current === draftGeneration &&
          submissionRef.current === submission
        ) {
          draftRef.current = null;
          setUrl("");
          setGoal("");
          handleOpenChange(false);
        }
      },
    );
  };

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <Button disabled={!canExecute} onClick={() => handleOpenChange(true)}>
        New agent run
      </Button>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>New agent run</DialogTitle>
        </DialogHeader>
        <DialogBody>
          <label className="flex flex-col gap-2 text-sm font-bold">
            Runtime
            <select
              className="bg-background h-9 rounded-md border px-3 text-sm font-normal"
              value={runtimeId}
              onChange={(event) => setRuntimeId(event.target.value)}
              disabled={isLoading || catalogLoading}
            >
              <option value="">Select Runtime</option>
              {runtimes.map((item) => (
                <option
                  key={item.id}
                  value={item.id}
                  disabled={!item.available}
                >
                  {item.name}
                  {!item.available
                    ? `: ${item.unavailable_reason ?? "Unavailable"}`
                    : ""}
                </option>
              ))}
            </select>
          </label>
          <label className="flex flex-col gap-2 text-sm font-bold">
            Harness
            <select
              className="bg-background h-9 rounded-md border px-3 text-sm font-normal"
              value={harnessId}
              onChange={(event) => setHarnessId(event.target.value)}
              disabled={isLoading || catalogLoading}
            >
              <option value="">Select Harness</option>
              {harnesses.map((item) => (
                <option key={item.id} value={item.id}>
                  {item.name} (version {item.version})
                </option>
              ))}
            </select>
          </label>
          {catalogLoading ? <p role="status">Loading catalogs...</p> : null}
          {catalogError ? <p role="alert">{catalogError}</p> : null}
          {!catalogLoading && !catalogError && !harnesses.length ? (
            <p>No Harnesses configured. Ask a project admin to create one.</p>
          ) : null}
          <label className="flex flex-col gap-2 text-sm font-bold">
            URL (optional)
            <Input
              aria-label="URL"
              value={url}
              onChange={(event) => setUrl(event.target.value)}
              type="url"
              disabled={isLoading}
            />
          </label>
          <label className="flex flex-col gap-2 text-sm font-bold">
            Goal
            <Textarea
              value={goal}
              disabled={isLoading}
              onChange={(event) => setGoal(event.target.value)}
            />
          </label>
          {url.trim() && !validInput.success ? (
            <p className="text-muted-foreground text-sm">
              Use an HTTP(S) URL and select a Runtime and Harness.
            </p>
          ) : null}
          {error ? (
            <p role="alert" className="text-destructive text-sm">
              {error}
            </p>
          ) : null}
        </DialogBody>
        <DialogFooter>
          <Button
            aria-label="Start run"
            onClick={submit}
            loading={isLoading}
            disabled={!ready}
          >
            Start run
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
