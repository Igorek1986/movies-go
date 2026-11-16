package store

import (
	"testing"
	"time"
)

func TestEpisodeSyncDue(t *testing.T) {
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	id, zero := 63875, 0
	ended, canceled, ongoing := "Ended", "Canceled", "Returning Series"
	cases := []struct {
		name    string
		id      *int
		status  *string
		tracked bool
		age     time.Duration
		never   bool
		want    bool
	}{
		{"unknown mapping", nil, &ongoing, true, 48 * time.Hour, false, false},
		{"zero mapping", &zero, &ongoing, true, 48 * time.Hour, false, false},
		{"first sync ended", &id, &ended, false, 0, true, true},
		{"between seasons without next date", &id, &ongoing, false, 24 * time.Hour, false, true},
		{"fresh ongoing", &id, &ongoing, false, 23 * time.Hour, false, false},
		{"tracked ended new season", &id, &ended, true, 24 * time.Hour, false, true},
		{"tracked ended fresh", &id, &ended, true, 23 * time.Hour, false, false},
		{"untracked ended not due", &id, &ended, false, 29 * 24 * time.Hour, false, false},
		{"untracked ended monthly", &id, &ended, false, 30 * 24 * time.Hour, false, true},
		{"untracked canceled monthly", &id, &canceled, false, 30 * 24 * time.Hour, false, true},
		{"unknown status", &id, nil, false, 24 * time.Hour, false, true},
		{"clock ahead", &id, &ongoing, true, -time.Hour, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			synced := now.Add(-tc.age)
			mc := &MediaCardEpInfo{MyshowsID: tc.id, Status: tc.status, Tracked: tc.tracked, EpisodesSyncedAt: &synced}
			if tc.never {
				mc.EpisodesSyncedAt = nil
			}
			if got := EpisodeSyncDue(mc, now); got != tc.want {
				t.Fatalf("due=%v, want %v", got, tc.want)
			}
		})
	}
}
