package main

import "time"

// Blizzard publishes each auction house about once an hour (gaps of 52 to 62 minutes were seen in November 2026,
// with an occasional skipped hour) and the project's mirror copies a new snapshot within about ten minutes. A house
// is asked about again only once its next snapshot can be there.
const (
	auctionSnapshotDue = 65 * time.Minute // after the snapshot's Last-Modified: Blizzard's hour plus the mirror's delay
	auctionDueEvery    = 10 * time.Minute // while the next snapshot is late
	auctionLateAfter   = 30 * time.Minute // late by this much: back to the old pace
)

// nextAuctionCheck is when a house should be asked about again: at once without a snapshot; otherwise when the next
// one is due, then every 10 minutes for half an hour and every 20 minutes after that, counted from the last try.
func nextAuctionCheck(modified, lastTry, now time.Time) time.Time {
	if modified.IsZero() {
		return now
	}
	due := modified.Add(auctionSnapshotDue)
	if now.Before(due) {
		return due
	}
	every := auctionDueEvery
	if now.Sub(due) >= auctionLateAfter {
		every = auctionEvery
	}
	next := lastTry.Add(every)
	if next.Before(due) {
		return due
	}
	return next
}
