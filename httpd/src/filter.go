package server

import (
	"errors"
	"time"

	"github.com/flosch/pongo2"
	"github.com/microcosm-cc/bluemonday"
	"github.com/yinhm/friendfeed/util"
)

var htmlSanitizer *bluemonday.Policy

func init() {
	pongo2.RegisterFilter("timesince", timeSince)

	htmlSanitizer = bluemonday.StripTagsPolicy()
	htmlSanitizer.AllowElements("h1", "h2", "h3", "h4")

	// The following are all inline phrasing elements
	htmlSanitizer.AllowElements("cite", "code", "em", "mark", "strong")
	// block and inline elements that impart no semantic meaning but style the
	// document
	htmlSanitizer.AllowElements("b", "i", "pre", "small", "strike")

	// "br" "p" "span" are permitted and take no attributes
	htmlSanitizer.AllowElements("br", "p", "span")
}

func timeSince(in *pongo2.Value, param *pongo2.Value) (*pongo2.Value, *pongo2.Error) {
	errMsg := &pongo2.Error{
		Sender:    "filter:timeuntil/timesince",
		OrigError: errors.New("time-value is not a time.Time string"),
	}

	dateStr, ok := in.Interface().(string)
	if !ok {
		return nil, errMsg
	}

	basetime, err := time.Parse(time.RFC3339, dateStr)
	if err != nil {
		return nil, errMsg
	}

	return pongo2.AsValue(util.FormatTime(basetime)), nil
}
