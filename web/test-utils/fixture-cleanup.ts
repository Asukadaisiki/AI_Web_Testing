export interface FixtureCleanupState {
  expectedRunCount: number;
  runCount: number;
  terminalRunCount: number;
  unexpectedRunCount: number;
  commandCount: number;
  publishedCommandCount: number;
  processedCommandCount: number;
  unpublishedOutboxCount: number;
  claimedUnpublishedCommandCount: number;
}

export function fixtureCleanupIsSafe(state: FixtureCleanupState): boolean {
  return (
    state.runCount === state.expectedRunCount &&
    state.runCount === state.terminalRunCount &&
    state.unexpectedRunCount === 0 &&
    state.commandCount === state.publishedCommandCount &&
    state.commandCount === state.processedCommandCount &&
    state.unpublishedOutboxCount === 0
  );
}

export function fixtureCleanupDiagnostics(state: FixtureCleanupState): string {
  return [
    `runs=${state.terminalRunCount}/${state.runCount}/${state.expectedRunCount}`,
    `unexpected_runs=${state.unexpectedRunCount}`,
    `commands=${state.processedCommandCount}/${state.publishedCommandCount}/${state.commandCount}`,
    `unpublished_outbox=${state.unpublishedOutboxCount}`,
    `claimed_commands=${state.claimedUnpublishedCommandCount}`,
  ].join(' ');
}
