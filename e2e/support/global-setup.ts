import type { FullConfig } from '@playwright/test';

/**
 * Fail fast, and fail legibly.
 *
 * Without this the first spec dies on a connection refused deep inside
 * a `page.goto`, which reads like a Playwright problem rather than "you
 * forgot to start the app".  Checking /__test/health also verifies the
 * two things every spec below assumes: that the control surface is
 * mounted (dev build + YJ_TESTCTL=1) and that the app is pointed at the
 * seeded fixture library rather than someone's real collection.
 */
const HELP = `
The app is not running, or is not a seeded dev build.  Start it with:

  make testdata                     # once — generates the fixtures
  make sandbox-seed NAME=default    # once — builds a seed by running the app
  make dev-headless SEED=default    # starts in the background and returns

and stop it afterwards with 'make dev-stop'.
`;

export default async function globalSetup(config: FullConfig) {
  const baseURL =
    config.projects[0]?.use?.baseURL ?? 'http://localhost:34115';

  let health: any;

  try {
    const res = await fetch(`${baseURL}/__test/health`, {
      signal: AbortSignal.timeout(5_000),
    });

    if (!res.ok) throw new Error(`health returned ${res.status}`);

    health = await res.json();
  } catch (err) {
    throw new Error(
      `e2e: no healthy backend at ${baseURL} (${String(err)})\n${HELP}`,
    );
  }

  if (!health.libraries?.length) {
    throw new Error(
      `e2e: backend has no library configured — specs would land on ` +
        `the first-run wizard, which intercepts every pointer event.\n${HELP}`,
    );
  }

  const tracks = health.counts?.tracks ?? 0;

  if (tracks < 1) {
    throw new Error(
      `e2e: backend library is empty (${tracks} tracks).\n${HELP}`,
    );
  }

  console.log(
    `e2e: backend up — ${tracks} tracks in ` +
      `${health.libraries.map((l: any) => l.name).join(', ')}`,
  );
}
