package explore

import "fmt"

const (
	coverArtBaseURL      = "https://coverartarchive.org/release"
	coverArtGroupBaseURL = "https://coverartarchive.org/release-group"
)

// CoverArtURL returns the Cover Art Archive URL for the 250px
// front cover of the given release MBID.
func CoverArtURL(releaseMBID string) string {
	return fmt.Sprintf("%s/%s/front-250", coverArtBaseURL, releaseMBID)
}

// CoverArtURLSize returns the Cover Art Archive URL for the front
// cover of the given release MBID at the specified pixel size.
// Common sizes are 250, 500, and 1200.
func CoverArtURLSize(releaseMBID string, size int) string {
	return fmt.Sprintf("%s/%s/front-%d", coverArtBaseURL, releaseMBID, size)
}

// CoverArtGroupURL returns the Cover Art Archive URL for the 250px
// front cover of the given release group MBID.  Search results
// return release group MBIDs (not release MBIDs), so this is the
// correct endpoint for displaying cover art in search results.
func CoverArtGroupURL(releaseGroupMBID string) string {
	return fmt.Sprintf("%s/%s/front-250", coverArtGroupBaseURL, releaseGroupMBID)
}

// CoverArtGroupURLSize returns the Cover Art Archive URL for the
// front cover of the given release group MBID at the specified
// pixel size.  Common sizes are 250, 500, and 1200.
func CoverArtGroupURLSize(releaseGroupMBID string, size int) string {
	return fmt.Sprintf("%s/%s/front-%d", coverArtGroupBaseURL, releaseGroupMBID, size)
}
