package media

import (
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
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

func TestDownloadAttachment(t *testing.T) {
	Convey("Fetch media from url", t, func() {
		client := NewGoogleStorage(config)
		url := "http://m.friendfeed-media.com/07a1ee699cef1999e03bcbaaec661ef77ac8852d"

		obj := &Object{
			Filename: "ff.png",
			Path:     "07a1ee699cef1999e03bcbaaec661ef77ac8852d",
			Url:      url,
			MimeType: "image/png",
		}

		resp, err := client.fetch(obj)
		So(err, ShouldBeNil)

		mimetype := resp.Header.Get("Content-Type")
		So(mimetype, ShouldEqual, "image/png")

		actual := resp.Header.Get("Content-Length")
		So(actual, ShouldEqual, "64313")

		So(len(obj.Content), ShouldEqual, 64313)

		newObj, err := client.Post(obj)
		So(err, ShouldBeNil)

		expect2 := config.Bucket + "/07a1ee699cef1999e03bcbaaec661ef77ac8852d"
		So(newObj.Path, ShouldEqual, expect2)
		expect3 := "https://storage.googleapis.com/" + expect2
		So(newObj.Url, ShouldEqual, expect3)

		filename := "07a1ee699cef1999e03bcbaaec661ef77ac8852d"
		ok, err := client.Exists(filename)
		So(err, ShouldBeNil)
		So(ok, ShouldBeTrue)
	})
}

func TestMirror(t *testing.T) {
	Convey("Fetch media from url", t, func() {
		client := NewGoogleStorage(config)
		url := "http://m.friendfeed-media.com/07a1ee699cef1999e03bcbaaec661ef77ac8852d"

		obj := &Object{
			Filename: "ff.png",
			Path:     "07a1ee699cef1999e03bcbaaec661ef77ac8852d",
			Url:      url,
			MimeType: "image/png",
		}
		oldObj, err := client.Post(obj)
		So(err, ShouldBeNil)

		newObj, err := client.Mirror(obj)
		So(err, ShouldBeNil)

		So(newObj.Path, ShouldEqual, oldObj.Path)
		So(newObj.Url, ShouldEqual, oldObj.Url)
	})
}

func TestUrlParseMirror(t *testing.T) {
	Convey("Fetch media from url", t, func() {
		client := NewGoogleStorage(config)

		fromUrl := "http://m.friendfeed-media.com/07a1ee699cef1999e03bcbaaec661ef77ac8852d"
		parsed, err := url.Parse(fromUrl)
		newpath := strings.TrimLeft(parsed.Path, "/")

		So(err, ShouldBeNil)
		So(parsed.Host, ShouldEqual, "m.friendfeed-media.com")
		So(newpath, ShouldEqual, "07a1ee699cef1999e03bcbaaec661ef77ac8852d")

		obj := &Object{
			Filename: newpath,
			Path:     newpath,
			Url:      fromUrl,
		}

		newObj, err := client.Mirror(obj)
		So(err, ShouldBeNil)

		expect := "07a1ee699cef1999e03bcbaaec661ef77ac8852d"
		So(newObj.Path, ShouldEqual, expect)
		expect = "https://storage.googleapis.com/lastff01/07a1ee699cef1999e03bcbaaec661ef77ac8852d"
		So(newObj.Url, ShouldEqual, expect)
	})
}
