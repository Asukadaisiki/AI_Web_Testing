import type { ReactNode } from "react";

import { NotebookNav } from "../components/NotebookNav";

type WorkspacePageLayoutProps = {
  title: string;
  description: string;
  actions?: ReactNode;
  children: ReactNode;
};

export function WorkspacePageLayout({
  title,
  description,
  actions,
  children,
}: WorkspacePageLayoutProps) {
  return (
    <div className="workspace-page">
      <aside className="workspace-sidebar">
        <div className="workspace-brand">AI Web Testing</div>
        <NotebookNav />
      </aside>
      <main className="workspace-main">
        <header className="workspace-header">
          <div>
            <h1>{title}</h1>
            <p>{description}</p>
          </div>
          {actions ? <div className="workspace-actions">{actions}</div> : null}
        </header>
        <div className="workspace-content">{children}</div>
      </main>
    </div>
  );
}
