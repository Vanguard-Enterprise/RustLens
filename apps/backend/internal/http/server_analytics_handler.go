package httpapi

import (
	"errors"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"rustlens/backend/internal/battlemetrics"
	"rustlens/backend/internal/store"
)

const (
	serverAnalyticsCacheTimeframe = 90
)

var serverAnalyticsTimeframes = map[string]int{
	"7d":  7,
	"14d": 14,
	"30d": 30,
	"60d": 60,
	"90d": 90,
}

type serverAnalyticsPoint struct {
	TimestampUnix int64   `json:"timestampUnix"`
	Timestamp     string  `json:"timestamp"`
	Min           int     `json:"min"`
	Value         float64 `json:"value"`
	Max           int     `json:"max"`
}

type serverAnalyticsWeekday struct {
	Weekday string  `json:"weekday"`
	Min     float64 `json:"min"`
	Value   float64 `json:"value"`
	Max     float64 `json:"max"`
}

type serverAnalyticsResponse struct {
	ServerBmID  string                   `json:"serverBmId"`
	ServerName  string                   `json:"serverName"`
	Timeframe   string                   `json:"timeframe"`
	Start       string                   `json:"start"`
	Stop        string                   `json:"stop"`
	Points      []serverAnalyticsPoint   `json:"points"`
	Weekdays    []serverAnalyticsWeekday `json:"weekdays"`
	CachedAt    string                   `json:"cachedAt"`
	CacheWindow string                   `json:"cacheWindow"`
}

func (h *Handler) GetServerPlayerCountAnalytics(w http.ResponseWriter, r *http.Request) {
	bmID := strings.TrimSpace(chi.URLParam(r, "bmID"))
	if !bmIDPattern.MatchString(bmID) {
		writeError(w, http.StatusBadRequest, "invalid BattleMetrics server ID")
		return
	}

	timeframe := strings.TrimSpace(r.URL.Query().Get("timeframe"))
	days, ok := serverAnalyticsTimeframes[timeframe]
	if !ok {
		writeError(w, http.StatusBadRequest, "timeframe must be one of 7d, 14d, 30d, 60d, 90d")
		return
	}

	server, err := h.postgres.GetTrackedServer(r.Context(), bmID)
	if errors.Is(err, store.ErrTrackedServerNotFound) {
		writeError(w, http.StatusNotFound, "tracked server not found")
		return
	}
	if err != nil {
		log.Printf("[backend] GetTrackedServer error for server %s: %v", bmID, err)
		writeError(w, http.StatusInternalServerError, "failed to load tracked server")
		return
	}

	now := time.Now().UTC()
	cacheStart, cacheStop := serverAnalyticsRange(now, serverAnalyticsCacheTimeframe)
	selectedStart, selectedStop := serverAnalyticsRange(now, days)

	entry, err := h.getServerAnalyticsCache(r, bmID, cacheStart, cacheStop)
	if errors.Is(err, battlemetrics.ErrPlayerNotFound) {
		writeError(w, http.StatusNotFound, "BattleMetrics server not found")
		return
	}
	if err != nil {
		log.Printf("[backend] server analytics fetch failed for server %s: %v", bmID, err)
		writeError(w, http.StatusBadGateway, "failed to fetch server analytics")
		return
	}

	allPoints := convertServerAnalyticsPoints(entry.Points)
	selected := filterServerAnalyticsPoints(allPoints, selectedStart, selectedStop)

	writeJSON(w, http.StatusOK, serverAnalyticsResponse{
		ServerBmID:  server.BmID,
		ServerName:  server.Name,
		Timeframe:   timeframe,
		Start:       selectedStart.Format(time.RFC3339),
		Stop:        selectedStop.Format(time.RFC3339),
		Points:      selected,
		Weekdays:    computeServerAnalyticsWeekdays(allPoints),
		CachedAt:    entry.FetchedAt.UTC().Format(time.RFC3339),
		CacheWindow: serverAnalyticsCacheWindow(allPoints),
	})
}

func (h *Handler) getServerAnalyticsCache(r *http.Request, bmID string, cacheStart time.Time, cacheStop time.Time) (*store.ServerPlayerCountHistoryCacheEntry, error) {
	now := time.Now().UTC()
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	entry, err := h.postgres.GetServerPlayerCountHistory(r.Context(), bmID)
	if err != nil {
		log.Printf("[backend] server analytics cache lookup failed for server %s: %v", bmID, err)
	}
	if entry != nil && !entry.FetchedAt.UTC().Before(dayStart) {
		return entry, nil
	}

	points, err := h.battleMetricsClient.FetchServerPlayerCountHistory(r.Context(), bmID, cacheStart, cacheStop)
	if err != nil {
		if entry != nil {
			return entry, nil
		}
		return nil, err
	}
	converted := make([]store.ServerPlayerCountPoint, 0, len(points))
	for _, point := range points {
		converted = append(converted, store.ServerPlayerCountPoint{
			TimestampUnix: point.Timestamp.UTC().Unix(),
			Min:           point.Min,
			Value:         point.Value,
			Max:           point.Max,
		})
	}
	if err := h.postgres.UpsertServerPlayerCountHistory(r.Context(), bmID, converted); err != nil {
		log.Printf("[backend] server analytics cache write failed for server %s: %v", bmID, err)
	}
	entry, err = h.postgres.GetServerPlayerCountHistory(r.Context(), bmID)
	if err != nil {
		log.Printf("[backend] server analytics cache reload failed for server %s: %v", bmID, err)
		return &store.ServerPlayerCountHistoryCacheEntry{ServerBmID: bmID, Points: converted, FetchedAt: now}, nil
	}
	if entry == nil {
		return &store.ServerPlayerCountHistoryCacheEntry{ServerBmID: bmID, Points: converted, FetchedAt: now}, nil
	}
	return entry, nil
}

func serverAnalyticsRange(now time.Time, days int) (time.Time, time.Time) {
	today := now.UTC()
	stop := time.Date(today.Year(), today.Month(), today.Day(), 23, 59, 59, 0, time.UTC)
	startDay := stop.AddDate(0, 0, -days)
	start := time.Date(startDay.Year(), startDay.Month(), startDay.Day(), 0, 0, 0, 0, time.UTC)
	return start, stop
}

func convertServerAnalyticsPoints(points []store.ServerPlayerCountPoint) []serverAnalyticsPoint {
	out := make([]serverAnalyticsPoint, 0, len(points))
	for _, point := range points {
		timestamp := time.Unix(point.TimestampUnix, 0).UTC()
		if timestamp.IsZero() {
			continue
		}
		out = append(out, serverAnalyticsPoint{
			TimestampUnix: point.TimestampUnix,
			Timestamp:     timestamp.Format(time.RFC3339),
			Min:           point.Min,
			Value:         roundTo2(point.Value),
			Max:           point.Max,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TimestampUnix < out[j].TimestampUnix })
	return out
}

func filterServerAnalyticsPoints(points []serverAnalyticsPoint, start time.Time, stop time.Time) []serverAnalyticsPoint {
	out := make([]serverAnalyticsPoint, 0, len(points))
	startUnix := start.Unix()
	stopUnix := stop.Unix()
	for _, point := range points {
		if point.TimestampUnix < startUnix || point.TimestampUnix > stopUnix {
			continue
		}
		out = append(out, point)
	}
	return out
}

func computeServerAnalyticsWeekdays(points []serverAnalyticsPoint) []serverAnalyticsWeekday {
	type bucket struct {
		count int
		min   float64
		value float64
		max   float64
	}
	buckets := [7]bucket{}
	for _, point := range points {
		weekday := mondayZero(time.Unix(point.TimestampUnix, 0).UTC().Weekday())
		buckets[weekday].count++
		buckets[weekday].min += float64(point.Min)
		buckets[weekday].value += point.Value
		buckets[weekday].max += float64(point.Max)
	}

	out := make([]serverAnalyticsWeekday, 0, 7)
	for weekday := 0; weekday < 7; weekday++ {
		item := serverAnalyticsWeekday{Weekday: trackAnalyticsWeekdayNames[weekday]}
		if buckets[weekday].count > 0 {
			count := float64(buckets[weekday].count)
			item.Min = roundTo2(buckets[weekday].min / count)
			item.Value = roundTo2(buckets[weekday].value / count)
			item.Max = roundTo2(buckets[weekday].max / count)
		}
		out = append(out, item)
	}
	return out
}

func serverAnalyticsCacheWindow(points []serverAnalyticsPoint) string {
	if len(points) == 0 {
		return "0d"
	}
	minUnix := points[0].TimestampUnix
	maxUnix := points[0].TimestampUnix
	for _, point := range points[1:] {
		if point.TimestampUnix < minUnix {
			minUnix = point.TimestampUnix
		}
		if point.TimestampUnix > maxUnix {
			maxUnix = point.TimestampUnix
		}
	}
	days := int(time.Unix(maxUnix, 0).UTC().Sub(time.Unix(minUnix, 0).UTC()).Hours()/24) + 1
	if days < 1 {
		days = 1
	}
	return strconv.Itoa(days) + "d"
}
