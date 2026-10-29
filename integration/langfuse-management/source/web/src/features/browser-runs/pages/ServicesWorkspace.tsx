import { api } from "@/src/utils/api";
import { Button } from "@/src/components/ui/button";
import {
  Card,
  CardHeader,
  CardTitle,
  CardContent,
} from "@/src/components/ui/card";
import { Badge } from "@/src/components/ui/badge";
import {
  Table,
  TableHeader,
  TableBody,
  TableHead,
  TableRow,
  TableCell,
} from "@/src/components/ui/table";

export function ServicesWorkspace({ projectId }: { projectId: string }) {
  const utils = api.useUtils();
  const services = api.browserRuns.services.useQuery(
    { projectId },
    { retry: false, refetchInterval: 10000 },
  );
  const drain = api.browserRuns.drainWorker.useMutation({
    onSuccess: () => utils.browserRuns.services.invalidate({ projectId }),
  });
  return (
    <div className="flex flex-col gap-4">
      <Card>
        <CardHeader>
          <CardTitle>Service health</CardTitle>
        </CardHeader>
        <CardContent className="grid gap-3 sm:grid-cols-3">
          {services.data?.components.map((item) => (
            <div key={item.name} className="rounded-md border p-3 text-sm">
              <div className="flex items-center justify-between gap-2">
                <span>{item.name}</span>
                <Badge variant="secondary">{item.status}</Badge>
              </div>
              {item.reason ? (
                <p className="text-muted-foreground mt-2 text-xs">
                  {item.reason}
                </p>
              ) : null}
            </div>
          ))}
          {Object.entries(services.data?.metrics ?? {}).map(([name, value]) => (
            <div key={name} className="rounded-md border p-3">
              <p className="text-muted-foreground text-xs">{name}</p>
              <p className="text-lg font-bold tabular-nums">
                {value ?? "Unavailable"}
              </p>
              {name === "dlq_count" && services.data?.dlq_scope ? (
                <p className="text-muted-foreground mt-2 text-xs">
                  Scope: {services.data.dlq_scope}. Retained handoffs, not Kafka
                  queue depth.
                </p>
              ) : null}
              {name === "oldest_outbox_seconds" && value === null ? (
                <p className="text-muted-foreground mt-2 text-xs">
                  No pending outbox age measurement is available.
                </p>
              ) : null}
            </div>
          ))}
          {Object.entries(services.data?.observability ?? {}).map(
            ([name, measurement]) => (
              <div key={name} className="rounded-md border p-3">
                <p className="text-muted-foreground text-xs">{name}</p>
                <p className="text-lg font-bold tabular-nums">
                  {measurement.value ?? "Unavailable"}
                </p>
                <Badge variant="secondary">{measurement.status}</Badge>
                <p className="text-muted-foreground mt-2 text-xs">
                  Scope: {measurement.scope}
                </p>
                {measurement.reason ? (
                  <p className="text-muted-foreground mt-1 text-xs">
                    {measurement.reason}
                  </p>
                ) : null}
                {measurement.consumer_group ? (
                  <p className="text-muted-foreground mt-1 text-xs">
                    Consumer group: {measurement.consumer_group}
                  </p>
                ) : null}
                {measurement.topic ? (
                  <p className="text-muted-foreground mt-1 text-xs">
                    Topic: {measurement.topic}
                  </p>
                ) : null}
                {measurement.observed_at ? (
                  <p className="text-muted-foreground mt-1 text-xs">
                    Observed: {measurement.observed_at}
                  </p>
                ) : null}
              </div>
            ),
          )}
          {services.isPending ? <p role="status">Loading services...</p> : null}
        </CardContent>
      </Card>
      <Card>
        <CardHeader>
          <CardTitle>Recent registry audit</CardTitle>
        </CardHeader>
        <CardContent>
          {services.data?.recent_registry_audit ? (
            services.data.recent_registry_audit.length ? (
              <div className="overflow-x-auto">
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>Action</TableHead>
                      <TableHead>Resource</TableHead>
                      <TableHead>Time</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {services.data.recent_registry_audit.map((entry) => (
                      <TableRow key={entry.id}>
                        <TableCell>{entry.action}</TableCell>
                        <TableCell className="font-mono break-all">
                          {entry.resource_id}
                        </TableCell>
                        <TableCell>{entry.occurred_at}</TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </div>
            ) : (
              <p className="text-muted-foreground text-sm">
                No recent registry actions for this project.
              </p>
            )
          ) : (
            <p className="text-muted-foreground text-sm">
              Registry audit unavailable.
            </p>
          )}
        </CardContent>
      </Card>
      <Card>
        <CardHeader>
          <CardTitle>Workers</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="overflow-x-auto">
            <Table>
              <TableHeader>
                <TableRow>
                  {[
                    "Worker",
                    "Role",
                    "Version",
                    "Active / capacity",
                    "Last heartbeat",
                    "State",
                    "Action",
                  ].map((name) => (
                    <TableHead key={name}>{name}</TableHead>
                  ))}
                </TableRow>
              </TableHeader>
              <TableBody>
                {services.data?.workers.map((worker) => (
                  <TableRow key={worker.id}>
                    <TableCell className="font-mono break-all">
                      {worker.id}
                    </TableCell>
                    <TableCell>{worker.role}</TableCell>
                    <TableCell>{worker.version}</TableCell>
                    <TableCell>
                      {worker.active} / {worker.capacity}
                    </TableCell>
                    <TableCell>{worker.last_seen_at}</TableCell>
                    <TableCell>
                      {worker.alive === false
                        ? "Offline"
                        : worker.drain
                          ? "Draining"
                          : "Accepting work"}
                    </TableCell>
                    <TableCell>
                      <Button
                        variant="outline"
                        size="sm"
                        disabled={
                          drain.isPending ||
                          services.isError ||
                          !worker.project_id
                        }
                        title={
                          !worker.project_id
                            ? "Shared deployment workers require system administration."
                            : undefined
                        }
                        onClick={() =>
                          drain.mutate({
                            projectId,
                            workerId: worker.id,
                            drain: !worker.drain,
                          })
                        }
                      >
                        {worker.drain ? "Resume" : "Drain"}
                      </Button>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
          {services.data && !services.data.workers.length ? (
            <p className="text-muted-foreground mt-3 text-sm">
              No workers registered.
            </p>
          ) : null}
          <p className="text-muted-foreground mt-3 text-xs">
            Drain stops new assignments while active work completes. Browser and
            runtime workers remain separate roles.
          </p>
        </CardContent>
      </Card>
      {services.isError || drain.isError ? (
        <div role="alert" className="text-destructive text-sm">
          Service action unavailable. Refresh state before retrying.
          <Button
            variant="outline"
            className="ml-2"
            onClick={() => void services.refetch()}
          >
            Refresh services
          </Button>
        </div>
      ) : null}
    </div>
  );
}
