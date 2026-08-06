//go:build !indexbuild

package explore

import (
	"context"
	"errors"

	"yellowjacket/backend/jobs"
)

// The app binary does not carry the full dump import.
//
// Building the catalog from source means streaming ~89GB of listens dump
// from a server that caps a client near 2MB/s — better than half a day
// of downloading to derive a catalog that is identical for everyone.
// That work happens once in CI (`go build -tags indexbuild ./cmd/indexbuild`)
// and reaches users as a prebuilt artifact instead.
//
// So on the client the build path is: merge the artifact, then keep
// popularity current with the daily incremental dumps.  Artists outside
// the artifact's coverage still resolve lazily, exactly as before.

// runDumpBuild populates the catalog.  In the app build that means the
// prebuilt artifact and nothing else; the tagged build in
// dumpimport.go runs the real import.
func (si *SearchIndex) runDumpBuild(ctx context.Context) {
	si.MarkReadyIfPopulated()

	if si.hasMeta(dumpImportDoneKey) {
		si.logger.Info("search index: catalog already populated, skipping")
		si.refreshStatusCounts()

		return
	}

	if si.artifactAlreadyMerged() {
		si.refreshStatusCounts()

		return
	}

	if err := si.tryCoreArtifact(ctx); err != nil {
		if ctx.Err() != nil {
			return
		}

		si.logArtifactFallback(err)
		si.refreshStatusCounts()
	}
}

// logArtifactFallback explains why the catalog is not there.  An empty
// Explore with nothing in the log is the worst version of this failure.
func (si *SearchIndex) logArtifactFallback(err error) {
	switch {
	case errors.Is(err, ErrArtifactUnavailable):
		si.logger.Info("search index: no prebuilt catalog available", "error", err)
		si.logIndexJob(jobs.LevelWarn,
			"No prebuilt catalog available — Explore will cover your own "+
				"library only until one can be fetched.")

	case errors.Is(err, ErrArtifactVersion):
		si.logger.Warn("search index: prebuilt catalog is for a different app version",
			"error", err)
		si.logIndexJob(jobs.LevelWarn,
			"The published catalog does not match this app version; skipping it.")

	default:
		si.logger.Warn("search index: prebuilt catalog import failed", "error", err)
		si.logIndexJob(jobs.LevelWarn, "Prebuilt catalog import failed: "+err.Error())
	}
}
