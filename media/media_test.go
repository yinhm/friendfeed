package media

import (
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

var (
	// mux is the HTTP request multiplexer used with the test server.
	mux *http.ServeMux

	// client is the GitHub client being tested.
	client *Client

	// server is a test HTTP server used to provide mock API responses.
	server *httptest.Server

	mcFile string

	config *Config
)

// setup sets up a test HTTP server along with a ff.Client that is
// configured to talk to that test server.  Tests should register handlers on
// mux which provide mock responses for the API method being tested.
func setup() {
	// test server
	mux = http.NewServeMux()
	server = httptest.NewServer(mux)

	// ff client configured to use test server
	client = NewClient()
	url, _ := url.Parse(server.URL)
	client.BaseURL = url

	mcFile = "../conf/media.json"

	var err error
	config, err = NewConfigFromJSON(mcFile)
	if err != nil {
		log.Fatal(err)
	}
}

// teardown closes the test HTTP server.
func teardown() {
	server.Close()
}

func TestMediaFromUrl(t *testing.T) {
	setup()
	defer teardown()

	rawdata :=
		`{
    "data": {
        "width": 380,
        "height": 430,
        "link": "https://s3.amazonaws.com/gophergala/original/CUqU4If",
        "mime": "image/jpeg",
        "name": "",
        "size": 190,
        "thumbs": {
            "profile":"https://s3.amazonaws.com/gophergala/t/CUqU4If/profile",
            "small": "https://s3.amazonaws.com/gophergala/t/CUqU4If/small"
        }
    },
    "status": 200,
    "success": true
}
`
	mux.HandleFunc("/url", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Content-Lenght", "190")
		fmt.Fprint(w, rawdata)
	})

	Convey("Fetch media from url", t, func() {
		url := "https://www.google.com/images/srpr/logo11w.png"
		resp, err := client.PostUrl(url)
		if err != nil {
			t.Fatal(err)
		}
		So(resp.Success, ShouldEqual, true)
		So(resp.Data.Height, ShouldEqual, 430)
	})
}
