/**
 * Finding the `css` tagged templates in a TypeScript source.
 *
 * Two checks read them — the unterminated-comment one and the nesting
 * one — and a second scanner would be a second thing to keep in step
 * with how a template literal actually ends.
 */

/**
 * Find the end of a template literal that starts at `start` (the index
 * of its opening backtick), respecting escapes and `${}` substitutions.
 * Returns the index of the closing backtick, or -1.
 */
export function endOfTemplate(src, start) {
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

/**
 * Strip `${...}` substitutions, which may legitimately contain anything.
 *
 * Newlines inside them are kept, so a line number taken from the
 * stripped text still names the right line of the file it came from.
 */
export function stripSubstitutions(text) {
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
    else if (text[i] === '\n') out += '\n';
  }

  return out;
}

/** The 1-based line number of `index` in `src`. */
export function lineOf(src, index) {
  return src.slice(0, index).split('\n').length;
}

/**
 * Every tagged template literal in `src` whose tag is in `tags`.
 *
 * `body` has its substitutions stripped and `line` is the line its
 * opening backtick sits on, so `line + (n - 1)` is the file line of the
 * body's own line `n`.
 */
export function taggedLiterals(src, tags) {
  const pattern = new RegExp(`(^|[^\\w$.])(${tags.join('|')})\``, 'g');
  const found = [];

  let match;

  while ((match = pattern.exec(src)) !== null) {
    const open = match.index + match[0].length - 1;
    const close = endOfTemplate(src, open);

    if (close === -1) continue;

    found.push({
      tag: match[2],
      body: stripSubstitutions(src.slice(open + 1, close)),
      line: lineOf(src, open),
    });
  }

  return found;
}
