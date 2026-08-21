/**
 * A nested rule whose selector starts with an element name is silently
 * dropped on the phone.
 *
 * The device renders in Chrome 113, which predates relaxed CSS nesting
 * (Chrome 120): before that a nested selector had to start with
 * something that could not be read as the beginning of a declaration,
 * so `.bottom-bar { audio-player { … } }` is not a parse error anyone
 * would notice — the inner rule simply does not exist, on the phone and
 * only on the phone. Three were live in `index.css`, one of them the
 * `text-overflow: ellipsis` on the bottom bar's title, which had
 * therefore never truncated on the device.
 *
 * `& audio-player` is valid in both syntaxes, so no nested rule here
 * has any reason to omit it.
 *
 * Two things the detection has to get right:
 *
 * - **A rule directly inside an at-rule is not nested.**
 *   `@media (…) { bottom-nav { … } }` at the top level is an ordinary
 *   rule and is fine — and it is the majority of the matches a regex
 *   over the file would produce. What decides it is whether a *style*
 *   rule is somewhere above, not what the immediate parent is: inside
 *   `.bar { @media (…) { audio-player { … } } }` the inner rule is
 *   nested, at-rule in between or not.
 * - **A declaration is not a rule.** `background: url(…)` and any
 *   string or comment can hold a brace, so this tracks them rather than
 *   matching lines.
 */

/** Does this selector start with an identifier, rather than a symbol? */
function startsWithIdent(selector) {
  return /^[A-Za-z_\u00A0-\uFFFF]/.test(selector);
}

/**
 * Every nested style rule in `css` whose selector starts with an
 * element name, as `{ line, selector }` with a 1-based line.
 */
export function findBareNestedRules(css) {
  const found = [];
  /** The blocks we are inside, innermost last: 'style' or 'at'. */
  const stack = [];
  /** The text since the last `{`, `}` or `;` — a prelude, if a `{` follows. */
  let prelude = '';
  let preludeLine = 1;
  let line = 1;

  const startPrelude = () => {
    prelude = '';
    preludeLine = line;
  };

  for (let i = 0; i < css.length; i++) {
    const c = css[i];

    if (c === '\n') {
      line++;
      if (prelude.trim() === '') preludeLine = line;
      prelude += c;
      continue;
    }

    if (c === '/' && css[i + 1] === '*') {
      const end = css.indexOf('*/', i + 2);
      const comment = css.slice(i, end === -1 ? css.length : end + 2);

      line += (comment.match(/\n/g) ?? []).length;
      i += comment.length - 1;
      if (prelude.trim() === '') preludeLine = line;
      continue;
    }

    if (c === '"' || c === "'") {
      let j = i + 1;

      while (j < css.length && css[j] !== c) {
        if (css[j] === '\\') j++;
        j++;
      }

      prelude += css.slice(i, j + 1);
      i = j;
      continue;
    }

    if (c === '{') {
      const selector = prelude.trim();
      const kind = selector.startsWith('@') ? 'at' : 'style';

      if (
        kind === 'style' &&
        stack.includes('style') &&
        startsWithIdent(selector)
      ) {
        found.push({ line: preludeLine, selector });
      }

      stack.push(kind);
      startPrelude();
      continue;
    }

    if (c === '}') {
      stack.pop();
      startPrelude();
      continue;
    }

    if (c === ';') {
      startPrelude();
      continue;
    }

    prelude += c;
  }

  return found;
}
