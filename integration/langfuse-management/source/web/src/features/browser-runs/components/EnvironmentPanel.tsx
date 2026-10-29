import { useState } from "react";
import { Button } from "@/src/components/ui/button";
import {
  Card,
  CardHeader,
  CardTitle,
  CardContent,
} from "@/src/components/ui/card";
import { Badge } from "@/src/components/ui/badge";
import type { BrowserEnvironment } from "../management-contracts";
import { ControlSessionView } from "./ControlSessionView";

export function EnvironmentPanel(props: {
  environment?: BrowserEnvironment;
  canExecute: boolean;
  pending?: boolean;
  error?: string;
  control?: { id: string; url: string; expires_at: string };
  onReview: () => void;
  onReset: () => void;
  onTakeover: () => void;
  onStop: () => void;
}) {
  const [confirmReset, setConfirmReset] = useState(false);
  const env = props.environment;
  const recoverable =
    env && ["ready", "needs_review", "broken"].includes(env.state);
  const takeoverReady = env && ["ready", "needs_review"].includes(env.state);
  return (
    <Card>
      <CardHeader className="flex-row items-center justify-between">
        <CardTitle>Browser environment</CardTitle>
        {env ? <Badge variant="secondary">{env.state}</Badge> : null}
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        {env ? (
          <>
            <dl className="grid gap-3 text-sm sm:grid-cols-3">
              <div>
                <dt className="text-muted-foreground">Environment</dt>
                <dd className="font-mono break-all">{env.id}</dd>
              </div>
              <div>
                <dt className="text-muted-foreground">Profile generation</dt>
                <dd>{env.profile_generation}</dd>
              </div>
              <div>
                <dt className="text-muted-foreground">Current run</dt>
                <dd className="font-mono break-all">
                  {env.current_run_id ?? "None"}
                </dd>
              </div>
              {env.lease ? (
                <div>
                  <dt className="text-muted-foreground">Lease expires</dt>
                  <dd>{env.lease.expires_at}</dd>
                </div>
              ) : null}
            </dl>
            {env.state === "needs_review" ? (
              <p className="rounded-md border p-3 text-sm">
                External effects may be uncertain. Inspect the browser before
                confirming reviewed, or reset its profile. Runs remain blocked
                until recovery.
              </p>
            ) : null}
            {props.canExecute ? (
              <div className="flex flex-wrap gap-2">
                <Button
                  variant="outline"
                  disabled={
                    props.pending || !takeoverReady || Boolean(props.control)
                  }
                  onClick={props.onTakeover}
                >
                  Take over browser
                </Button>
                {env.state === "needs_review" ? (
                  <Button
                    disabled={props.pending || Boolean(props.control)}
                    onClick={props.onReview}
                  >
                    Confirm reviewed
                  </Button>
                ) : null}
                <Button
                  variant="outline"
                  disabled={
                    props.pending || !recoverable || Boolean(props.control)
                  }
                  onClick={() => setConfirmReset(true)}
                >
                  Reset profile
                </Button>
                {props.control ? (
                  <Button disabled={props.pending} onClick={props.onStop}>
                    Stop takeover
                  </Button>
                ) : null}
              </div>
            ) : null}
            {confirmReset && recoverable && props.canExecute ? (
              <div className="flex flex-wrap items-center gap-3 rounded-md border p-3">
                <p className="text-sm">
                  Reset removes saved login state and increments the profile
                  generation.
                </p>
                <Button
                  variant="destructive"
                  disabled={props.pending || Boolean(props.control)}
                  onClick={() => {
                    setConfirmReset(false);
                    props.onReset();
                  }}
                >
                  Confirm reset
                </Button>
                <Button
                  variant="outline"
                  onClick={() => setConfirmReset(false)}
                >
                  Keep profile
                </Button>
              </div>
            ) : null}
            {props.control && props.canExecute ? (
              <ControlSessionView
                key={props.control.id}
                session={props.control}
                onStop={props.onStop}
              />
            ) : null}
          </>
        ) : (
          <p role="status" className="text-muted-foreground text-sm">
            Environment not loaded.
          </p>
        )}
        {props.error ? (
          <p role="alert" className="text-destructive text-sm">
            {props.error}
          </p>
        ) : null}
      </CardContent>
    </Card>
  );
}
