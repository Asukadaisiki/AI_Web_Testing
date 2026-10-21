import { Activity, Plus, RefreshCw, Search } from 'lucide-react';
import { useMemo, useState } from 'react';
import type { WorkspaceState } from '../workspace/useWorkspace';

interface Props {
  workspace: WorkspaceState;
}

function shortId(value: string): string {
  return value.length > 12 ? `${value.slice(0, 8)}...` : value;
}

export function ProjectConversationPane({ workspace }: Props): JSX.Element {
  const [search, setSearch] = useState('');
  const [creating, setCreating] = useState<'project' | 'conversation' | null>(null);
  const [draft, setDraft] = useState('');

  const conversations = useMemo(() => {
    const query = search.trim().toLocaleLowerCase();
    if (query === '') return workspace.conversations;
    return workspace.conversations.filter((conversation) =>
      (conversation.title ?? conversation.id).toLocaleLowerCase().includes(query),
    );
  }, [search, workspace.conversations]);

  async function create(): Promise<void> {
    const value = draft.trim();
    if (value === '' || creating === null) return;
    if (creating === 'project') await workspace.addProject(value);
    else await workspace.addConversation(value);
    setDraft('');
    setCreating(null);
  }

  return (
    <aside className="pane project-pane" aria-label="Projects and conversations">
      <header className="pane-header brand-header">
        <div className="brand-mark" aria-hidden="true">
          AT
        </div>
        <div>
          <h1>AgentTrace</h1>
          <p>Eval Hub</p>
        </div>
        <button
          className="icon-button"
          type="button"
          aria-label="Refresh workspace data"
          title="Refresh"
          disabled={workspace.busy}
          onClick={() => void workspace.refresh()}
        >
          <RefreshCw size={16} />
        </button>
      </header>

      <section className="project-switcher" aria-labelledby="project-label">
        <div className="section-heading">
          <span id="project-label">Project</span>
          <button
            className="icon-button"
            type="button"
            aria-label="Create project"
            title="Create project"
            onClick={() => {
              setCreating('project');
              setDraft('');
            }}
          >
            <Plus size={15} />
          </button>
        </div>
        <select
          aria-label="Select project"
          value={workspace.selection.projectId ?? ''}
          onChange={(event) => workspace.selectProject(event.target.value)}
        >
          {workspace.projects.length === 0 ? <option value="">No projects</option> : null}
          {workspace.projects.map((project) => (
            <option key={project.id} value={project.id}>
              {project.name}
            </option>
          ))}
        </select>
      </section>

      <section className="conversation-section" aria-labelledby="conversation-label">
        <div className="section-heading">
          <span id="conversation-label">Conversations</span>
          <button
            className="icon-button"
            type="button"
            aria-label="Create conversation"
            title="Create conversation"
            disabled={workspace.selection.projectId === null}
            onClick={() => {
              setCreating('conversation');
              setDraft('');
            }}
          >
            <Plus size={15} />
          </button>
        </div>
        <label className="search-field">
          <Search size={14} aria-hidden="true" />
          <span className="sr-only">Search conversations</span>
          <input
            type="search"
            value={search}
            placeholder="Search"
            onChange={(event) => setSearch(event.target.value)}
          />
        </label>

        {creating !== null ? (
          <form
            className="inline-create"
            onSubmit={(event) => {
              event.preventDefault();
              void create();
            }}
          >
            <label>
              <span className="sr-only">
                {creating === 'project' ? 'Project name' : 'Conversation title'}
              </span>
              <input
                autoFocus
                value={draft}
                placeholder={creating === 'project' ? 'Project name' : 'Conversation title'}
                onChange={(event) => setDraft(event.target.value)}
              />
            </label>
            <div className="inline-actions">
              <button type="submit" className="button button-primary" disabled={draft.trim() === ''}>
                Create
              </button>
              <button type="button" className="button" onClick={() => setCreating(null)}>
                Cancel
              </button>
            </div>
          </form>
        ) : null}

        <ul className="conversation-list" aria-label="Conversation list">
          {conversations.map((conversation) => {
            const active = conversation.id === workspace.selection.conversationId;
            return (
              <li key={conversation.id}>
                <button
                  type="button"
                  className={`conversation-row${active ? ' is-selected' : ''}`}
                  aria-current={active ? 'true' : undefined}
                  onClick={() => workspace.selectConversation(conversation.id)}
                >
                  <span className="conversation-title">
                    {conversation.title?.trim() || 'Untitled conversation'}
                  </span>
                  <span className="conversation-meta">
                    <span>{shortId(conversation.id)}</span>
                    <time dateTime={conversation.updated_at}>
                      {new Date(conversation.updated_at).toLocaleDateString()}
                    </time>
                  </span>
                </button>
              </li>
            );
          })}
        </ul>
        {conversations.length === 0 ? (
          <div className="empty-state">
            <Activity size={18} />
            <span>{search === '' ? 'No conversations yet' : 'No matching conversations'}</span>
          </div>
        ) : null}
      </section>

      <footer className="health-strip">
        <span className={`health-dot health-${workspace.health?.status ?? 'down'}`} />
        <span>System {workspace.health?.status ?? 'unavailable'}</span>
      </footer>
    </aside>
  );
}
