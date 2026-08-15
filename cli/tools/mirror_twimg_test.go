package main

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/require"

	"github.com/yinhm/friendfeed/media"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
)

func TestCollectTwimgURLs(t *testing.T) {
	refs := twimgURLRefs{}
	collectTwimgURLs(`<img src="http://pbs.twimg.com/media/AAA.jpg"> <img src='https://pbs.twimg.com/media/BBB.png'>`, "e1", refs)
	collectTwimgURLs(`<img src="http://a0.twimg.com/profile_images/1/avatar.jpg">`, "e1", refs)
	collectTwimgURLs("no media here", "e1", refs)
	collectTwimgURLs(`<a href="http://pbs.twimg.com/media/AAA.jpg">x</a>`, "e2", refs)

	require.Len(t, refs, 2)
	require.Equal(t, []string{"e1", "e2"}, refs["http://pbs.twimg.com/media/AAA.jpg"].entryIDs)
	require.Equal(t, 2, refs["http://pbs.twimg.com/media/AAA.jpg"].count)
	require.Equal(t, []string{"e1"}, refs["https://pbs.twimg.com/media/BBB.png"].entryIDs)
}

func TestRescuableTwimgURL(t *testing.T) {
	require.True(t, rescuableTwimgURL("http://p.twimg.com/ABC.jpg"))
	require.True(t, rescuableTwimgURL("https://pbs.twimg.com/media/ABC.jpg"))
	require.False(t, rescuableTwimgURL("http://a0.twimg.com/profile_images/1/avatar.jpg"))
	require.False(t, rescuableTwimgURL("https://example.com/media/ABC.jpg"))
}

func TestScanTwimgURLs(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	withBody := uuid.Must(uuid.NewV4())
	_, err = model.Entry.Put(db, withBody.Bytes(), &pb.Entry{
		Id:   withBody.String(),
		Body: `<div class="media"><img src="http://pbs.twimg.com/media/AAA.jpg"></div>`,
		Thumbnails: []*pb.Thumbnail{
			{Url: "https://pbs.twimg.com/media/TTT.jpg", Link: "http://example.com/page"},
		},
		Files: []*pb.File{
			{Url: "https://pbs.twimg.com/media/FFF.gif", Name: "f.gif"},
		},
	})
	require.NoError(t, err)

	without := uuid.Must(uuid.NewV4())
	_, err = model.Entry.Put(db, without.Bytes(), &pb.Entry{Id: without.String(), Body: "plain text"})
	require.NoError(t, err)

	repeat := uuid.Must(uuid.NewV4())
	_, err = model.Entry.Put(db, repeat.Bytes(), &pb.Entry{
		Id:   repeat.String(),
		Body: `<img src="http://pbs.twimg.com/media/AAA.jpg">`,
	})
	require.NoError(t, err)

	refs, scanned, err := scanTwimgURLs(db)
	require.NoError(t, err)
	require.Equal(t, 3, scanned)
	require.Len(t, refs, 3)
	require.ElementsMatch(t, []string{withBody.String(), repeat.String()},
		refs["http://pbs.twimg.com/media/AAA.jpg"].entryIDs)
	require.Equal(t, []string{withBody.String()}, refs["https://pbs.twimg.com/media/TTT.jpg"].entryIDs)
	require.Equal(t, []string{withBody.String()}, refs["https://pbs.twimg.com/media/FFF.gif"].entryIDs)
}

func TestLoadTwimgSyncRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sync.jsonl")

	records, err := loadTwimgSyncRecords(path)
	require.NoError(t, err)
	require.Empty(t, records)

	content := `{"version":1,"url":"a","status":"mirrored","new_url":"https://m.friendfeed.me/x","object_key":"x","sha256":"abc","bytes":3}
{"version":1,"url":"b","status":"dead","http_status":404}
{"url":"c","status":"error","error":"timeout"}
`
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))

	records, err = loadTwimgSyncRecords(path)
	require.NoError(t, err)
	require.Len(t, records, 3)
	require.True(t, records["a"].terminal())
	require.True(t, records["b"].terminal())
	require.False(t, records["c"].terminal())
	require.Equal(t, 404, records["b"].HTTPStatus)
}

func TestFetchHTTPStatus(t *testing.T) {
	require.Equal(t, 0, fetchHTTPStatus(nil))
	require.Equal(t, 404, fetchHTTPStatus(errors.New("fetch http://x/y: unexpected status 404 Not Found")))
	require.Equal(t, 403, fetchHTTPStatus(errors.New("fetch http://x/y: unexpected status 403 Forbidden")))
	require.Equal(t, 0, fetchHTTPStatus(errors.New("dial tcp: i/o timeout")))
}

func TestTwimgRetryDelay(t *testing.T) {
	require.Equal(t, 5*time.Second, twimgRetryDelay(5*time.Second, 1, http.StatusInternalServerError))
	require.Equal(t, 10*time.Second, twimgRetryDelay(5*time.Second, 2, http.StatusInternalServerError))
	require.Equal(t, 20*time.Second, twimgRetryDelay(5*time.Second, 3, http.StatusInternalServerError))
	require.Equal(t, time.Minute, twimgRetryDelay(5*time.Second, 1, http.StatusTooManyRequests))
	require.Equal(t, 5*time.Minute, twimgRetryDelay(5*time.Second, 20, http.StatusInternalServerError))
}

// waybackFixture serves the CDX index API and raw captures from one handler.
type waybackFixture struct {
	snapshots map[string]string // original url -> capture timestamp
	images    map[string][]byte // raw capture path -> bytes
	apiHits   *int32
	failFirst bool
}

func (f *waybackFixture) handler(t *testing.T) http.Handler {
	// A plain HandlerFunc, not ServeMux: raw capture URLs embed the original
	// "http://..." with its double slash, and ServeMux would clean-redirect
	// the path and lose it.
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/cdx/search/cdx") {
			if f.apiHits != nil {
				if hits := atomic.AddInt32(f.apiHits, 1); f.failFirst && hits == 1 {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
			}
			rawURL := r.URL.Query().Get("url")
			ts, ok := f.snapshots[rawURL]
			if !ok {
				fmt.Fprint(w, `[]`)
				return
			}
			fmt.Fprintf(w, `[["timestamp"],["%s"]]`, ts)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/web/") {
			for suffix, body := range f.images {
				if r.URL.Path == suffix {
					w.Header().Set("Content-Type", "image/jpeg")
					w.Write(body)
					return
				}
			}
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
}

func TestWaybackSnapshotTimestamp(t *testing.T) {
	live := "http://pbs.twimg.com/media/AAA.jpg"
	fixture := &waybackFixture{snapshots: map[string]string{live: "20120601000000"}}
	srv := httptest.NewServer(fixture.handler(t))
	defer srv.Close()
	wb := &waybackClient{webBase: srv.URL, http: srv.Client(), sleep: func(time.Duration) {}}

	ts, err := wb.snapshotTimestamp(live)
	require.NoError(t, err)
	require.Equal(t, "20120601000000", ts)

	_, err = wb.snapshotTimestamp("http://pbs.twimg.com/media/MISSING.jpg")
	require.ErrorIs(t, err, errNoWaybackSnapshot)
}

func TestWaybackSnapshotTimestampRetriesTransientFailure(t *testing.T) {
	live := "http://pbs.twimg.com/media/AAA.jpg"
	fixture := &waybackFixture{
		snapshots: map[string]string{live: "20120601000000"},
		apiHits:   new(int32),
		failFirst: true,
	}
	srv := httptest.NewServer(fixture.handler(t))
	defer srv.Close()
	wb := &waybackClient{webBase: srv.URL, http: srv.Client(), sleep: func(time.Duration) {}}

	ts, err := wb.snapshotTimestamp(live)
	require.NoError(t, err)
	require.Equal(t, "20120601000000", ts)
	require.Equal(t, int32(2), atomic.LoadInt32(fixture.apiHits))
}

func TestWaybackSnapshotTimestampEmptyIsFinal(t *testing.T) {
	// The CDX index is authoritative: an empty answer needs no retry.
	fixture := &waybackFixture{apiHits: new(int32)}
	srv := httptest.NewServer(fixture.handler(t))
	defer srv.Close()
	wb := &waybackClient{webBase: srv.URL, http: srv.Client(), sleep: func(time.Duration) {}}

	_, err := wb.snapshotTimestamp("http://pbs.twimg.com/media/MISSING.jpg")
	require.ErrorIs(t, err, errNoWaybackSnapshot)
	require.Equal(t, int32(1), atomic.LoadInt32(fixture.apiHits))
}

func TestIsPermanentFetchFailure(t *testing.T) {
	require.True(t, isPermanentFetchFailure(errors.New("fetch http://x/y: unexpected status 404 Not Found")))
	require.True(t, isPermanentFetchFailure(errors.New("fetch http://x/y: unexpected status 403 Forbidden")))
	require.True(t, isPermanentFetchFailure(&net.DNSError{Err: "no such host", IsNotFound: true}))
	require.True(t, isPermanentFetchFailure(fmt.Errorf("fetch: %w", &net.DNSError{Err: "no such host", IsNotFound: true})))
	require.False(t, isPermanentFetchFailure(errors.New("fetch http://x/y: unexpected status 500 Internal Server Error")))
	require.False(t, isPermanentFetchFailure(&net.DNSError{Err: "server misbehaving"}))
	require.False(t, isPermanentFetchFailure(errors.New("dial tcp: i/o timeout")))
}

func TestTwimgFetchCandidates(t *testing.T) {
	require.Equal(t,
		[]string{"http://pbs.twimg.com/media/AAA.jpg"},
		twimgFetchCandidates("http://pbs.twimg.com/media/AAA.jpg"))

	require.Equal(t, []string{
		"https://pbs.twimg.com/media/Ay6pEhmCEAAfFEr.png",
		"https://pbs.twimg.com/media/Ay6pEhmCEAAfFEr?format=png&name=orig",
		"http://p.twimg.com/Ay6pEhmCEAAfFEr.png",
	}, twimgFetchCandidates("http://p.twimg.com/Ay6pEhmCEAAfFEr.png"))

	require.Equal(t, []string{
		"https://pbs.twimg.com/media/A1KIypuCEAAPJaY.jpg",
		"https://pbs.twimg.com/media/A1KIypuCEAAPJaY?format=jpg&name=orig",
		"http://p.twimg.com/A1KIypuCEAAPJaY.jpg",
	}, twimgFetchCandidates("http://p.twimg.com/A1KIypuCEAAPJaY.jpg"))

	// Unusable paths fall back to the URL itself.
	require.Equal(t,
		[]string{"http://p.twimg.com/"},
		twimgFetchCandidates("http://p.twimg.com/"))
}

func TestFetchSnapshotValidation(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/img", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg; charset=binary")
		w.Write([]byte("\xff\xd8jpeg-bytes"))
	})
	mux.HandleFunc("/html", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<html>not an image</html>"))
	})
	mux.HandleFunc("/empty", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	wb := &waybackClient{http: srv.Client()}

	content, ct, err := wb.fetchSnapshot(srv.URL + "/img")
	require.NoError(t, err)
	require.Equal(t, "image/jpeg", ct)
	require.NotEmpty(t, content)

	_, _, err = wb.fetchSnapshot(srv.URL + "/html")
	require.ErrorContains(t, err, "content type")

	_, _, err = wb.fetchSnapshot(srv.URL + "/empty")
	require.ErrorContains(t, err, "empty body")

	_, _, err = wb.fetchSnapshot(srv.URL + "/missing")
	require.ErrorContains(t, err, "404")
}

// fakeTwimgStorage mirrors nothing: live fetches and Posts are scripted.
type fakeTwimgStorage struct {
	liveOK  map[string]string
	liveErr map[string]error
	posts   int
	calls   int32
}

func (f *fakeTwimgStorage) Exists(name string) (bool, error) { return false, nil }
func (f *fakeTwimgStorage) Fetch(obj *media.Object) (*http.Response, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeTwimgStorage) Post(obj *media.Object) (*media.Object, error) {
	f.posts++
	obj.Path = fmt.Sprintf("a/b/%d", f.posts)
	if obj.MimeType == "" {
		obj.MimeType = "image/jpeg"
	}
	return obj, nil
}
func (f *fakeTwimgStorage) Thumbnail(obj *media.Object) (*media.Object, error) { return obj, nil }
func (f *fakeTwimgStorage) Mirror(obj *media.Object) (*media.Object, error)    { return obj, nil }
func (f *fakeTwimgStorage) FromUrl(filename, src, mimetype string) (*media.Object, error) {
	atomic.AddInt32(&f.calls, 1)
	if err, ok := f.liveErr[src]; ok {
		return nil, err
	}
	if mirrored, ok := f.liveOK[src]; ok {
		return &media.Object{
			Url: mirrored, Path: strings.TrimPrefix(mirrored, "https://m.friendfeed.me/"),
			Content: []byte("live image: " + src), MimeType: "image/jpeg",
		}, nil
	}
	return nil, fmt.Errorf("fetch %s: unexpected status 404 Not Found", src)
}

func TestFetchTwimgCandidateRetryPolicy(t *testing.T) {
	tests := []struct {
		name  string
		err   error
		calls int32
	}{
		{"server error retries", errors.New("fetch x: unexpected status 500 Internal Server Error"), 4},
		{"not found is final", errors.New("fetch x: unexpected status 404 Not Found"), 1},
		{"dns not found is final", &net.DNSError{Err: "no such host", IsNotFound: true}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storage := &fakeTwimgStorage{liveErr: map[string]error{"x": tt.err}}
			_, err := fetchTwimgCandidate(storage, &twimgRequestGate{}, "x", 3, 0)
			require.Error(t, err)
			require.Equal(t, tt.calls, atomic.LoadInt32(&storage.calls))
		})
	}
}

func putTwimgEntry(t *testing.T, db *store.Store, body string) string {
	t.Helper()
	id := uuid.Must(uuid.NewV4())
	_, err := model.Entry.Put(db, id.Bytes(), &pb.Entry{Id: id.String(), Body: body})
	require.NoError(t, err)
	return id.String()
}

func TestRunMirrorTwimg(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	entryA := putTwimgEntry(t, db, `<img src="http://pbs.twimg.com/media/LIVE.jpg">`)
	putTwimgEntry(t, db, `<img src="http://pbs.twimg.com/media/WAYBACK.jpg">`)
	putTwimgEntry(t, db, `<img src="http://pbs.twimg.com/media/GONE.jpg">`)
	putTwimgEntry(t, db, `<img src="http://pbs.twimg.com/media/NXHOST.jpg">`)
	putTwimgEntry(t, db, `<img src="http://pbs.twimg.com/media/FLAKY.jpg">`)
	putTwimgEntry(t, db, `<img src="http://p.twimg.com/RESCUE.png">`)

	storage := &fakeTwimgStorage{
		liveOK: map[string]string{
			"http://pbs.twimg.com/media/LIVE.jpg":    "https://m.friendfeed.me/0/1/livehash",
			"https://pbs.twimg.com/media/RESCUE.png": "https://m.friendfeed.me/9/9/rescued",
		},
		liveErr: map[string]error{
			"http://pbs.twimg.com/media/NXHOST.jpg": &net.DNSError{Err: "no such host", IsNotFound: true},
			"http://pbs.twimg.com/media/FLAKY.jpg":  errors.New("dial tcp: i/o timeout"),
		},
	}

	waybackURL := "http://pbs.twimg.com/media/WAYBACK.jpg"
	goneURL := "http://pbs.twimg.com/media/GONE.jpg"
	fixture := &waybackFixture{
		snapshots: map[string]string{waybackURL: "20120601000000"},
		images: map[string][]byte{
			"/web/20120601000000im_/" + waybackURL: []byte("\xff\xd8jpeg"),
		},
	}
	srv := httptest.NewServer(fixture.handler(t))
	defer srv.Close()
	wb := &waybackClient{webBase: srv.URL, http: srv.Client(), sleep: func(time.Duration) {}}

	outPath := filepath.Join(t.TempDir(), "sync.jsonl")
	opts := mirrorTwimgOptions{
		storage: storage, wayback: wb, outPath: outPath,
		workers: 4, waybackDelay: time.Millisecond,
	}

	stats, err := runMirrorTwimg(db, opts)
	require.NoError(t, err)
	require.Equal(t, 6, stats.entries)
	require.Equal(t, 6, stats.urls)
	require.Equal(t, 6, stats.pending)
	require.Equal(t, 2, stats.mirrored)
	require.Equal(t, 1, stats.wayback)
	require.Equal(t, 2, stats.dead)
	require.Equal(t, 1, stats.failed)

	records, err := loadTwimgSyncRecords(outPath)
	require.NoError(t, err)
	require.Len(t, records, 6)

	live := records["http://pbs.twimg.com/media/LIVE.jpg"]
	require.Equal(t, twimgStatusMirrored, live.Status)
	require.Equal(t, "https://m.friendfeed.me/0/1/livehash", live.NewURL)
	require.Equal(t, []string{entryA}, live.Refs)
	require.Equal(t, 1, live.Version)
	require.Equal(t, 1, live.RefCount)
	require.NotEmpty(t, live.SHA256)
	require.NotEmpty(t, live.ObjectKey)
	require.Positive(t, live.Bytes)

	// p.twimg.com URLs are rescued through their pbs.twimg.com candidates;
	// the record keeps the original URL as key and notes the fetch source.
	rescued := records["http://p.twimg.com/RESCUE.png"]
	require.Equal(t, twimgStatusMirrored, rescued.Status)
	require.Equal(t, "https://m.friendfeed.me/9/9/rescued", rescued.NewURL)
	require.Equal(t, "https://pbs.twimg.com/media/RESCUE.png", rescued.Via)

	recovered := records[waybackURL]
	require.Equal(t, twimgStatusWayback, recovered.Status)
	require.Equal(t, "https://m.friendfeed.me/a/b/1", recovered.NewURL)
	require.Contains(t, recovered.WaybackURL, "20120601000000im_")
	require.Empty(t, recovered.Error)

	gone := records[goneURL]
	require.Equal(t, twimgStatusDead, gone.Status)
	require.Equal(t, 404, gone.HTTPStatus)

	nxhost := records["http://pbs.twimg.com/media/NXHOST.jpg"]
	require.Equal(t, twimgStatusDead, nxhost.Status)
	require.Equal(t, 0, nxhost.HTTPStatus)

	flaky := records["http://pbs.twimg.com/media/FLAKY.jpg"]
	require.Equal(t, twimgStatusError, flaky.Status)

	// Rerun: terminal records resume, transient errors are retried.
	stats, err = runMirrorTwimg(db, opts)
	require.NoError(t, err)
	require.Equal(t, 5, stats.resumed)
	require.Equal(t, 1, stats.pending)
	require.Equal(t, 1, stats.failed)
	data, err := os.ReadFile(outPath)
	require.NoError(t, err)
	// The five terminal URLs were neither fetched nor appended again; only
	// the prior transient error produced one new audit record.
	require.Equal(t, 7, strings.Count(strings.TrimSpace(string(data)), "\n")+1)
}

func TestRunMirrorTwimgDryRun(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()
	putTwimgEntry(t, db, `<img src="http://pbs.twimg.com/media/LIVE.jpg">`)

	outPath := filepath.Join(t.TempDir(), "sync.jsonl")
	stats, err := runMirrorTwimg(db, mirrorTwimgOptions{outPath: outPath, dryRun: true})
	require.NoError(t, err)
	require.Equal(t, 1, stats.entries)
	require.Equal(t, 1, stats.urls)
	require.Equal(t, 1, stats.pending)

	_, err = os.Stat(outPath)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestRunMirrorTwimgNoWayback(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()
	putTwimgEntry(t, db, `<img src="http://pbs.twimg.com/media/GONE.jpg">`)
	putTwimgEntry(t, db, `<img src="http://pbs.twimg.com/media/NXHOST.jpg">`)
	putTwimgEntry(t, db, `<img src="http://pbs.twimg.com/media/FLAKY.jpg">`)

	storage := &fakeTwimgStorage{
		liveErr: map[string]error{
			"http://pbs.twimg.com/media/NXHOST.jpg": &net.DNSError{Err: "no such host", IsNotFound: true},
			"http://pbs.twimg.com/media/FLAKY.jpg":  errors.New("dial tcp: i/o timeout"),
		},
	}
	outPath := filepath.Join(t.TempDir(), "sync.jsonl")
	stats, err := runMirrorTwimg(db, mirrorTwimgOptions{
		storage: storage, outPath: outPath, workers: 2, noWayback: true,
	})
	require.NoError(t, err)
	// Without the Wayback stage no failure is final; everything is retriable.
	require.Equal(t, 0, stats.dead)
	require.Equal(t, 3, stats.failed)
}

// Ensure the fixture mux matches the exact raw capture URL shape the client
// builds, so a construction drift fails the end-to-end test.
func TestRawSnapshotURL(t *testing.T) {
	wb := &waybackClient{webBase: "https://web.archive.org"}
	got := wb.rawSnapshotURL("20120601000000", "http://pbs.twimg.com/media/AAA.jpg")
	require.Equal(t,
		"https://web.archive.org/web/20120601000000im_/http://pbs.twimg.com/media/AAA.jpg", got)

	_, err := url.Parse(got)
	require.NoError(t, err)
}

func TestScanTwimgURLsSkipsUndecodableEntries(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	// A corrupt value containing the marker must fail the scan loudly, the
	// same contract the other entry-table migrations follow.
	id := uuid.Must(uuid.NewV4())
	raw := append([]byte{0xff, 0xff}, []byte("twimg.com")...)
	require.NoError(t, db.Put(model.Entry.PrefixAppend(id.Bytes()), raw))

	_, _, err = scanTwimgURLs(db)
	require.Error(t, err)
	require.Contains(t, err.Error(), "decode entry")
}
