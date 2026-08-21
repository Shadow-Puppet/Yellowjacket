/**
 * The nesting check's own semantics.
 *
 * `make css-check` runs it over a tree that currently has no violation,
 * so the check passing says nothing about whether it can still find
 * one. What it has to get right is two distinctions, and both are the
 * kind a regex over the file gets wrong: a rule directly inside an
 * at-rule is not nested, and a brace inside a string or a comment is
 * not a block.
 *
 * The rule it enforces is the device's: Chrome 113 predates relaxed CSS
 * nesting, so a nested selector starting with an element name is
 * dropped in silence. See `scripts/css-nesting.mjs`.
 */
import { describe, expect, it } from 'vitest';

import { findBareNestedRules } from '../../scripts/css-nesting.mjs';

describe('the nested-rule check', () => {
  it('flags a nested rule that starts with an element name', () => {
    const found = findBareNestedRules(
      '.bottom-bar {\n    color: red;\n\n    audio-player { margin: 0 }\n}',
    );

    expect(found).toEqual([{ line: 4, selector: 'audio-player' }]);
  });

  it('accepts the same rule written with a leading &', () => {
    expect(
      findBareNestedRules('.bottom-bar {\n    & audio-player { margin: 0 }\n}'),
    ).toEqual([]);
  });

  it('accepts a nested selector that starts with any other symbol', () => {
    expect(
      findBareNestedRules('.bar {\n    #track-info { color: red }\n}'),
    ).toEqual([]);
    expect(findBareNestedRules('.bar {\n    :host { color: red }\n}')).toEqual(
      [],
    );
  });

  it('leaves a top-level rule alone, element name or not', () => {
    expect(findBareNestedRules('p {\n    margin: 0;\n}')).toEqual([]);
  });

  /**
   * The majority of what a naive sweep would report: a media query at
   * the top level holds ordinary rules, not nested ones.
   */
  it('leaves a rule directly inside an at-rule alone', () => {
    expect(
      findBareNestedRules(
        '@media (max-width: 599px) {\n    bottom-nav { display: flex }\n}',
      ),
    ).toEqual([]);
  });

  /**
   * And the other half of that: what decides it is whether a style rule
   * is anywhere above, not what the immediate parent is.
   */
  it('flags one inside an at-rule that is itself inside a rule', () => {
    expect(
      findBareNestedRules(
        '.bar {\n    @media (min-width: 900px) {\n        audio-player { margin: 0 }\n    }\n}',
      ),
    ).toEqual([{ line: 3, selector: 'audio-player' }]);
  });

  it('reads through a brace in a string or a comment', () => {
    expect(
      findBareNestedRules('.a {\n    background: url("x{y}");\n}'),
    ).toEqual([]);
    expect(
      findBareNestedRules('.a {\n    /* audio-player { x: y } */\n}'),
    ).toEqual([]);
  });
});
