package stats_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/richhaase/bigboard/git"
	"github.com/richhaase/bigboard/stats"
)

func TestActivityByMonthUsesExplicitCalendarAndKeepsInputMultiplicity(t *testing.T) {
	zone, err := time.LoadLocation("America/Denver")
	if err != nil {
		t.Fatal(err)
	}
	boundary := time.Date(2026, time.February, 1, 1, 0, 0, 0, time.UTC)
	known := git.CommitRecord{CommitID: "same", Date: boundary, Added: 4, Removed: 2, AIAssisted: true}
	unknown := git.CommitRecord{Date: boundary, Added: 1000, Removed: 1000, LinesUnknown: true, AIAssisted: true}
	future := git.CommitRecord{Date: boundary.AddDate(0, 14, 0), Added: 9}
	records := []git.CommitRecord{future, known, unknown, known}
	months := stats.ActivityByMonth(records, zone)
	if len(months) != 15 {
		t.Fatalf("projection cropped future or old months: %#v", months)
	}
	wantFirst := stats.MonthActivity{Month: time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), Commits: 3, Added: 8, Removed: 4, AI: 3, UnknownLineCommits: 1}
	if months[0] != wantFirst {
		t.Fatalf("calendar, multiplicity or unknown counts changed: got %+v, want %+v", months[0], wantFirst)
	}
	for index, month := range months[1:14] {
		want := stats.MonthActivity{Month: wantFirst.Month.AddDate(0, index+1, 0)}
		if month != want {
			t.Fatalf("gap %d is not an empty calendar month: got %+v, want %+v", index, month, want)
		}
	}
	if got, want := months[14], (stats.MonthActivity{Month: time.Date(2027, time.March, 1, 0, 0, 0, 0, time.UTC), Commits: 1, Added: 9}); got != want {
		t.Fatalf("future month changed: got %+v, want %+v", got, want)
	}
	if got := stats.ActivityByMonth([]git.CommitRecord{known}, time.UTC); len(got) != 1 || got[0].Month.Month() != time.February {
		t.Fatalf("projection ignored the supplied location: %#v", got)
	}
	if !reflect.DeepEqual(records, []git.CommitRecord{future, known, unknown, known}) {
		t.Fatal("projection mutated the input")
	}
}

func TestActivityByMonthFillsMidnightDSTGapWithUTCMarkers(t *testing.T) {
	zone, err := time.LoadLocation("America/Asuncion")
	if err != nil {
		t.Fatal(err)
	}
	records := []git.CommitRecord{
		{Date: time.Date(2023, time.December, 15, 12, 0, 0, 0, time.UTC), Added: 3},
		{Date: time.Date(2023, time.September, 15, 12, 0, 0, 0, time.UTC), Added: 2},
	}
	months := stats.ActivityByMonth(records, zone)
	want := []stats.MonthActivity{
		{Month: time.Date(2023, time.September, 1, 0, 0, 0, 0, time.UTC), Commits: 1, Added: 2},
		{Month: time.Date(2023, time.October, 1, 0, 0, 0, 0, time.UTC)},
		{Month: time.Date(2023, time.November, 1, 0, 0, 0, 0, time.UTC)},
		{Month: time.Date(2023, time.December, 1, 0, 0, 0, 0, time.UTC), Commits: 1, Added: 3},
	}
	if !reflect.DeepEqual(months, want) {
		t.Fatalf("midnight DST gap changed month markers: got %+v, want %+v", months, want)
	}
}

func TestActivityByDayKeepsKnownLinesUnknownFlagsAndFullRangeMaximum(t *testing.T) {
	zone := time.FixedZone("west", -7*60*60)
	boundary := time.Date(2026, time.February, 1, 1, 0, 0, 0, time.UTC)
	known := git.CommitRecord{CommitID: "same", Date: boundary, Added: 4, Removed: 2}
	records := []git.CommitRecord{
		known, known,
		{Date: boundary, Added: 9999, LinesUnknown: true},
		{Date: boundary.AddDate(0, -16, 0), Added: 20},
		{Date: boundary.AddDate(0, 0, 1), Added: 100},
	}
	days, maximum := stats.ActivityByDay(records, zone)
	want := map[string]stats.DayActivity{
		"2026-01-31": {TotalChange: 12, LinesUnknown: true},
		"2024-09-30": {TotalChange: 20},
		"2026-02-01": {TotalChange: 100},
	}
	if !reflect.DeepEqual(days, want) || maximum != 100 {
		t.Fatalf("daily evidence or scale changed: days=%+v maximum=%d", days, maximum)
	}
	days, maximum = stats.ActivityByDay(records[:4], zone)
	if maximum != 20 || days["2026-01-31"].TotalChange != 12 {
		t.Fatalf("out-of-window activity stopped contributing to scale: days=%+v maximum=%d", days, maximum)
	}
	utcDays, _ := stats.ActivityByDay([]git.CommitRecord{known}, time.UTC)
	if got := utcDays["2026-02-01"].TotalChange; got != 6 {
		t.Fatalf("projection ignored the supplied location: %+v", utcDays)
	}
}

func TestCalendarProjectionsPreserveEmptyAndZeroValues(t *testing.T) {
	if got := stats.ActivityByMonth(nil, time.UTC); len(got) != 0 {
		t.Fatalf("empty monthly projection: %#v", got)
	}
	if days, maximum := stats.ActivityByDay(nil, time.UTC); len(days) != 0 || maximum != 0 {
		t.Fatalf("empty daily projection: days=%+v maximum=%d", days, maximum)
	}
	records := []git.CommitRecord{{}}
	if got, want := stats.ActivityByMonth(records, time.UTC), ([]stats.MonthActivity{{Month: time.Time{}, Commits: 1}}); !reflect.DeepEqual(got, want) {
		t.Fatalf("zero monthly projection: got %+v, want %+v", got, want)
	}
	if days, maximum := stats.ActivityByDay(records, time.UTC); !reflect.DeepEqual(days, map[string]stats.DayActivity{"0001-01-01": {}}) || maximum != 0 {
		t.Fatalf("zero daily projection: days=%+v maximum=%d", days, maximum)
	}
	if days, maximum := stats.ActivityByDay([]git.CommitRecord{{LinesUnknown: true}}, time.UTC); !days["0001-01-01"].LinesUnknown || maximum != 0 {
		t.Fatalf("unknown-only daily projection: days=%+v maximum=%d", days, maximum)
	}
}
