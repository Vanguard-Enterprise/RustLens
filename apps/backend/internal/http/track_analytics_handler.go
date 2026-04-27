package httpapi

import (
	"context"
	"errors"
	"log"
	"net/http"
	"sort"
	"sync"
	"time"

	"rustlens/backend/internal/battlemetrics"
	"rustlens/backend/internal/store"
)

const (
	trackAnalyticsCacheTTL    = 24 * time.Hour
	trackAnalyticsPageSize    = 100
	trackAnalyticsMaxPages    = 2
	trackAnalyticsMinDayMins  = 30
	trackAnalyticsRefDayUnix  = int64(1767571200) // 2026-01-05 00:00:00 UTC = Monday
	trackAnalyticsHoursPerDay = 24
)

var trackAnalyticsWeekdayNames = [7]string{
	"Monday",
	"Tuesday",
	"Wednesday",
	"Thursday",
	"Friday",
	"Saturday",
	"Sunday",
}

type trackAnalyticsHourValue struct {
	HourUnix int64   `json:"hourUnix"`
	Avg      float64 `json:"avg"`
}

type trackAnalyticsDayValue struct {
	Weekday   string                    `json:"weekday"`
	BestStart int64                     `json:"bestStart"`
	BestEnd   int64                     `json:"bestEnd"`
	Hours     []trackAnalyticsHourValue `json:"hours"`
}

type trackAnalyticsResponse struct {
	GroupName  string                   `json:"groupName"`
	ServerName string                   `json:"serverName"`
	Members    int                      `json:"members"`
	Analyzed   int                      `json:"analyzed"`
	Days       []trackAnalyticsDayValue `json:"days"`
}

func (h *Handler) GetTrackGroupAnalytics(w http.ResponseWriter, r *http.Request) {
	guildID, ok := validateGuildID(w, r)
	if !ok {
		return
	}
	id, ok := parseTrackGroupID(w, r)
	if !ok {
		return
	}

	group, err := h.postgres.GetTrackGroup(r.Context(), guildID, id)
	if errors.Is(err, store.ErrTrackGroupNotFound) {
		writeError(w, http.StatusNotFound, "track group not found")
		return
	}
	if err != nil {
		log.Printf("[backend] GetTrackGroup error for guild %s: %v", guildID, err)
		writeError(w, http.StatusInternalServerError, "failed to load track group")
		return
	}

	members, err := h.postgres.ListTrackAnalyticsMembers(r.Context(), id)
	if err != nil {
		log.Printf("[backend] ListTrackAnalyticsMembers error for group %d: %v", id, err)
		writeError(w, http.StatusInternalServerError, "failed to load track group members")
		return
	}

	serverName := group.ServerBmID
	if servers, err := h.postgres.ListTrackedServers(r.Context()); err == nil {
		for _, srv := range servers {
			if srv.BmID == group.ServerBmID {
				serverName = srv.Name
				break
			}
		}
	}

	playerSessions := h.fetchTrackAnalyticsSessions(r.Context(), members, group.ServerBmID)

	days := computeTrackAnalyticsDays(playerSessions)

	writeJSON(w, http.StatusOK, trackAnalyticsResponse{
		GroupName:  group.Name,
		ServerName: serverName,
		Members:    len(members),
		Analyzed:   len(playerSessions),
		Days:       days,
	})
}

// fetchTrackAnalyticsSessions returns one entry per member (with valid sessions
// data) keyed by player_bm_id. Cache hits within the TTL skip the BattleMetrics
// fetch; misses are dispatched concurrently with concurrency equal to the number
// of available proxies (or 1 when proxying is disabled).
func (h *Handler) fetchTrackAnalyticsSessions(ctx context.Context, members []store.TrackAnalyticsMember, serverBmID string) map[string][]store.TrackAnalyticsSession {
	now := time.Now()
	results := make(map[string][]store.TrackAnalyticsSession, len(members))
	toFetch := make([]store.TrackAnalyticsMember, 0, len(members))

	for _, member := range members {
		entry, err := h.postgres.GetTrackAnalyticsCache(ctx, member.BmID, serverBmID)
		if err != nil {
			log.Printf("[backend] track analytics cache lookup failed for player %s: %v", member.BmID, err)
		}
		if entry != nil && now.Sub(entry.FetchedAt) < trackAnalyticsCacheTTL {
			results[member.BmID] = entry.Sessions
			continue
		}
		toFetch = append(toFetch, member)
	}

	if len(toFetch) == 0 {
		return results
	}

	concurrency := 1
	if h.cfg.ProxyEnabled && len(h.cfg.ProxyEndpoints) > 0 {
		concurrency = len(h.cfg.ProxyEndpoints)
	}
	if concurrency > len(toFetch) {
		concurrency = len(toFetch)
	}

	type fetchResult struct {
		bmID     string
		sessions []store.TrackAnalyticsSession
	}

	jobs := make(chan store.TrackAnalyticsMember, len(toFetch))
	out := make(chan fetchResult, len(toFetch))

	var wg sync.WaitGroup
	for w := 0; w < concurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for member := range jobs {
				sessions, err := h.battleMetricsClient.FetchPlayerSessions(ctx, member.BmID, serverBmID, trackAnalyticsPageSize, trackAnalyticsMaxPages)
				if err != nil {
					log.Printf("[backend] track analytics fetch failed for player %s: %v", member.BmID, err)
					out <- fetchResult{bmID: member.BmID}
					continue
				}
				converted := convertBmSessions(sessions)
				if upsertErr := h.postgres.UpsertTrackAnalyticsCache(ctx, member.BmID, serverBmID, converted); upsertErr != nil {
					log.Printf("[backend] track analytics cache write failed for player %s: %v", member.BmID, upsertErr)
				}
				out <- fetchResult{bmID: member.BmID, sessions: converted}
			}
		}()
	}

	for _, member := range toFetch {
		jobs <- member
	}
	close(jobs)
	wg.Wait()
	close(out)

	for result := range out {
		results[result.bmID] = result.sessions
	}
	return results
}

func convertBmSessions(input []battlemetrics.PlayerSession) []store.TrackAnalyticsSession {
	out := make([]store.TrackAnalyticsSession, 0, len(input))
	for _, s := range input {
		if s.Start == nil || s.Stop == nil {
			continue
		}
		start := s.Start.UTC().Unix()
		stop := s.Stop.UTC().Unix()
		if stop <= start {
			continue
		}
		out = append(out, store.TrackAnalyticsSession{StartUnix: start, StopUnix: stop})
	}
	return out
}

// computeTrackAnalyticsDays produces the per-weekday analytics from the raw
// session data of every analyzed player. The math:
//
//   For each player:
//     - Group the player's session-time by UTC date.
//     - Drop any date where the player accumulated < 30 minutes total online.
//     - For each remaining date, compute the per-hour fraction (0.0-1.0) of that
//       hour the player was online, then bucket those fractions per weekday.
//     - Average per (weekday, hour) across the player's distinct dates for that
//       weekday — this is the player's contribution to that hour.
//   The (weekday, hour) result is the SUM of every player's contribution: the
//   "average people online" at that weekday-hour.
//
// Best window per weekday: the 2-hour contiguous block (wrapping at midnight)
// with the lowest summed avg-people-online.
func computeTrackAnalyticsDays(sessions map[string][]store.TrackAnalyticsSession) []trackAnalyticsDayValue {
	totals := [7][trackAnalyticsHoursPerDay]float64{}

	for _, playerSessions := range sessions {
		perPlayer := buildPlayerWeekdayHourFractions(playerSessions)
		for weekday := 0; weekday < 7; weekday++ {
			for hour := 0; hour < trackAnalyticsHoursPerDay; hour++ {
				totals[weekday][hour] += perPlayer[weekday][hour]
			}
		}
	}

	out := make([]trackAnalyticsDayValue, 0, 7)
	for weekday := 0; weekday < 7; weekday++ {
		dayBaseUnix := trackAnalyticsRefDayUnix + int64(weekday)*86400
		hourValues := make([]trackAnalyticsHourValue, 0, trackAnalyticsHoursPerDay)
		for hour := 0; hour < trackAnalyticsHoursPerDay; hour++ {
			hourValues = append(hourValues, trackAnalyticsHourValue{
				HourUnix: dayBaseUnix + int64(hour)*3600,
				Avg:      roundTo2(totals[weekday][hour]),
			})
		}

		bestStart, bestEnd := bestOfflineWindow(totals[weekday], dayBaseUnix)

		out = append(out, trackAnalyticsDayValue{
			Weekday:   trackAnalyticsWeekdayNames[weekday],
			BestStart: bestStart,
			BestEnd:   bestEnd,
			Hours:     sortedHourValues(hourValues),
		})
	}
	return out
}

// buildPlayerWeekdayHourFractions returns, for one player, a [7][24]float64
// matrix where each cell is the average fraction (0..1) of that weekday-hour
// the player was online — averaged across distinct dates with >= 30 minutes
// of total play.
func buildPlayerWeekdayHourFractions(playerSessions []store.TrackAnalyticsSession) [7][trackAnalyticsHoursPerDay]float64 {
	type dateBucket struct {
		totalSeconds  int64
		hourFractions [trackAnalyticsHoursPerDay]float64
	}

	buckets := make(map[int64]*dateBucket)
	for _, session := range playerSessions {
		distributeSessionAcrossDays(session.StartUnix, session.StopUnix, func(dayUnix int64, hour int, seconds int64) {
			bucket, ok := buckets[dayUnix]
			if !ok {
				bucket = &dateBucket{}
				buckets[dayUnix] = bucket
			}
			bucket.totalSeconds += seconds
			bucket.hourFractions[hour] += float64(seconds) / 3600.0
		})
	}

	type weekdayAccumulator struct {
		sum  [trackAnalyticsHoursPerDay]float64
		days int
	}

	weekdayAcc := [7]weekdayAccumulator{}
	threshold := int64(trackAnalyticsMinDayMins) * 60
	for dayUnix, bucket := range buckets {
		if bucket.totalSeconds < threshold {
			continue
		}
		weekday := utcWeekdayMondayZero(dayUnix)
		acc := &weekdayAcc[weekday]
		acc.days++
		for h := 0; h < trackAnalyticsHoursPerDay; h++ {
			fraction := bucket.hourFractions[h]
			if fraction > 1.0 {
				fraction = 1.0
			}
			acc.sum[h] += fraction
		}
	}

	out := [7][trackAnalyticsHoursPerDay]float64{}
	for weekday := 0; weekday < 7; weekday++ {
		acc := weekdayAcc[weekday]
		if acc.days == 0 {
			continue
		}
		for h := 0; h < trackAnalyticsHoursPerDay; h++ {
			out[weekday][h] = acc.sum[h] / float64(acc.days)
		}
	}
	return out
}

// distributeSessionAcrossDays calls fn for each (UTC day midnight, hour, seconds)
// that the session covers, splitting across day boundaries as needed.
func distributeSessionAcrossDays(startUnix, stopUnix int64, fn func(dayUnix int64, hour int, seconds int64)) {
	if stopUnix <= startUnix {
		return
	}
	cursor := startUnix
	for cursor < stopUnix {
		dayUnix := cursor - mod(cursor, 86400)
		hour := int(mod(cursor, 86400) / 3600)
		hourEnd := dayUnix + (int64(hour)+1)*3600
		segmentEnd := hourEnd
		if segmentEnd > stopUnix {
			segmentEnd = stopUnix
		}
		seconds := segmentEnd - cursor
		if seconds > 0 {
			fn(dayUnix, hour, seconds)
		}
		cursor = segmentEnd
	}
}

func mod(value, divisor int64) int64 {
	r := value % divisor
	if r < 0 {
		r += divisor
	}
	return r
}

// utcWeekdayMondayZero returns 0=Monday..6=Sunday for the given UTC unix day
// (must be a midnight epoch).
func utcWeekdayMondayZero(dayUnix int64) int {
	t := time.Unix(dayUnix, 0).UTC()
	w := int(t.Weekday()) // 0=Sunday..6=Saturday
	w--
	if w < 0 {
		w = 6
	}
	return w
}

// bestOfflineWindow finds a 2-hour contiguous window (wrapping past midnight)
// with the lowest sum of avg-people-online and returns the start/end unix
// timestamps relative to dayBaseUnix.
func bestOfflineWindow(hours [trackAnalyticsHoursPerDay]float64, dayBaseUnix int64) (int64, int64) {
	bestSum := -1.0
	bestHour := 0
	for h := 0; h < trackAnalyticsHoursPerDay; h++ {
		sum := hours[h] + hours[(h+1)%trackAnalyticsHoursPerDay]
		if bestSum < 0 || sum < bestSum {
			bestSum = sum
			bestHour = h
		}
	}
	start := dayBaseUnix + int64(bestHour)*3600
	end := start + 2*3600
	return start, end
}

func sortedHourValues(values []trackAnalyticsHourValue) []trackAnalyticsHourValue {
	sort.SliceStable(values, func(i, j int) bool {
		if values[i].Avg == values[j].Avg {
			return values[i].HourUnix < values[j].HourUnix
		}
		return values[i].Avg > values[j].Avg
	})
	return values
}

func roundTo2(value float64) float64 {
	return float64(int64(value*100+0.5)) / 100
}
