#!/usr/bin/env node
/**
 * A backtick inside a comment in a `css` tagged template literal ends
 * the literal.
 *
 * This has cost four sessions across three plans. It is written down in
 * CLAUDE.md, in the yellowjacket-dev skill and in NOTES.md, and it was
 * read twice in the session it then cost a cycle in — so it is a check
 * now rather than a fourth paragraph. Knowledge that has been ignored
 * three times is not a knowledge problem.
 *
 * What makes it expensive is not the mistake but the *report*. The
 * literal ends early, the rest of the CSS is parsed as JavaScript, and
 * what comes back is `Expected "]" but found "inline"` pointing at a
 * line of prose — or, when it happens in a shared module like
 * `tokens.css.ts`, every test in the suite failing to import and an
 * output that reads like a broken test runner. And `make dev-headless`
 * leaves the dev server serving the last good bundle, so the page still
 * works and still shows the old behaviour.
 *
 * The detection is exact rather than heuristic. If a backtick inside a
 * comment closed the literal early, then the text the parser *did* take
 * as the literal contains an unterminated `/*`. Nothing else produces
 * that, and a legitimate literal cannot contain one.
 */
import { globSync, readFileSync } from 'node:fs';

import { taggedLiterals } from './css-literals.mjs';

const TAGS = ['css', 'html', 'svg'];

const files = globSync('src/**/*.ts', { cwd: process.cwd() });
const problems = [];

for (const file of files) {
  const src = readFileSync(file, 'utf8');

  for (const { tag, body, line } of taggedLiterals(src, TAGS)) {
    const opens = (body.match(/\/\*/g) ?? []).length;
    const closes = (body.match(/\*\//g) ?? []).length;

    if (opens > closes) problems.push({ file, line, tag });
  }
}

if (problems.length > 0) {
  for (const p of problems) {
    console.error(
      `${p.file}:${p.line}: unterminated /* inside a ${p.tag}\`\` literal — ` +
        'a backtick in a comment ends the literal early',
    );
  }

  console.error(
    `\ncss-literal-check: ${problems.length} problem(s). ` +
      'Remove the backticks from the comment; markdown quoting does not ' +
      'survive a tagged template.',
  );
  process.exit(1);
}

console.log(`css-literal-check: ${files.length} files, no broken literals`);
