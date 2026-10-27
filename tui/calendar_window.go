package tui

import (
	"time"

	"github.com/richhaase/bigboard/git"
	"github.com/richhaase/bigboard/stats"
)

type activityChartWindow struct {
	now      time.Time
	duration time.Duration
}

func (w activityChartWindow) shortRange() bool {
	return w.duration > 0 && w.duration <= 180*24*time.Hour
}

func (w activityChartWindow) timelineLabel() string {
	if !w.shortRange() {
		return "monthly"
	}
	if w.duration <= 30*24*time.Hour {
		return "daily"
	}
	return "weekly"
}

func (w activityChartWindow) timelineDateFormat() string {
	switch w.timelineLabel() {
	case "daily":
		return "Jan 02 2006"
	case "weekly":
		return "Week of Jan 02 2006"
	default:
		return "Jan 2006"
	}
}

func (w activityChartWindow) timeline(records []git.CommitRecord) []stats.TimelineActivity {
	if w.shortRange() {
		interval := stats.TimelineDaily
		if w.timelineLabel() == "weekly" {
			interval = stats.TimelineWeekly
		}
		return stats.ActivityTimeline(records, time.Local, w.now.Add(-w.duration), w.now, interval)
	}
	months := stats.ActivityByMonth(records, time.Local)
	if len(months) > 12 {
		months = months[len(months)-12:]
	}
	buckets := make([]stats.TimelineActivity, len(months))
	for i, month := range months {
		buckets[i] = stats.TimelineActivity{
			Start: month.Month, Commits: month.Commits, Added: month.Added,
			Removed: month.Removed, AI: month.AI, UnknownLineCommits: month.UnknownLineCommits,
		}
	}
	return buckets
}

func chartCalendarDay(at time.Time) time.Time {
	year, month, day := at.In(time.Local).Date()
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

func (w activityChartWindow) heatmapDates(width int) (startSunday, firstDay, lastDay time.Time, weeks int) {
	lastDay = chartCalendarDay(w.now)
	lastSunday := lastDay.AddDate(0, 0, -int(lastDay.Weekday()))
	weeks = min(53, max(12, width-chromeInset-6))
	startSunday = lastSunday.AddDate(0, 0, -7*(weeks-1))
	firstDay = startSunday
	if w.shortRange() {
		firstDay = chartCalendarDay(w.now.Add(-w.duration))
		startSunday = firstDay.AddDate(0, 0, -int(firstDay.Weekday()))
		weeks = int(lastSunday.Sub(startSunday)/(7*24*time.Hour)) + 1
		capacity := max(1, width-chromeInset-6)
		if weeks > capacity {
			weeks = capacity
			startSunday = lastSunday.AddDate(0, 0, -7*(weeks-1))
			firstDay = startSunday
		}
	}
	return
}
