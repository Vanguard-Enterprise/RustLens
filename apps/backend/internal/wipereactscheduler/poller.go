package wipereactscheduler

import (
	"context"
	"log"
	"time"

	"rustlens/backend/internal/store"
)

const schedulerInterval = time.Minute

type Poller struct {
	store *store.PostgresStore
}

func NewPoller(store *store.PostgresStore) *Poller {
	return &Poller{store: store}
}

func (p *Poller) Start(ctx context.Context) {
	ticker := time.NewTicker(schedulerInterval)
	go func() {
		defer ticker.Stop()
		for {
			if err := p.tick(ctx); err != nil {
				log.Printf("[backend] wipe react scheduler tick error: %v", err)
			}

			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func (p *Poller) tick(ctx context.Context) error {
	start := time.Now()
	schedules, err := p.store.ListAllWipeReactSchedules(ctx)
	if err != nil {
		runErrorCounter.Add(ctx, 1)
		return err
	}

	nowUnix := time.Now().Unix()
	for _, schedule := range schedules {
		next := computeNextPostTime(schedule, nowUnix)
		if next == nil {
			continue
		}
		if nowUnix < next.postUnix {
			continue
		}
		if schedule.LastPostedForUnix >= next.wipeUnix {
			continue
		}

		if err := p.store.EnqueueWipeReactSchedulePost(
			ctx,
			schedule.ID,
			schedule.GuildID,
			schedule.ChannelID,
			next.wipeUnix,
			next.postUnix,
		); err != nil {
			log.Printf("[backend] failed to enqueue wipe react schedule post guild=%s schedule=%d: %v", schedule.GuildID, schedule.ID, err)
		}
	}

	runDurationHist.Record(ctx, time.Since(start).Seconds())
	return nil
}

type nextPostTime struct {
	wipeUnix int64
	postUnix int64
}

var postBeforeSeconds = map[string]int64{
	"1d": 86400,
	"2d": 2 * 86400,
	"3d": 3 * 86400,
	"4d": 4 * 86400,
	"1w": 7 * 86400,
	"2w": 14 * 86400,
}

var dayNameToWeekday = map[string]time.Weekday{
	"sunday":    time.Sunday,
	"monday":    time.Monday,
	"tuesday":   time.Tuesday,
	"wednesday": time.Wednesday,
	"thursday":  time.Thursday,
	"friday":    time.Friday,
	"saturday":  time.Saturday,
}

func computeNextPostTime(schedule store.WipeReactSchedule, nowUnix int64) *nextPostTime {
	postBefore, ok := postBeforeSeconds[schedule.PostBefore]
	if !ok {
		return nil
	}

	afterUnix := schedule.LastPostedForUnix
	if afterUnix <= 0 {
		afterUnix = nowUnix - postBefore - 1
	}

	var earliestWipeUnix int64
	for _, dayName := range schedule.Days {
		nextOccurrence := computeNextWeekdayOccurrence(dayName, schedule.Time, schedule.TimeZone, schedule.Repeat, afterUnix, nowUnix)
		if nextOccurrence == 0 {
			continue
		}
		if earliestWipeUnix == 0 || nextOccurrence < earliestWipeUnix {
			earliestWipeUnix = nextOccurrence
		}
	}

	if schedule.IncludesForceWipe {
		nextForceWipe := computeNextForceWipeOccurrence(afterUnix, nowUnix)
		if nextForceWipe != 0 && (earliestWipeUnix == 0 || nextForceWipe < earliestWipeUnix) {
			earliestWipeUnix = nextForceWipe
		}
	}

	if earliestWipeUnix == 0 {
		return nil
	}

	return &nextPostTime{
		wipeUnix: earliestWipeUnix,
		postUnix: earliestWipeUnix - postBefore,
	}
}

func computeNextWeekdayOccurrence(dayName string, timeText string, timeZone string, repeat string, afterUnix int64, nowUnix int64) int64 {
	targetDay, ok := dayNameToWeekday[dayName]
	if !ok {
		return 0
	}
	loc, err := time.LoadLocation(timeZone)
	if err != nil {
		return 0
	}
	parsedTime, err := time.ParseInLocation("15:04", timeText, loc)
	if err != nil {
		return 0
	}

	after := time.Unix(afterUnix, 0).In(loc)
	startDay := time.Date(after.Year(), after.Month(), after.Day(), 0, 0, 0, 0, loc)
	minGapSeconds := int64(0)
	if repeat == "biweekly" {
		minGapSeconds = 13 * 86400
	}

	for offset := 0; offset <= 62; offset++ {
		candidateDay := startDay.AddDate(0, 0, offset)
		if candidateDay.Weekday() != targetDay {
			continue
		}
		if repeat == "monthly" && candidateDay.Day() > 7 {
			continue
		}

		candidate := time.Date(
			candidateDay.Year(),
			candidateDay.Month(),
			candidateDay.Day(),
			parsedTime.Hour(),
			parsedTime.Minute(),
			0,
			0,
			loc,
		)
		candidateUnix := candidate.Unix()
		if candidateUnix <= afterUnix {
			continue
		}
		// The wipe itself must still be in the future. Otherwise a freshly
		// created schedule (where afterUnix is backdated by postBefore to allow
		// lead time) could enqueue a reminder for a wipe that already happened.
		if candidateUnix <= nowUnix {
			continue
		}
		if minGapSeconds > 0 && candidateUnix-afterUnix < minGapSeconds {
			continue
		}

		return candidateUnix
	}

	return 0
}

func computeNextForceWipeOccurrence(afterUnix int64, nowUnix int64) int64 {
	loc, err := time.LoadLocation("Europe/London")
	if err != nil {
		return 0
	}
	after := time.Unix(afterUnix, 0).In(loc)
	year, month := after.Year(), after.Month()

	for monthOffset := 0; monthOffset < 4; monthOffset++ {
		candidateMonth := month + time.Month(monthOffset)
		candidateYear := year
		for candidateMonth > 12 {
			candidateMonth -= 12
			candidateYear += 1
		}

		for day := 1; day <= 7; day++ {
			candidate := time.Date(candidateYear, candidateMonth, day, 19, 0, 0, 0, loc)
			if candidate.Weekday() != time.Thursday {
				continue
			}
			// The force-wipe time must be both after the dedup floor and still
			// in the future, so a fresh schedule never enqueues a post for a
			// force wipe that already passed.
			if candidate.Unix() > afterUnix && candidate.Unix() > nowUnix {
				return candidate.Unix()
			}
			break
		}
	}

	return 0
}
