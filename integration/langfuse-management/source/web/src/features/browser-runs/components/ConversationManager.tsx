import { useState } from "react";
import { Button } from "@/src/components/ui/button";
import { Input } from "@/src/components/ui/input";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogBody,
} from "@/src/components/ui/dialog";
import type { Conversation } from "../management-contracts";

export function ConversationManager(props: {
  conversations: Conversation[];
  selectedId?: string;
  canExecute: boolean;
  pending?: boolean;
  error?: string;
  onSelect: (id: string) => void;
  onCreate: (name: string, onSuccess: () => void) => void;
  onStatus: (id: string, status: "active" | "archived") => void;
  onDelete: (id: string) => void;
}) {
  const [creating, setCreating] = useState(false);
  const [name, setName] = useState("");
  const [deleteId, setDeleteId] = useState<string>();
  const selected = props.conversations.find(
    (item) => item.id === props.selectedId,
  );
  const blocked =
    props.pending ||
    !selected ||
    ["deleting", "deleted"].includes(selected.status);
  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap items-end gap-2">
        <label className="flex min-w-64 flex-col gap-1 text-sm">
          Conversation
          <select
            className="bg-background h-9 rounded-md border px-3"
            value={props.selectedId ?? ""}
            onChange={(event) => {
              setDeleteId(undefined);
              props.onSelect(event.target.value);
            }}
          >
            <option value="" disabled>
              Select conversation
            </option>
            {props.conversations.map((item) => (
              <option key={item.id} value={item.id}>
                {item.name}
                {item.status !== "active" ? ` (${item.status})` : ""}
              </option>
            ))}
          </select>
        </label>
        {props.canExecute ? (
          <>
            <Button
              variant="outline"
              disabled={props.pending}
              onClick={() => setCreating(true)}
            >
              New conversation
            </Button>
            {selected?.status === "active" ? (
              <Button
                variant="outline"
                disabled={blocked}
                onClick={() =>
                  selected && props.onStatus(selected.id, "archived")
                }
              >
                Archive conversation
              </Button>
            ) : null}
            <Button
              variant="outline"
              disabled={blocked}
              onClick={() => setDeleteId(selected?.id)}
            >
              Delete conversation
            </Button>
          </>
        ) : null}
      </div>
      {deleteId && selected?.id === deleteId && props.canExecute ? (
        <div className="flex flex-wrap items-center gap-3 rounded-md border p-3">
          <p className="text-sm">
            Delete {selected.name}? Conversation data and its browser profile
            will be removed.
          </p>
          <Button
            variant="destructive"
            disabled={props.pending}
            onClick={() => props.onDelete(deleteId)}
          >
            Confirm delete
          </Button>
          <Button variant="outline" onClick={() => setDeleteId(undefined)}>
            Keep conversation
          </Button>
        </div>
      ) : null}
      {props.error ? (
        <p role="alert" className="text-destructive text-sm">
          {props.error}
        </p>
      ) : null}
      <Dialog open={creating} onOpenChange={setCreating}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>New conversation</DialogTitle>
          </DialogHeader>
          <DialogBody>
            <label className="flex flex-col gap-2 text-sm">
              Conversation name
              <Input
                value={name}
                onChange={(event) => setName(event.target.value)}
                maxLength={200}
                disabled={props.pending}
              />
            </label>
            <Button
              disabled={!props.canExecute || props.pending || !name.trim()}
              onClick={() =>
                props.onCreate(name.trim(), () => {
                  setName("");
                  setCreating(false);
                })
              }
            >
              Create conversation
            </Button>
          </DialogBody>
        </DialogContent>
      </Dialog>
    </div>
  );
}
