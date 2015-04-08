package server

import (
	"net/http"
	"net/url"
)

func CheckRedirect(reqUrl string) (bool, string, int) {
	client := http.Transport{}
	req, _ := http.NewRequest("HEAD", reqUrl, nil)
	r, err := client.RoundTrip(req)
	if err != nil {
		return false, "", 502
	}
	lv := &url.URL{}
	lv, err = r.Location()
	if err == nil {
		reqUrl = lv.String()
	}

	if r.StatusCode < 300 {
		return false, reqUrl, 200
	} else if r.StatusCode >= 300 && r.StatusCode < 400 {
		return true, reqUrl, r.StatusCode
	} else {
		return false, "", 502
	}
}
