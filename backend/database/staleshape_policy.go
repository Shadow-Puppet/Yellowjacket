//go:build !indexbuild

package database

// retireStaleCache reports whether a Cache table whose shape no longer
// matches the schema may be dropped and rebuilt.
//
// In the app: yes.  The only Cache table large enough to care about is
// the catalog, and the app does not derive it — it downloads it.  A
// stale one costs about a minute of re-fetching the artifact, and
// keeping it costs every Explore read on the install, because a
// projection naming a column the table does not have fails outright.
//
// In cmd/indexbuild: no, and the file next to this one says why.
const retireStaleCache = true
