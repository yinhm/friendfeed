package server

import (
	"net/http"
	"strconv"
)

func ParseStart(req *http.Request) int {
	query := req.URL.Query()
	startS := query.Get("start")
	if startS == "" {
		startS = "0"
	}
	start, _ := strconv.Atoi(startS)
	if start > 20000 {
		start = 20000
	}
	return start
}
