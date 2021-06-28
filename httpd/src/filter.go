package server

import (
	"errors"
	"time"

	"github.com/flosch/pongo2"
	"github.com/yinhm/friendfeed/util"
)

func init() {
	pongo2.RegisterFilter("timesince", timeSince)
}

func timeSince(in *pongo2.Value, param *pongo2.Value) (*pongo2.Value, *pongo2.Error) {
	errMsg := &pongo2.Error{
		Sender:    "filter:timeuntil/timesince",
		OrigError: errors.New("time-value is not a time.Time string."),
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
