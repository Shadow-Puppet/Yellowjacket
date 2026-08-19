# Changelog

The changelog is the releases page:

<https://git.ljones.me/yonlu/yellowjacket/releases>

Every release there is generated from the Conventional Commits it
contains, by `.gitea/workflows/release.yml`. Each one carries its notes
as its body, grouped by change type, with a link to the commit behind
every line.

That workflow is **run by hand**, so a release holds everything merged
since the last one rather than one PR's worth. It used to fire on every
push to `main`, which made a version per merged PR (issue #115).

**This file is not generated and is not a copy of that.** `main` is a
protected branch, so nothing pushes a changelog commit back to it — and a
file that claimed to be a changelog while silently never updating would
be worse than no file at all. `make release-dry` prints what a release
run would cut right now, and the workflow's own `dry_run` input answers
the same question from CI.

History before `v0.0.1` is in `git log`. The versions before it were cut
by hand and are not on the releases page; the entries this file used to
hold were generated against a GitHub remote this project no longer has,
and every link in them was dead.
