package ff

import (
	"fmt"
	"io/ioutil"
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
)

// setup sets up a test HTTP server along with a ff.Client that is
// configured to talk to that test server.  Tests should register handlers on
// mux which provide mock responses for the API method being tested.
func setup() {
	// test server
	mux = http.NewServeMux()
	server = httptest.NewServer(mux)

	// ff client configured to use test server
	client = NewClient(nil, "user", "pwd")
	url, _ := url.Parse(server.URL + "/v2")
	client.BaseURL = url
}

// teardown closes the test HTTP server.
func teardown() {
	server.Close()
}

func TestFeed(t *testing.T) {
	setup()
	defer teardown()

	filename := "testdata/feed.json"
	bytes, err := ioutil.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}

	mux.HandleFunc("/v2/feed/yinhm", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, string(bytes))
	})

	Convey("Fetch archive user feed", t, func() {
		opt := new(FeedOptions)
		opt.RawBody = 1
		feed, _, err := client.Feed("yinhm", opt)
		if err != nil {
			t.Fatal(err)
		}
		So(feed.Id, ShouldEqual, "yinhm")
		So(feed.Name, ShouldEqual, "yinhm")
		So(feed.SupId, ShouldEqual, "4ceb94af")
		So(feed.Type, ShouldEqual, "user")
		So(feed.Description, ShouldEqual, "Golang/Python/Linux")

		So(len(feed.Entries), ShouldEqual, 30)

		entry := feed.Entries[0]
		So(len(entry.RawBody), ShouldEqual, 80)
		So(entry.Id, ShouldEqual, "e/95a0d02fb680418ea1b7fb55baf1ee2d")
	})

	Convey("Fetch page 2", t, func() {
		opt := new(FeedOptions)
		opt.Start = 50
		opt.Num = 100
		opt.RawBody = 1

		_, resp, err := client.Feed("yinhm", opt)
		if err != nil {
			t.Fatal(err)
		}
		So(resp.Request.URL.String(), ShouldEqual, client.BaseURL.String()+"/feed/yinhm?num=100&raw=1&start=50")
	})
}

func TestFeed1(t *testing.T) {
	setup()
	defer teardown()

	filename := "testdata/feed1.json"
	bytes, err := ioutil.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}

	mux.HandleFunc("/v2/feed/yinhm", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, string(bytes))
	})

	Convey("Fetch archive user feed", t, func() {
		opt := new(FeedOptions)
		opt.Start = 0
		opt.Num = 100
		opt.RawBody = 1

		feed, _, err := client.Feed("yinhm", opt)
		if err != nil {
			t.Fatal(err)
		}
		So(feed.Id, ShouldEqual, "yinhm")
		So(feed.Name, ShouldEqual, "yinhm")
		So(feed.SupId, ShouldEqual, "4ceb94af")
		So(feed.Type, ShouldEqual, "user")
		So(feed.Description, ShouldEqual, "Golang/Python/Linux")

		So(len(feed.Commands), ShouldEqual, 2)
		So(len(feed.Entries), ShouldEqual, 100)

		entry := feed.Entries[0]
		So(len(entry.RawBody), ShouldEqual, 80)
		So(entry.Id, ShouldEqual, "e/95a0d02fb680418ea1b7fb55baf1ee2d")
	})
}

func TestFeed5(t *testing.T) {
	setup()
	defer teardown()

	filename := "testdata/feed5.json"
	bytes, err := ioutil.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}

	mux.HandleFunc("/v2/feed/yinhm", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, string(bytes))
	})

	Convey("Fetch archive user feed startfrom 500", t, func() {
		opt := new(FeedOptions)
		opt.Start = 500
		opt.Num = 100
		opt.RawBody = 1

		feed, _, err := client.Feed("yinhm", opt)
		if err != nil {
			t.Fatal(err)
		}
		So(feed.Id, ShouldEqual, "yinhm")
		So(feed.Name, ShouldEqual, "yinhm")
		So(feed.SupId, ShouldEqual, "4ceb94af")
		So(feed.Type, ShouldEqual, "user")
		So(feed.Description, ShouldEqual, "Golang/Python/Linux")

		So(len(feed.Commands), ShouldEqual, 2)
		So(len(feed.Entries), ShouldEqual, 100)

		entry := feed.Entries[0]
		So(len(entry.RawBody), ShouldEqual, 362)
		So(entry.Id, ShouldEqual, "e/2b43a9066074d120ed2e45494eea1797")
	})
}

func TestFeedInfo(t *testing.T) {
	setup()
	defer teardown()

	filename := "testdata/feedinfo.json"
	bytes, err := ioutil.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}

	mux.HandleFunc("/v2/feedinfo/yinhm", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, string(bytes))
	})

	Convey("Fetch user feedinfo", t, func() {
		feedinfo, _, err := client.Feedinfo("yinhm")
		if err != nil {
			t.Fatal(err)
		}

		So(feedinfo.Id, ShouldEqual, "yinhm")
		So(feedinfo.Name, ShouldEqual, "yinhm")
		So(feedinfo.SupId, ShouldEqual, "4ceb94af")
		So(feedinfo.Type, ShouldEqual, "user")
		So(feedinfo.Description, ShouldEqual, "Golang/Python/Linux")

		So(len(feedinfo.Subscribers), ShouldEqual, 141)
		So(len(feedinfo.Subscriptions), ShouldEqual, 140)
		So(len(feedinfo.Services), ShouldEqual, 8)
	})
}

func TestEntry(t *testing.T) {
	setup()
	defer teardown()

	filename := "testdata/entry.json"
	bytes, err := ioutil.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}

	mux.HandleFunc("/v2/entry/e/3e208510463f44af83af444c004c4314", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, string(bytes))
	})

	Convey("Fetch entry", t, func() {
		opt := new(FeedOptions)
		entry, _, err := client.Entry("e/3e208510463f44af83af444c004c4314", opt)
		if err != nil {
			t.Fatal(err)
		}

		So(entry.Id, ShouldEqual, "e/3e208510463f44af83af444c004c4314")
		So(entry.Date, ShouldEqual, "2009-06-25T18:23:38Z")
		So(entry.From.Id, ShouldEqual, "bret")
		So(entry.Via.Name, ShouldEqual, "Bookmarklet")

		So(len(entry.Thumbnails), ShouldEqual, 2)
		So(len(entry.Comments), ShouldEqual, 6)
		So(len(entry.Likes), ShouldEqual, 16)

		So(entry.Url, ShouldEqual, "http://friendfeed.com/bret/3e208510/why-legendary-game-developer-john-carmack")
		So(entry.RawBody, ShouldEqual, "Why legendary game developer John Carmack shelved his ego and sold id to ZeniMax | VentureBeat")
		So(entry.RawLink, ShouldEqual, "http://venturebeat.com/2009/06/25/the-big-game-deal-why-ids-john-carmack-shelved-his-ego-and-sold-out-to-zenimax/")

	})
}

func TestV1Profile(t *testing.T) {
	setup()
	defer teardown()

	filename := "testdata/v1profile.json"
	bytes, err := ioutil.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}

	// fake v2
	mux.HandleFunc("/v2/api/user/yinhm/profile", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, string(bytes))
	})

	Convey("Fetch user feedinfo", t, func() {
		p, _, err := client.V1Profile("yinhm", "user")
		if err != nil {
			t.Fatal(err)
		}

		So(p.Id, ShouldEqual, "c6f8dca8-54f0-11dd-b489-003048343a40")
		So(p.Name, ShouldEqual, "yinhm")
		So(p.Nickname, ShouldEqual, "yinhm")
		So(p.Status, ShouldEqual, "public")
		So(p.ProfileUrl, ShouldEqual, "http://friendfeed.com/yinhm")

		So(p.Rooms[0].Id, ShouldEqual, "413450dc-560b-420b-a901-628ca0da2855")
	})
}

func TestIsMediaServer(t *testing.T) {
	Convey("Given media url, should identify is it from ff media serer", t, func() {
		ok := IsMediaServer("i.friendfeed.com")
		So(ok, ShouldBeTrue)
		ok = IsMediaServer("m.friendfeed-media.com")
		So(ok, ShouldBeTrue)
		ok = IsMediaServer("friendfeed-media.s3.amazonaws.com")
		So(ok, ShouldBeTrue)
		ok = IsMediaServer("twitpic.com")
		So(ok, ShouldBeFalse)
		ok = IsMediaServer("friendfeed.com")
		So(ok, ShouldBeFalse)
	})
}

func TestFeedAutoCollapsing(t *testing.T) {
	setup()
	defer teardown()

	filename := "testdata/feed_auto.json"
	bytes, err := ioutil.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}

	mux.HandleFunc("/v2/feed/bret", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, string(bytes))
	})

	Convey("Fetch archive user feed", t, func() {
		opt := new(FeedOptions)
		opt.Start = 0
		opt.MaxComments = "auto"
		opt.MaxLikes = "auto"

		feed, _, err := client.Feed("bret", opt)
		if err != nil {
			t.Fatal(err)
		}
		So(feed.Id, ShouldEqual, "bret")
		So(len(feed.Entries), ShouldEqual, 30)

		comments := feed.Entries[0].Comments
		So(comments[0].From.Id, ShouldEqual, "bret")
		So(comments[1].Body, ShouldEqual, "11 more comments")
		So(comments[1].Num, ShouldEqual, 11)
		So(comments[1].Placeholder, ShouldEqual, true)
		So(comments[2].From.Id, ShouldEqual, "insomamnes")
	})
}
