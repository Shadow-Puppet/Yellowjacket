/*
 * Vendor the icon set into the repo.
 *
 * The app used to fetch every `<wa-icon>` from ka-f.fontawesome.com at
 * runtime, which meant it had no icons at all offline, on a captive
 * portal or behind a firewall (audit H-4 / perf.M9).  Bundling them is
 * the fix; this script is how the bundle is produced, so "where did
 * these SVGs come from" has an answer that is a command rather than a
 * memory.
 *
 * IT MUST BE FONT AWESOME **FREE**.  The kit CDN the app was hitting
 * serves *Pro* SVGs — every file carries a "Commercial License"
 * comment — and those cannot be redistributed in this repository.  The
 * Free set is CC BY 4.0, which can, with attribution; LICENSE.txt is
 * copied next to the icons for exactly that reason.  Every name the app
 * uses happens to exist in Free, so this costs nothing visually, but
 * a future addition might not: if a name is missing here, pick a
 * different icon rather than reaching for the Pro one.
 *
 * Usage:
 *   node frontend/scripts/fetch-icons.mjs
 *
 * Reads names from src/icons/names.txt, writes src/assets/icons/fa/.
 */

import { execFileSync } from 'node:child_process';
import {
	copyFileSync, existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync,
	readdirSync,
} from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const HERE = dirname(fileURLToPath(import.meta.url));
const FRONTEND = resolve(HERE, '..');
const NAMES = resolve(FRONTEND, 'src/icons/names.txt');
const DEST = resolve(FRONTEND, 'src/assets/icons/fa');
const PKG = '@fortawesome/fontawesome-free@7.3.1';

const names = readFileSync(NAMES, 'utf8')
	.split('\n')
	.map((l) => l.replace(/#.*$/, '').trim())
	.filter(Boolean);

const work = mkdtempSync(join(tmpdir(), 'yj-icons-'));

try {
	execFileSync('npm', ['pack', PKG], { cwd: work, stdio: 'pipe' });
	const tgz = readdirSync(work).find((f) => f.endsWith('.tgz'));
	execFileSync('tar', ['xf', tgz], { cwd: work });

	const src = join(work, 'package');
	rmSync(DEST, { recursive: true, force: true });

	for (const name of names) {
		const from = join(src, 'svgs', `${name}.svg`);
		if (!existsSync(from)) {
			console.error(
				`fetch-icons: '${name}' is not in Font Awesome Free.\n` +
				'  Pick an icon that is; do not vendor the Pro version.',
			);
			process.exit(1);
		}

		const to = join(DEST, `${name}.svg`);
		mkdirSync(dirname(to), { recursive: true });
		copyFileSync(from, to);
	}

	copyFileSync(join(src, 'LICENSE.txt'), join(DEST, 'LICENSE.txt'));
	console.log(`fetch-icons: vendored ${names.length} icons from ${PKG}`);
} finally {
	rmSync(work, { recursive: true, force: true });
}
