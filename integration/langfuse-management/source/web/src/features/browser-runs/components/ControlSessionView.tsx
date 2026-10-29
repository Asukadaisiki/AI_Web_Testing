import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Button } from "@/src/components/ui/button";
import { Input } from "@/src/components/ui/input";
import { ControlInputSchema } from "../control-contracts";
import type { z } from "zod/v4";

export function ControlSessionView({
  session,
  onStop,
}: {
  session: { id: string; url: string; expires_at: string };
  onStop: () => void;
}) {
  const [text, setText] = useState("");
  const [url, setUrl] = useState("");
  const [key, setKey] = useState("Enter");
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string>();
  const [stopped, setStopped] = useState(false);
  const frame = useQuery({
    queryKey: ["browser-control", session.id],
    enabled: !stopped,
    retry: false,
    refetchInterval: () =>
      !stopped && !pending && Date.now() < Date.parse(session.expires_at)
        ? 2000
        : false,
    queryFn: async ({ signal }) => {
      if (Date.now() >= Date.parse(session.expires_at))
        throw new Error("Session expired");
      const response = await fetch(session.url, {
        signal,
        credentials: "same-origin",
        cache: "no-store",
      });
      if (!response.ok) throw new Error("Session unavailable");
      const blob = await response.blob();
      return new Promise<string>((resolve, reject) => {
        const reader = new FileReader();
        reader.onload = () => resolve(String(reader.result));
        reader.onerror = reject;
        reader.readAsDataURL(blob);
      });
    },
  });
  const blocked =
    pending ||
    stopped ||
    frame.isFetching ||
    frame.isError ||
    Date.now() >= Date.parse(session.expires_at);
  const send = async (input: z.infer<typeof ControlInputSchema>) => {
    const parsed = ControlInputSchema.safeParse(input);
    if (blocked || !parsed.success) {
      setError("Use valid browser input and an HTTP(S) address.");
      return;
    }
    setPending(true);
    setError(undefined);
    try {
      const response = await fetch(session.url, {
        method: "POST",
        credentials: "same-origin",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(parsed.data),
      });
      if (!response.ok) throw new Error("Input rejected");
      setText("");
      await frame.refetch();
    } catch {
      setError("Browser input failed. Check session state before retrying.");
    } finally {
      setPending(false);
    }
  };
  return (
    <div className="flex flex-col gap-3 rounded-md border p-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <p className="text-sm">
          Manual control expires {session.expires_at}. Click the browser frame
          to interact.
        </p>
        <Button
          variant="outline"
          onClick={() => {
            setStopped(true);
            onStop();
          }}
        >
          End control session
        </Button>
      </div>
      {frame.data ? (
        <button
          type="button"
          disabled={blocked}
          className="overflow-hidden rounded-md border text-left disabled:opacity-60"
          aria-label="Browser frame"
          onClick={(event) => {
            const image = event.currentTarget.querySelector("img");
            if (!image) return;
            const rect = image.getBoundingClientRect();
            void send({
              action: "click",
              x: Math.min(
                image.naturalWidth - 1,
                Math.max(
                  0,
                  Math.round(
                    ((event.clientX - rect.left) * image.naturalWidth) /
                      rect.width,
                  ),
                ),
              ),
              y: Math.min(
                image.naturalHeight - 1,
                Math.max(
                  0,
                  Math.round(
                    ((event.clientY - rect.top) * image.naturalHeight) /
                      rect.height,
                  ),
                ),
              ),
            });
          }}
        >
          {/* Actual authenticated browser evidence, never a provider URL. */}
          {/* eslint-disable-next-line @next/next/no-img-element */}
          <img
            src={frame.data}
            alt="Current controlled browser"
            className="h-auto w-full"
          />
        </button>
      ) : (
        <p role="status">Loading browser frame...</p>
      )}
      <div className="flex flex-wrap gap-2">
        <Input
          aria-label="Browser text"
          type="password"
          value={text}
          onChange={(event) => setText(event.target.value)}
          disabled={blocked}
          maxLength={2000}
          className="flex-1"
        />
        <Button
          disabled={blocked || !text}
          onClick={() => void send({ action: "type", text })}
        >
          Type text
        </Button>
      </div>
      <div className="flex flex-wrap gap-2">
        <label className="text-sm">
          Browser key{" "}
          <select
            value={key}
            onChange={(event) => setKey(event.target.value)}
            disabled={blocked}
            className="bg-background h-9 rounded-md border px-2"
          >
            {[
              "Enter",
              "Tab",
              "Escape",
              "Backspace",
              "ArrowUp",
              "ArrowDown",
              "ArrowLeft",
              "ArrowRight",
            ].map((value) => (
              <option key={value}>{value}</option>
            ))}
          </select>
        </label>
        <Button
          disabled={blocked}
          onClick={() => {
            const input = ControlInputSchema.parse({ action: "key", key });
            void send(input);
          }}
        >
          Send key
        </Button>
      </div>
      <div className="flex flex-wrap gap-2">
        <Input
          aria-label="Browser address"
          type="url"
          value={url}
          onChange={(event) => setUrl(event.target.value)}
          disabled={blocked}
          className="flex-1"
        />
        <Button
          disabled={blocked || !url}
          onClick={() => void send({ action: "navigate", url })}
        >
          Navigate
        </Button>
      </div>
      {frame.isError || error ? (
        <p role="alert" className="text-destructive text-sm">
          {error ??
            "Control session expired or unavailable. Stop takeover and review the environment."}
        </p>
      ) : null}
    </div>
  );
}
