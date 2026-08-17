package explore

import (
	"context"

	"yellowjacket/backend/jobs"
)

// Fetching and merging the prebuilt catalog artifact.
//
// This is the whole catalog build on a user's machine.  The import that
// derives the catalog from the MetaBrainz dumps lives behind the
// `indexbuild` tag and runs only in CI (see dumpbuild_stub.go).

// Artifact build stages, shown in the jobs panel.
const (
	artifactStageDownload = iota
	artifactStageMerge
)

var artifactStageNames = [...]string{
	"Downloading catalog",
	"Merging catalog",
}

// tryCoreArtifact fetches and merges the prebuilt catalog.  Every
// failure path is non-fatal by design: the caller falls back, and a
// fresh install with no network still gets its own library in Explore.
func (si *SearchIndex) tryCoreArtifact(ctx context.Context) error {
	// Before anything is staged: ~0.6 GB is not a download to start on
	// someone's cellular allowance without being asked (plan 016 B4).
	// This is checked first so no job appears and no status changes --
	// declining is a no-op, not a failure the user has to dismiss.
	if si.netPolicy.refuses() {
		si.logIndexJob(
			jobs.LevelInfo,
			"Skipping the catalog download on a metered connection. "+
				"Enable it in Settings to download anyway.",
		)

		return ErrMeteredNetwork
	}

	si.mu.Lock()
	si.buildStatus = IndexStatus{
		Building: true,
		Tiers: []TierStatus{
			{Name: artifactStageNames[artifactStageDownload], State: "pending"},
			{Name: artifactStageNames[artifactStageMerge], State: "pending"},
		},
	}
	si.mu.Unlock()
	si.refreshStatusCounts()

	fetcher, err := newArtifactFetcher(si)
	if err != nil {
		return err
	}

	si.setTierStatus(artifactStageNames[artifactStageDownload], "running", 0, 0)
	si.logIndexJob(jobs.LevelInfo, "Fetching prebuilt catalog")

	path, err := fetcher.fetch(ctx)
	if err != nil {
		si.setTierStatus(artifactStageNames[artifactStageDownload], "error", 0, 0)

		return err
	}

	si.setTierStatus(artifactStageNames[artifactStageDownload], "complete", 0, 0)
	si.setTierStatus(artifactStageNames[artifactStageMerge], "running", 0, 0)

	if err := si.importCoreArtifact(ctx, path); err != nil {
		si.setTierStatus(artifactStageNames[artifactStageMerge], "error", 0, 0)
		si.removeArtifactFile(path)

		return err
	}

	si.removeArtifactFile(path)
	si.setTierStatus(artifactStageNames[artifactStageMerge], "complete", 0, 0)

	si.mu.Lock()
	si.buildStatus.Building = false
	si.mu.Unlock()

	// Fold the user's own library into the freshly-merged catalog:
	// owned entities the artifact does not cover are inserted, and
	// covered ones are flagged in_library.
	si.PopulateLocalCrossReferences()
	si.refreshStatusCounts()
	si.scheduleChampionRebuild()

	return nil
}

// artifactAlreadyMerged reports whether this index already carries a
// merged artifact, so a restart does not re-download one.
func (si *SearchIndex) artifactAlreadyMerged() bool {
	return si.hasMeta(coreArtifactVersionKey)
}
