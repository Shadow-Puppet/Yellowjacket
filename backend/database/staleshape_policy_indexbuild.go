//go:build indexbuild

package database

// retireStaleCache is false here, and this is the whole reason the
// policy is a build tag rather than a rule inside retireStaleTables.
//
// The index database is the one place in this project where the catalog
// is *derived* rather than downloaded.  Rebuilding it is a ~205 GB dump
// stream over hours, resumed across runs from a checkpoint on a
// persistent volume; that volume exists for no other purpose.  The app's
// answer to a stale catalog — drop it, fetch the artifact again — is
// not available here, because this database *is* what the artifact is
// cut from.
//
// This was not hypothetical.  The repair shipped without it and dropped
// the CI catalog on its first run:
//
//	retiring a table ... table=explore_index
//	  reason="column entity_type is TEXT, schema declares INTEGER"
//	index maintenance mode=build reason="no completed import yet"
//
// The shape mismatch was real and the drop was correct by the app's
// rule.  It was still wrong here: that database is deliberately kept in
// the older encoding, which is what `fix(indexexport): read an index
// older than the binary` exists to tolerate.  A rule that is right for
// every install and catastrophic for one database has to be told which
// one it is in, and a build tag is how this project already tells the
// index tools apart (backend/events/runtime_indexbuild.go,
// backend/explore/servicestartup.go, dumpbuild_stub.go).
//
// cmd/indexbuild has its own repair for the half it *can* safely
// discard: retireLibraryTables drops every table the datamap does not
// classify as Cache, which is empty by construction in that database.
// Between the two, the library half is repaired and the catalog is
// never touched.
const retireStaleCache = false
