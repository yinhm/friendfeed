package server

import (
	"fmt"
	"time"

	"github.com/flosch/pongo2"
)

// Seconds-based time units
const (
	Minute   = 60
	Hour     = 60 * Minute
	Day      = 24 * Hour
	Week     = 7 * Day
	Month    = 30 * Day
	Year     = 12 * Month
	LongTime = 37 * Year

	layoutDayMonth     = "Jan 2"
	layoutDayMonthYear = "Jan 2, 2006"
)

func init() {
	pongo2.RegisterFilter("timesince", timeSince)
}

func timeSince(in *pongo2.Value, param *pongo2.Value) (*pongo2.Value, *pongo2.Error) {
	errMsg := &pongo2.Error{
		Sender:   "filter:timeuntil/timesince",
		ErrorMsg: "time-value is not a time.Time string.",
	}

	dateStr, ok := in.Interface().(string)
	if !ok {
		return nil, errMsg
	}

	basetime, err := time.Parse(time.RFC3339, dateStr)
	if err != nil {
		return nil, errMsg
	}

	return pongo2.AsValue(fmtTime(basetime)), nil
}

func fmtTime(t time.Time) string {
	delta := t.Sub(time.Now())
	diff := int(delta.Seconds())

	lbl := "ago"
	diff *= -1

	switch {
	case diff <= 0:
		return "now"
	case diff <= 2:
		return fmt.Sprintf("1 second %s", lbl)
	case diff < 1*Minute:
		return fmt.Sprintf("%d seconds %s", diff, lbl)

	case diff < 2*Minute:
		return fmt.Sprintf("1 minute %s", lbl)
	case diff < 1*Hour:
		return fmt.Sprintf("%d minutes %s", diff/Minute, lbl)

	case diff < 2*Hour:
		return fmt.Sprintf("1 hour %s", lbl)
	case diff < 1*Day:
		return fmt.Sprintf("%d hours %s", diff/Hour, lbl)

	case diff < 2*Day:
		return fmt.Sprintf("1 day %s", lbl)
	case diff < 1*Week:
		return fmt.Sprintf("%s", t.Weekday())

	case diff < 2*Week:
		return fmt.Sprintf("1 week %s", lbl)
	case diff < 1*Month:
		return fmt.Sprintf("%d weeks %s", diff/Week, lbl)

	case diff < 2*Month:
		return fmt.Sprintf("1 month %s", lbl)
	case diff < 1*Year:
		return t.Format(layoutDayMonth)

	default:
		return t.Format(layoutDayMonthYear)
	}
}
