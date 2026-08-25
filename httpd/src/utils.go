package server

import (
	"errors"
	"net/http"
	"strconv"
)

func ParseStart(req *http.Request) int {
	query := req.URL.Query()
	startS := query.Get("start")
	if startS == "" {
		startS = "0"
	}
	start, err := strconv.ParseInt(startS, 10, 32)
	if err != nil && !errors.Is(err, strconv.ErrRange) {
		// Syntax errors start from the first page; range errors keep the
		// 32-bit limit value and are capped by the ceiling below.
		start = 0
	}
	if start < 0 {
		start = 0
	}
	if start > 20000 {
		start = 20000
	}
	return int(start)
}
