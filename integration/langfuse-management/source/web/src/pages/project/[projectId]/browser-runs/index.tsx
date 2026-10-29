import { useRouter } from "next/router";
import Page from "@/src/components/layouts/page";
import { BrowserRunsPage } from "@/src/features/browser-runs/pages/BrowserRunsPage";

export default function BrowserRuns() {
  const router = useRouter();
  const projectId = router.query.projectId as string;

  return (
    <Page headerProps={{ title: "Agent runs" }}>
      <BrowserRunsPage key={projectId} projectId={projectId} />
    </Page>
  );
}
