#!/usr/bin/env node
/**
 * Fail on a nested rule whose selector starts with an element name.
 *
 * See `css-nesting.mjs` for what the phone does with one. No tier here
 * can see it: the component tier, the e2e tier and `make ui-visual` all
 * run a current Chromium, where the rule applies normally, so the only
 * report is a screenshot of the device — which is how the bottom bar's
 * title came to have never truncated there.
 *
 * It covers `index.css` and the `css` literals in the components alike,
 * because a shadow-root stylesheet is parsed by the same engine.
 */
import { globSync, readFileSync } from 'node:fs';

import { taggedLiterals } from './css-literals.mjs';
import { findBareNestedRules } from './css-nesting.mjs';

const problems = [];

for (const { line, selector } of findBareNestedRules(
  readFileSync('index.css', 'utf8'),
)) {
  problems.push({ file: 'index.css', line, selector });
}

const sources = globSync('src/**/*.ts', { cwd: process.cwd() });

// A sweep over an empty glob passes, and this one is expected to find
// nothing, so "it found nothing" has to mean it looked.
if (sources.length === 0) {
  console.error('css-nesting-check: no sources matched src/**/*.ts');
  process.exit(1);
}

for (const file of sources) {
  const src = readFileSync(file, 'utf8');

  for (const literal of taggedLiterals(src, ['css'])) {
    for (const { line, selector } of findBareNestedRules(literal.body)) {
      problems.push({ file, line: literal.line + line - 1, selector });
    }
  }
}

if (problems.length > 0) {
  for (const p of problems) {
    console.error(
      `${p.file}:${p.line}: nested rule "${p.selector.split('\n')[0]}" starts ` +
        'with an element name — write it as "& ' +
        `${p.selector.split('\n')[0]}"`,
    );
  }

  console.error(
    `\ncss-nesting-check: ${problems.length} problem(s). ` +
      'Chrome 113 (the device) drops a nested rule that does not start ' +
      'with a symbol; the leading & is valid in both syntaxes.',
  );
  process.exit(1);
}

console.log(
  `css-nesting-check: index.css + ${sources.length} files, no bare nested rules`,
);
