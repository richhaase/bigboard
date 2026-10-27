package stats

import (
	"sort"
	"time"

	"github.com/richhaase/bigboard/git"
)

type MonthActivity struct {
	Month              time.Time
	Commits            int
	Added              int
	Removed            int
	AI                 int
	UnknownLineCommits int
}

type DayActivity struct {
	TotalChange  int
	LinesUnknown bool
}

func ActivityByMonth(records []git.CommitRecord, location *time.Location) []MonthActivity {
	byMonth := make(map[string]*MonthActivity)
	for _, record := range records {
		date := record.Date.In(location)
		key := date.Format("2006-01")
		month := byMonth[key]
		if month == nil {
			year, number, _ := date.Date()
			month = &MonthActivity{Month: time.Date(year, number, 1, 0, 0, 0, 0, time.UTC)}
			byMonth[key] = month
		}
		month.Commits++
		if record.LinesUnknown {
			month.UnknownLineCommits++
		} else {
			month.Added += record.Added
			month.Removed += record.Removed
		}
		if record.AIAssisted {
			month.AI++
		}
	}
	months := make([]MonthActivity, 0, len(byMonth))
	for _, month := range byMonth {
		months = append(months, *month)
	}
	sort.Slice(months, func(i, j int) bool {
		return months[i].Month.Before(months[j].Month)
	})
	return fillMonthGaps(months)
}

func fillMonthGaps(months []MonthActivity) []MonthActivity {
	if len(months) < 2 {
		return months
	}
	filled := make([]MonthActivity, 0, len(months))
	filled = append(filled, months[0])
	for _, month := range months[1:] {
		previous := filled[len(filled)-1].Month
		for current := previous.AddDate(0, 1, 0); current.Before(month.Month); current = current.AddDate(0, 1, 0) {
			filled = append(filled, MonthActivity{Month: current})
		}
		filled = append(filled, month)
	}
	return filled
}

func ActivityByDay(records []git.CommitRecord, location *time.Location) (map[string]DayActivity, int) {
	days := make(map[string]DayActivity)
	maxTotal := 0
	for _, record := range records {
		key := record.Date.In(location).Format("2006-01-02")
		day := days[key]
		if record.LinesUnknown {
			day.LinesUnknown = true
		} else {
			day.TotalChange += record.Added + record.Removed
		}
		days[key] = day
		maxTotal = max(maxTotal, day.TotalChange)
	}
	return days, maxTotal
}
