import { describe, expect, test } from 'vitest';

import {
  fixtureCleanupDiagnostics,
  fixtureCleanupIsSafe,
  type FixtureCleanupState,
} from './fixture-cleanup';

describe('fixture cleanup safety', () => {
  test('does not report a claimed unpublished command as safe to delete', () => {
    const state: FixtureCleanupState = {
      expectedRunCount: 1,
      runCount: 1,
      terminalRunCount: 1,
      unexpectedRunCount: 0,
      commandCount: 1,
      publishedCommandCount: 0,
      processedCommandCount: 0,
      unpublishedOutboxCount: 1,
      claimedUnpublishedCommandCount: 1,
    };

    expect(fixtureCleanupIsSafe(state)).toBe(false);
    expect(fixtureCleanupDiagnostics(state)).toContain('claimed_commands=1');
  });

  test('does not report an unexpected fixture run as safe to delete', () => {
    const state: FixtureCleanupState = {
      expectedRunCount: 1,
      runCount: 1,
      terminalRunCount: 1,
      unexpectedRunCount: 1,
      commandCount: 0,
      publishedCommandCount: 0,
      processedCommandCount: 0,
      unpublishedOutboxCount: 0,
      claimedUnpublishedCommandCount: 0,
    };

    expect(fixtureCleanupIsSafe(state)).toBe(false);
    expect(fixtureCleanupDiagnostics(state)).toContain('unexpected_runs=1');
  });
});
