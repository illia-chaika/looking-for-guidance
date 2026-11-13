package main

import (
	"cmp"
	"slices"
	"time"
)

// payloadLimit is one window of the adviser's allowance, as its provider reports it. The game shows it beside the
// conversation's context; nothing is shown for a provider or an account that reports none.
type payloadLimit struct {
	Mins   int64 `json:"mins"`             // length of the window
	Left   int   `json:"left"`             // percent of the window not used yet
	Resets int64 `json:"resets,omitempty"` // Unix time when the window starts over
}

// Codex names a window by its length, not by its place: an account with one weekly window reports it as "primary".
type cxRateWindow struct {
	UsedPercent int    `json:"usedPercent"`
	Mins        *int64 `json:"windowDurationMins"`
	ResetsAt    *int64 `json:"resetsAt"`
}

type cxRateLimits struct {
	Primary   *cxRateWindow `json:"primary"`
	Secondary *cxRateWindow `json:"secondary"`
}

func (r cxRateLimits) windows() []payloadLimit {
	var list []payloadLimit
	for _, w := range []*cxRateWindow{r.Primary, r.Secondary} {
		if w == nil || w.Mins == nil || *w.Mins <= 0 {
			continue
		}
		limit := payloadLimit{Mins: *w.Mins, Left: min(max(100-w.UsedPercent, 0), 100)}
		if w.ResetsAt != nil {
			limit.Resets = *w.ResetsAt
		}
		list = append(list, limit)
	}
	return sortedLimits(list)
}

// Claude Code names its windows; the lengths are the ones its plans use.
var claudeWindowMins = map[string]int64{"five_hour": 5 * 60, "seven_day": 7 * 24 * 60}

type claudeRateLimit struct {
	Windows map[string]struct {
		Utilization float64 `json:"utilization"`
		ResetsAt    int64   `json:"resetsAt"`
	} `json:"unifiedWindows"`
}

func (r claudeRateLimit) windows() []payloadLimit {
	var list []payloadLimit
	for name, w := range r.Windows {
		if mins := claudeWindowMins[name]; mins > 0 && w.Utilization >= 0 {
			list = append(list, payloadLimit{Mins: mins, Left: min(max(100-int(w.Utilization*100+0.5), 0), 100), Resets: w.ResetsAt})
		}
	}
	return sortedLimits(list)
}

func sortedLimits(list []payloadLimit) []payloadLimit {
	slices.SortFunc(list, func(a, b payloadLimit) int { return cmp.Compare(a.Mins, b.Mins) })
	return list
}

// readLimits asks Codex for the account's allowance. An account without one, such as an API key, answers with an
// error, and then nothing is shown.
func (s *session) readLimits(now time.Time) {
	var r struct {
		RateLimits cxRateLimits `json:"rateLimits"`
	}
	err := s.client.call("account/rateLimits/read", struct{}{}, &r)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastLimits = now
	if err != nil {
		if s.codexLimits != nil || !s.limitsRefused {
			logf("Codex reports no allowance for this account: %v", err)
		}
		s.codexLimits, s.limitsRefused = nil, true
		return
	}
	s.limitsRefused = false
	if got := r.RateLimits.windows(); !slices.Equal(got, s.codexLimits) {
		s.codexLimits, s.dirty = got, true
	}
}

// setClaudeLimits keeps what Claude Code said in its latest turn, a chat or a journal job.
func (s *session) setClaudeLimits(list []payloadLimit) {
	if len(list) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !slices.Equal(list, s.claudeLimits) {
		s.claudeLimits, s.dirty = list, true
	}
}
