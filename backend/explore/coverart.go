package explore

import "fmt"

const coverArtBaseURL = "https://coverartarchive.org/release"

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
