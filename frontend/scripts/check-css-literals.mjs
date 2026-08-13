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
import { readFileSync } from 'node:fs';
import { globSync } from 'node:fs';

const TAGS = ['css', 'html', 'svg'];

/**
 * Find the end of a template literal that starts at `start` (the index
 * of its opening backtick), respecting escapes and `${}` substitutions.
 * Returns the index of the closing backtick, or -1.
 */
function endOfTemplate(src, start) {
  let depth = 0;

  for (let i = start + 1; i < src.length; i++) {
    const c = src[i];

    if (c === '\\') {
      i++;
      continue;
    }

    if (c === '$' && src[i + 1] === '{') {
      depth++;
      i++;
      continue;
    }

    if (c === '}' && depth > 0) {
      depth--;
      continue;
    }

    if (c === '`' && depth === 0) return i;
  }

  return -1;
}

/** Strip `${...}` substitutions, which may legitimately contain anything. */
function stripSubstitutions(text) {
  let out = '';
  let depth = 0;

  for (let i = 0; i < text.length; i++) {
    if (text[i] === '$' && text[i + 1] === '{') {
      depth++;
      i++;
      continue;
    }

    if (text[i] === '}' && depth > 0) {
      depth--;
      continue;
    }

    if (depth === 0) out += text[i];
  }

  return out;
}

function lineOf(src, index) {
  return src.slice(0, index).split('\n').length;
}

const files = globSync('src/**/*.ts', { cwd: process.cwd() });
const problems = [];

for (const file of files) {
  const src = readFileSync(file, 'utf8');
  const tagPattern = new RegExp(`(^|[^\\w$.])(${TAGS.join('|')})\``, 'g');

  let match;

  while ((match = tagPattern.exec(src)) !== null) {
    const open = match.index + match[0].length - 1;
    const close = endOfTemplate(src, open);

    if (close === -1) continue;

    const body = stripSubstitutions(src.slice(open + 1, close));
    const opens = (body.match(/\/\*/g) ?? []).length;
    const closes = (body.match(/\*\//g) ?? []).length;

    if (opens > closes) {
      problems.push({
        file,
        line: lineOf(src, open),
        tag: match[2],
      });
    }
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
