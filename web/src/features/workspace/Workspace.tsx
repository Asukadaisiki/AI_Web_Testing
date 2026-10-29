import { ListTree, MessagesSquare, PanelLeft } from 'lucide-react';
import { useState } from 'react';
import { ConversationPane } from '../conversation/ConversationPane';
import { useExecutionCatalog } from '../harness/useExecutionCatalog';
import { ProjectConversationPane } from '../projects/ProjectConversationPane';
import { TracePane } from '../trace/TracePane';
import { useWorkspace } from './useWorkspace';

type Pane = 'projects' | 'conversation' | 'trace';

export function Workspace(): JSX.Element {
  const workspace = useWorkspace({ legacyAgents: false });
  const catalog = useExecutionCatalog();
  const [activePane, setActivePane] = useState<Pane>('conversation');

  return (
    <div className="workspace-shell">
      <nav className="pane-tabs" aria-label="Workspace panes">
        <button
          type="button"
          aria-label="Show projects and conversations"
          aria-pressed={activePane === 'projects'}
          className={activePane === 'projects' ? 'is-active' : ''}
          onClick={() => setActivePane('projects')}
        >
          <PanelLeft size={16} />
          Projects
        </button>
        <button
          type="button"
          aria-label="Show conversation"
          aria-pressed={activePane === 'conversation'}
          className={activePane === 'conversation' ? 'is-active' : ''}
          onClick={() => setActivePane('conversation')}
        >
          <MessagesSquare size={16} />
          Conversation
        </button>
        <button
          type="button"
          aria-label="Show trace"
          aria-pressed={activePane === 'trace'}
          className={activePane === 'trace' ? 'is-active' : ''}
          onClick={() => setActivePane('trace')}
        >
          <ListTree size={16} />
          Trace
        </button>
      </nav>

      <div className="workspace">
        <div className={`pane-slot pane-slot-left${activePane === 'projects' ? ' is-active' : ''}`}>
          <ProjectConversationPane workspace={workspace} />
        </div>
        <div className={`pane-slot pane-slot-center${activePane === 'conversation' ? ' is-active' : ''}`}>
          <ConversationPane workspace={workspace} catalog={catalog} />
        </div>
        <div className={`pane-slot pane-slot-right${activePane === 'trace' ? ' is-active' : ''}`}>
          <TracePane workspace={workspace} />
        </div>
      </div>

      {workspace.loading ? (
        <div className="workspace-loading" role="status">
          Loading workspace...
        </div>
      ) : null}
    </div>
  );
}
