package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/yinhm/friendfeed/media"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
	"github.com/yinhm/friendfeed/util"
)

// mirror_twimg rescues rotting twimg media referenced by archived entries:
// it scans the Entry table read-only, mirrors every URL that is still alive
// into the production media storage (local + R2), falls back to the Wayback
// Machine for dead ones, and records every outcome in a JSONL file. The
// database is never written; a later migration rewrites entry bodies from
// the recorded URL mapping.

var (
	twimgMarker = []byte("twimg.com")
	twimgURLRe  = regexp.MustCompile(`https?://[a-zA-Z0-9.-]*twimg\.com/[^\s"'<>\\)]+`)
)

// maxTwimgURLRefs caps the per-URL entry reference list; practically a URL
// appears in a handful of entries.
const maxTwimgURLRefs = 100

type twimgURLRef struct {
	entryIDs []string
	count    int
}

// twimgURLRefs maps a twimg URL to a bounded sample of referring Entry IDs
// plus the complete reference count. Production rewriting streams Entry
// again and uses URL -> NewURL; it never depends on this diagnostic sample.
type twimgURLRefs map[string]*twimgURLRef

func (refs twimgURLRefs) add(rawURL, entryID string) {
	ref, ok := refs[rawURL]
	if !ok {
		refs[rawURL] = &twimgURLRef{entryIDs: []string{entryID}, count: 1}
		return
	}
	for _, existing := range ref.entryIDs {
		if existing == entryID {
			return
		}
	}
	ref.count++
	if len(ref.entryIDs) < maxTwimgURLRefs {
		ref.entryIDs = append(ref.entryIDs, entryID)
	}
}

func collectTwimgURLs(text, entryID string, refs twimgURLRefs) {
	if !strings.Contains(text, "twimg.com") {
		return
	}
	for _, rawURL := range twimgURLRe.FindAllString(text, -1) {
		if rescuableTwimgURL(rawURL) {
			refs.add(rawURL, entryID)
		}
	}
}

// rescuableTwimgURL intentionally excludes retired static/profile hosts such
// as a0.twimg.com. The salvage contract is limited to original media URLs:
// live pbs objects and legacy p.twimg.com objects that can be mapped onto pbs.
func rescuableTwimgURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	switch strings.ToLower(u.Hostname()) {
	case "p.twimg.com", "pbs.twimg.com":
		return true
	default:
		return false
	}
}

// scanTwimgURLs streams the Entry table once, collecting twimg URLs from
// Body/RawBody HTML and Thumbnail/File fields. Memory stays bounded by the
// number of distinct twimg URLs (tens of thousands), not by entry count.
func scanTwimgURLs(db *store.Store) (twimgURLRefs, int, error) {
	refs := twimgURLRefs{}
	scanned := 0
	err := model.Entry.Iter(db, func(key, raw []byte) error {
		scanned++
		if !bytes.Contains(raw, twimgMarker) {
			return nil
		}
		entry := new(pb.Entry)
		if err := proto.Unmarshal(raw, entry); err != nil {
			return fmt.Errorf("decode entry at %x: %w", key, err)
		}
		collectTwimgURLs(entry.Body, entry.Id, refs)
		collectTwimgURLs(entry.RawBody, entry.Id, refs)
		for _, thumbnail := range entry.Thumbnails {
			if thumbnail != nil {
				collectTwimgURLs(thumbnail.Url, entry.Id, refs)
				collectTwimgURLs(thumbnail.Link, entry.Id, refs)
			}
		}
		for _, file := range entry.Files {
			if file != nil {
				collectTwimgURLs(file.Url, entry.Id, refs)
			}
		}
		return nil
	})
	return refs, scanned, err
}

// Sync record statuses written to the JSONL mapping file.
const (
	twimgStatusMirrored = "mirrored" // fetched from twimg and stored
	twimgStatusWayback  = "wayback"  // recovered from a Wayback capture
	twimgStatusDead     = "dead"     // gone upstream and not archived
	twimgStatusError    = "error"    // transient failure, retried on the next run
)

// twimgSyncRecord is one line of the sync mapping consumed by the later
// database rewrite: URL -> NewURL for statuses mirrored/wayback.
type twimgSyncRecord struct {
	Version       int      `json:"version"`
	URL           string   `json:"url"`
	Refs          []string `json:"refs,omitempty"`
	RefCount      int      `json:"ref_count"`
	RefsTruncated bool     `json:"refs_truncated,omitempty"`
	Status        string   `json:"status"`
	NewURL        string   `json:"new_url,omitempty"`
	Via           string   `json:"via,omitempty"` // fetch URL when it differs from URL
	HTTPStatus    int      `json:"http_status,omitempty"`
	WaybackURL    string   `json:"wayback_url,omitempty"`
	ObjectKey     string   `json:"object_key,omitempty"`
	SHA256        string   `json:"sha256,omitempty"`
	Bytes         int      `json:"bytes,omitempty"`
	ContentType   string   `json:"content_type,omitempty"`
	Error         string   `json:"error,omitempty"`
	CheckedAt     string   `json:"checked_at"`
}

func (r twimgSyncRecord) terminal() bool {
	if r.Version != twimgSyncVersion {
		return false
	}
	if r.Status == twimgStatusDead {
		return true
	}
	return (r.Status == twimgStatusMirrored || r.Status == twimgStatusWayback) &&
		r.NewURL != "" && r.ObjectKey != "" && r.SHA256 != "" && r.Bytes > 0
}

const twimgSyncVersion = 1

func setTwimgObjectMetadata(rec *twimgSyncRecord, obj *media.Object) error {
	if obj == nil || obj.Path == "" || len(obj.Content) == 0 {
		return errors.New("mirrored object is missing path or content")
	}
	sum := sha256.Sum256(obj.Content)
	rec.ObjectKey = obj.Path
	rec.SHA256 = hex.EncodeToString(sum[:])
	rec.Bytes = len(obj.Content)
	rec.ContentType = obj.MimeType
	return nil
}

// loadTwimgSyncRecords reads a previous run's mapping so reruns resume:
// terminal records are skipped, error records are retried.
func loadTwimgSyncRecords(path string) (map[string]twimgSyncRecord, error) {
	records := map[string]twimgSyncRecord{}
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return records, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	for {
		var rec twimgSyncRecord
		if err := dec.Decode(&rec); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("decode %s: %w", path, err)
		}
		if rec.URL == "" {
			return nil, fmt.Errorf("decode %s: record has empty url", path)
		}
		records[rec.URL] = rec
	}
	return records, nil
}

var fetchStatusRe = regexp.MustCompile(`unexpected status (\d{3})`)

// fetchHTTPStatus extracts the upstream status code from a media fetch error
// ("fetch <url>: unexpected status 404 Not Found"). 0 means the failure had
// no HTTP response (DNS, timeout, SSRF guard).
func fetchHTTPStatus(err error) int {
	if err == nil {
		return 0
	}
	m := fetchStatusRe.FindStringSubmatch(err.Error())
	if m == nil {
		return 0
	}
	code, _ := strconv.Atoi(m[1])
	return code
}

// isPermanentFetchFailure reports whether a live fetch failed for good:
// upstream 4xx, or the host is gone from DNS entirely (NXDOMAIN).
func isPermanentFetchFailure(err error) bool {
	if code := fetchHTTPStatus(err); code >= 400 && code < 500 {
		return true
	}
	var dnsErr *net.DNSError
	return errors.As(err, &dnsErr) && dnsErr.IsNotFound
}

// twimgFetchCandidates returns the fetch URLs to try for a scanned URL, in
// order. p.twimg.com was the 2010-era photo upload host and is long gone
// from DNS, but its images moved onto pbs.twimg.com/media/<basename>, so the
// pbs forms are tried first:
//
//	http://p.twimg.com/Ay6pEhmCEAAfFEr.png
//	  → https://pbs.twimg.com/media/Ay6pEhmCEAAfFEr.png
//	  → https://pbs.twimg.com/media/Ay6pEhmCEAAfFEr?format=png&name=orig
//	  → the original URL (a formality; it no longer resolves)
func twimgFetchCandidates(rawURL string) []string {
	u, err := url.Parse(rawURL)
	if err != nil || !strings.EqualFold(u.Hostname(), "p.twimg.com") {
		return []string{rawURL}
	}
	base := path.Base(u.Path)
	if base == "." || base == "/" || base == "" {
		return []string{rawURL}
	}
	ext := strings.ToLower(strings.TrimPrefix(path.Ext(base), "."))
	switch ext {
	case "jpeg":
		ext = "jpg"
	case "jpg", "png", "gif", "webp":
	default:
		ext = "jpg"
	}
	name := strings.TrimSuffix(base, path.Ext(base))
	return []string{
		"https://pbs.twimg.com/media/" + base,
		"https://pbs.twimg.com/media/" + name + "?format=" + ext + "&name=orig",
		rawURL,
	}
}

const (
	waybackWebBase     = "https://web.archive.org"
	waybackMaxAttempts = 3
	waybackMaxBody     = 32 << 20 // mirrors media.maxFetchBytes
)

var (
	errNoWaybackSnapshot = errors.New("no wayback snapshot")
	waybackTimestampRe   = regexp.MustCompile(`^\d{14}$`)
)

// waybackClient queries the Wayback CDX index and fetches raw captures.
// Targets are confined to archive.org hosts; the only caller input embedded
// in requests is the URL being looked up.
type waybackClient struct {
	webBase string
	http    *http.Client
	sleep   func(time.Duration) // nil: time.Sleep; tests stub it out
}

func newWaybackClient() *waybackClient {
	return &waybackClient{
		webBase: waybackWebBase,
		http: &http.Client{
			Timeout: 30 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 10 {
					return fmt.Errorf("stopped after %d redirects", len(via))
				}
				return nil
			},
		},
		sleep: time.Sleep,
	}
}

func (w *waybackClient) backoff(d time.Duration) {
	if w.sleep != nil {
		w.sleep(d)
		return
	}
	time.Sleep(d)
}

// snapshotTimestamp returns the timestamp of the latest successful image
// capture for rawURL from the Wayback CDX index. The CDX index is
// authoritative: an empty answer means no capture exists and is final;
// network errors and 5xx/429 (rate limiting) are retried with backoff.
func (w *waybackClient) snapshotTimestamp(rawURL string) (string, error) {
	api := w.webBase + "/cdx/search/cdx?url=" + url.QueryEscape(rawURL) +
		"&output=json&fl=timestamp&filter=statuscode:200&filter=mimetype:image.*&limit=-1"
	var lastErr error
	for attempt := 0; attempt < waybackMaxAttempts; attempt++ {
		if attempt > 0 {
			w.backoff(time.Duration(attempt*attempt) * time.Second)
		}
		resp, err := w.http.Get(api)
		if err != nil {
			lastErr = err
			continue
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("wayback cdx: %s", resp.Status)
			continue
		}
		if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("wayback cdx: %s", resp.Status)
		}
		var rows [][]string
		if err := json.Unmarshal(body, &rows); err != nil {
			return "", fmt.Errorf("wayback cdx decode: %w", err)
		}
		if len(rows) < 2 || len(rows[1]) == 0 || !waybackTimestampRe.MatchString(rows[1][0]) {
			return "", errNoWaybackSnapshot
		}
		return rows[1][0], nil
	}
	if lastErr == nil {
		lastErr = errors.New("wayback cdx: no attempt made")
	}
	return "", lastErr
}

// rawSnapshotURL builds the raw-content (im_) capture URL so the response is
// the archived object itself, not the Wayback-wrapped HTML page.
func (w *waybackClient) rawSnapshotURL(timestamp, rawURL string) string {
	return fmt.Sprintf("%s/web/%sim_/%s", w.webBase, timestamp, rawURL)
}

// fetchSnapshot downloads a raw capture, accepting only image payloads:
// anything else (error pages, HTML rewrites) is a lookup failure, never
// something to persist.
func (w *waybackClient) fetchSnapshot(rawURL string) (content []byte, contentType string, err error) {
	resp, err := w.http.Get(rawURL)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("wayback snapshot: %s", resp.Status)
	}
	contentType = resp.Header.Get("Content-Type")
	if ct, _, perr := mime.ParseMediaType(contentType); perr == nil {
		contentType = ct
	}
	if !strings.HasPrefix(contentType, "image/") && contentType != "application/octet-stream" {
		return nil, "", fmt.Errorf("wayback snapshot: unexpected content type %q", contentType)
	}
	content, err = io.ReadAll(io.LimitReader(resp.Body, waybackMaxBody+1))
	if err != nil {
		return nil, "", err
	}
	if len(content) > waybackMaxBody {
		return nil, "", fmt.Errorf("wayback snapshot: body exceeds %d bytes limit", waybackMaxBody)
	}
	if len(content) == 0 {
		return nil, "", errors.New("wayback snapshot: empty body")
	}
	return content, contentType, nil
}

// recoverViaWayback looks up and downloads an archived copy of rawURL.
func recoverViaWayback(wb *waybackClient, rawURL string) (snapshotURL string, content []byte, contentType string, err error) {
	timestamp, err := wb.snapshotTimestamp(rawURL)
	if err != nil {
		return "", nil, "", err
	}
	snapshotURL = wb.rawSnapshotURL(timestamp, rawURL)
	content, contentType, err = wb.fetchSnapshot(snapshotURL)
	if err != nil {
		return "", nil, "", err
	}
	return snapshotURL, content, contentType, nil
}

type mirrorTwimgOptions struct {
	config       *util.Config   // required unless dryRun or storage is set
	storage      media.Storage  // nil: built from config
	wayback      *waybackClient // nil: default client (unless noWayback)
	outPath      string
	workers      int
	requestDelay time.Duration
	backoffBase  time.Duration
	retries      int
	maxURLs      int
	noWayback    bool
	waybackDelay time.Duration
	dryRun       bool
}

// twimgRequestGate limits request starts globally, not per worker. Waiters
// recheck the deadline after sleeping so a concurrent 429 can extend it;
// adding workers may overlap storage work but cannot increase request rate.
type twimgRequestGate struct {
	mu    sync.Mutex
	next  time.Time
	delay time.Duration
}

func (g *twimgRequestGate) wait() {
	for {
		g.mu.Lock()
		now := time.Now()
		if !now.Before(g.next) {
			g.next = now.Add(g.delay)
			g.mu.Unlock()
			return
		}
		wait := g.next.Sub(now)
		g.mu.Unlock()
		time.Sleep(wait)
	}
}

func (g *twimgRequestGate) coolDown(delay time.Duration) {
	g.mu.Lock()
	defer g.mu.Unlock()
	until := time.Now().Add(delay)
	if until.After(g.next) {
		g.next = until
	}
}

func twimgRetryDelay(base time.Duration, retry int, status int) time.Duration {
	if retry < 1 {
		retry = 1
	}
	delay := base
	for i := 1; i < retry && delay < 5*time.Minute; i++ {
		delay *= 2
	}
	if delay > 5*time.Minute {
		delay = 5 * time.Minute
	}
	if status == http.StatusTooManyRequests && delay < time.Minute {
		return time.Minute
	}
	return delay
}

func fetchTwimgCandidate(storage media.Storage, gate *twimgRequestGate, candidate string, retries int, backoffBase time.Duration) (*media.Object, error) {
	var lastErr error
	for attempt := 0; attempt <= retries; attempt++ {
		gate.wait()
		obj, err := storage.FromUrl("", candidate, "")
		if err == nil {
			return obj, nil
		}
		lastErr = err
		status := fetchHTTPStatus(err)
		if (isPermanentFetchFailure(err) && status != http.StatusTooManyRequests) || attempt == retries {
			return nil, err
		}
		gate.coolDown(twimgRetryDelay(backoffBase, attempt+1, status))
	}
	return nil, lastErr
}

type mirrorTwimgStats struct {
	entries  int
	urls     int
	resumed  int
	pending  int
	mirrored int
	wayback  int
	dead     int
	failed   int
}

// liveFailure carries a live-fetch failure into the Wayback stage.
// permanent marks upstream 4xx responses, which never recover.
type liveFailure struct {
	rec       twimgSyncRecord
	permanent bool
}

func runMirrorTwimg(db *store.Store, opts mirrorTwimgOptions) (mirrorTwimgStats, error) {
	stats := mirrorTwimgStats{}
	if opts.workers < 0 || opts.requestDelay < 0 || opts.backoffBase < 0 || opts.retries < 0 || opts.waybackDelay < 0 {
		return stats, errors.New("mirror_twimg: workers, retries, and delays must not be negative")
	}
	refs, scanned, err := scanTwimgURLs(db)
	if err != nil {
		return stats, err
	}
	stats.entries = scanned
	stats.urls = len(refs)

	records, err := loadTwimgSyncRecords(opts.outPath)
	if err != nil {
		return stats, err
	}

	unseen := make([]string, 0, len(refs))
	retries := make([]string, 0)
	for rawURL := range refs {
		rec, ok := records[rawURL]
		if ok && rec.terminal() {
			stats.resumed++
			continue
		}
		if ok {
			retries = append(retries, rawURL)
		} else {
			unseen = append(unseen, rawURL)
		}
	}
	// Small salvage batches must make forward progress even when an old 404
	// remains retriable because Wayback is temporarily unavailable. Visit every
	// unseen URL before spending capacity on prior transient failures.
	sort.Strings(unseen)
	sort.Strings(retries)
	pending := append(unseen, retries...)
	if opts.maxURLs > 0 && len(pending) > opts.maxURLs {
		pending = pending[:opts.maxURLs]
	}
	stats.pending = len(pending)
	if opts.dryRun {
		return stats, nil
	}

	storage := opts.storage
	if storage == nil {
		if opts.config == nil {
			return stats, errors.New("mirror_twimg: -config is required without -dry-run")
		}
		storage = media.NewStorage(opts.config, 0)
	}
	// mediaBase mirrors media.defaultMediaBaseURL / cfg.MediaURL handling:
	// Post returns sharded paths that join onto the public media origin.
	mediaBase := mediaOrigin
	if opts.config != nil && opts.config.MediaURL != "" {
		mediaBase = strings.TrimRight(opts.config.MediaURL, "/")
	}
	wb := opts.wayback
	if wb == nil && !opts.noWayback {
		wb = newWaybackClient()
	}
	workers := opts.workers
	if workers <= 0 {
		workers = 2
	}
	requestGate := &twimgRequestGate{delay: opts.requestDelay}

	out, err := os.OpenFile(opts.outPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return stats, err
	}
	defer out.Close()

	results := make(chan twimgSyncRecord)
	writerDone := make(chan error, 1)
	go func() {
		buffered := bufio.NewWriter(out)
		enc := json.NewEncoder(buffered)
		processed := 0
		var writeErr error
		for rec := range results {
			if writeErr != nil {
				continue
			}
			if err := enc.Encode(rec); err != nil {
				writeErr = fmt.Errorf("encode result for %q: %w", rec.URL, err)
				continue
			}
			// A successful R2 PUT without its mapping is not useful for the
			// later production rewrite. Persist each record before counting it
			// complete; reruns remain cheap because object keys are content hashes.
			if err := buffered.Flush(); err != nil {
				writeErr = fmt.Errorf("flush result for %q to %s: %w", rec.URL, opts.outPath, err)
				continue
			}
			if err := out.Sync(); err != nil {
				writeErr = fmt.Errorf("sync result for %q to %s: %w", rec.URL, opts.outPath, err)
				continue
			}
			processed++
			switch rec.Status {
			case twimgStatusMirrored:
				stats.mirrored++
			case twimgStatusWayback:
				stats.wayback++
			case twimgStatusDead:
				stats.dead++
			default:
				stats.failed++
			}
			if processed%500 == 0 {
				log.Printf("mirror_twimg: processed=%d mirrored=%d wayback=%d dead=%d failed=%d",
					processed, stats.mirrored, stats.wayback, stats.dead, stats.failed)
			}
		}
		writerDone <- writeErr
	}()

	// Wayback stage: single-threaded and rate-limited; archive.org asks for
	// polite clients, and live failures are rare compared to live successes.
	wbJobs := make(chan liveFailure)
	var wbWg sync.WaitGroup
	if !opts.noWayback {
		wbWg.Add(1)
		go func() {
			defer wbWg.Done()
			first := true
			for fail := range wbJobs {
				if !first && opts.waybackDelay > 0 {
					time.Sleep(opts.waybackDelay)
				}
				first = false
				rec := fail.rec
				rec.CheckedAt = time.Now().UTC().Format(time.RFC3339)
				snapshotURL, content, contentType, err := recoverViaWayback(wb, rec.URL)
				if err != nil {
					rec.Error += "; wayback: " + err.Error()
					if fail.permanent && errors.Is(err, errNoWaybackSnapshot) {
						rec.Status = twimgStatusDead
					} else {
						rec.Status = twimgStatusError
					}
					results <- rec
					continue
				}
				obj := &media.Object{Url: snapshotURL, Content: content, MimeType: contentType}
				if _, err := storage.Post(obj); err != nil {
					rec.Status = twimgStatusError
					rec.Error += "; wayback post: " + err.Error()
					results <- rec
					continue
				}
				rec.Status = twimgStatusWayback
				rec.NewURL = mediaBase + "/" + obj.Path
				rec.WaybackURL = snapshotURL
				if err := setTwimgObjectMetadata(&rec, obj); err != nil {
					rec.Status = twimgStatusError
					rec.NewURL = ""
					rec.Error = "wayback metadata: " + err.Error()
					results <- rec
					continue
				}
				rec.Error = ""
				results <- rec
			}
		}()
	}

	jobs := make(chan string)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for rawURL := range jobs {
				ref := refs[rawURL]
				rec := twimgSyncRecord{
					Version:       twimgSyncVersion,
					URL:           rawURL,
					Refs:          ref.entryIDs,
					RefCount:      ref.count,
					RefsTruncated: ref.count > len(ref.entryIDs),
					CheckedAt:     time.Now().UTC().Format(time.RFC3339),
				}
				candidates := twimgFetchCandidates(rawURL)
				var firstErr error
				permanent := true
				for _, candidate := range candidates {
					obj, err := fetchTwimgCandidate(storage, requestGate, candidate, opts.retries, opts.backoffBase)
					if err == nil {
						if err := setTwimgObjectMetadata(&rec, obj); err != nil {
							if firstErr == nil {
								firstErr = err
							}
							permanent = false
							continue
						}
						rec.Status = twimgStatusMirrored
						rec.NewURL = obj.Url
						if candidate != rawURL {
							rec.Via = candidate
						}
						break
					}
					if firstErr == nil {
						firstErr = err
					}
					if !isPermanentFetchFailure(err) {
						permanent = false
					}
				}
				if rec.Status == twimgStatusMirrored {
					results <- rec
					continue
				}
				rec.HTTPStatus = fetchHTTPStatus(firstErr)
				rec.Error = firstErr.Error()
				if opts.noWayback {
					// Without the Wayback stage nothing is final: failures
					// stay retriable so a later wayback-enabled run can
					// still recover them.
					rec.Status = twimgStatusError
					results <- rec
					continue
				}
				wbJobs <- liveFailure{rec: rec, permanent: permanent}
			}
		}()
	}

	for _, rawURL := range pending {
		jobs <- rawURL
	}
	close(jobs)
	wg.Wait()
	if !opts.noWayback {
		close(wbJobs)
		wbWg.Wait()
	}
	close(results)
	if err := <-writerDone; err != nil {
		return stats, err
	}
	return stats, nil
}
