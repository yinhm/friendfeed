package server

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gofrs/uuid"
	"github.com/mmcdole/gofeed"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	taskqueue "github.com/yinhm/friendfeed/task"
	"github.com/yinhm/friendfeed/util"
	"google.golang.org/protobuf/proto"
)

const (
	rssFetchTaskType    = "rss.fetch"
	rssScheduleInterval = time.Minute
	rssFetchInterval    = 30 * time.Minute
	rssFetchMaxInterval = 24 * time.Hour
	rssHTTPTimeout      = 15 * time.Second
	rssMaxBodyBytes     = 5 << 20
	rssMaxItems         = 25
)

type rssFetchResult struct {
	feed         *gofeed.Feed
	status       int
	etag         string
	lastModified string
}

type rssFetcher func(context.Context, *pb.Subscription, *pb.SubscriptionState) (*rssFetchResult, error)

func rssTaskDefinition(handler taskqueue.Handler) taskqueue.Definition {
	return taskqueue.Definition{
		ValidatePayload: func(payload []byte, version uint32) error {
			if version != 1 {
				return fmt.Errorf("unsupported payload version %d", version)
			}
			message := new(pb.RSSFetchPayload)
			if err := proto.Unmarshal(payload, message); err != nil {
				return err
			}
			id, err := uuid.FromString(message.FeedUuid)
			if err != nil || id == uuid.Nil {
				return errors.New("valid feed_uuid is required")
			}
			return nil
		},
		MaxAttempts: 3, LeaseDuration: 2 * time.Minute, MaxLease: 30 * time.Minute,
		BackoffBase: time.Minute, BackoffCap: 30 * time.Minute, Handler: handler,
	}
}

// NewTaskRegistry is the single registry definition used by the server and
// offline queue tools. A nil RSS handler is valid for tools that only validate
// or replay persisted tasks.
func NewTaskRegistry(rssHandler taskqueue.Handler) (*taskqueue.Registry, error) {
	return taskqueue.NewRegistry(map[string]taskqueue.Definition{
		rssFetchTaskType: rssTaskDefinition(rssHandler),
	})
}

func (s *ApiServer) RSSScheduleLoop() {
	if !s.beginBackgroundJob() {
		return
	}
	defer s.wg.Done()
	ticker := time.NewTicker(rssScheduleInterval)
	defer ticker.Stop()
	for {
		if err := s.scheduleDueRSS(s.taskCtx, s.rssNow()); err != nil &&
			!errors.Is(err, context.Canceled) && !errors.Is(err, taskqueue.ErrClosed) {
			slog.Error("RSS scheduler scan failed", "error", err)
		}
		select {
		case <-s.taskCtx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *ApiServer) scheduleDueRSS(ctx context.Context, now time.Time) error {
	return model.SubscriptionState.Iter(s.rdb, func(key, value []byte) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		state := new(pb.SubscriptionState)
		if err := proto.Unmarshal(value, state); err != nil {
			slog.Error("skip corrupt RSS subscription state", "key", fmt.Sprintf("%x", key), "error", err)
			return nil
		}
		if state.NextFetchMs > now.UnixMilli() {
			return nil
		}
		feedID, err := uuid.FromString(state.FeedUuid)
		if err != nil || feedID == uuid.Nil {
			slog.Error("skip RSS subscription state with invalid feed UUID", "key", fmt.Sprintf("%x", key))
			return nil
		}
		hasFollowers, err := model.SubscriptionHasFollowers(s.rdb, feedID)
		if err != nil || !hasFollowers {
			return err
		}
		payload, err := proto.Marshal(&pb.RSSFetchPayload{FeedUuid: state.FeedUuid})
		if err != nil {
			return err
		}
		_, err = s.tasks.Enqueue(ctx, taskqueue.Spec{Type: rssFetchTaskType, Payload: payload, PayloadVersion: 1, IdempotencyKey: state.FeedUuid})
		return err
	})
}

func (s *ApiServer) handleRSSFetchTask(ctx context.Context, task *pb.Task) error {
	payload := new(pb.RSSFetchPayload)
	if err := proto.Unmarshal(task.Payload, payload); err != nil {
		return err
	}
	feedID, err := uuid.FromString(payload.FeedUuid)
	if err != nil {
		return err
	}
	subscription, err := model.GetSubscription(s.rdb, feedID)
	if errors.Is(err, model.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	state, err := model.GetSubscriptionState(s.rdb, feedID)
	if err != nil {
		return err
	}
	hasFollowers, err := model.SubscriptionHasFollowers(s.rdb, feedID)
	if err != nil || !hasFollowers {
		return err
	}
	if state.NextFetchMs > task.CreatedAtMs {
		return nil
	}

	hostLock, err := s.rssHostLock(subscription.Url)
	if err != nil {
		return err
	}
	hostLock.Lock()
	defer hostLock.Unlock()
	result, err := s.rssFetch(ctx, subscription, state)
	now := s.rssNow()
	if err != nil {
		if task.Attempts >= task.MaxAttempts {
			state.ConsecutiveFailures++
			state.LastFetchMs = now.UnixMilli()
			state.NextFetchMs = now.Add(longRSSBackoff(state.ConsecutiveFailures)).UnixMilli()
			if putErr := model.PutSubscriptionState(s.rdb, feedID, state); putErr != nil {
				return fmt.Errorf("fetch failed (%v), persist failure: %w", err, putErr)
			}
		}
		return err
	}
	if result.status != http.StatusNotModified {
		if err := s.importRSSItems(ctx, subscription, result.feed, now); err != nil {
			return err
		}
	}
	state.LastFetchMs = now.UnixMilli()
	state.HttpStatus = int32(result.status)
	state.Etag = result.etag
	state.LastModified = result.lastModified
	state.ConsecutiveFailures = 0
	if result.status == http.StatusNotModified || result.feed == nil || len(result.feed.Items) == 0 {
		state.EmptyFetches++
	} else {
		state.EmptyFetches = 0
	}
	state.NextFetchMs = now.Add(rssNextInterval(state.EmptyFetches)).UnixMilli()
	return model.PutSubscriptionState(s.rdb, feedID, state)
}

func (s *ApiServer) importRSSItems(ctx context.Context, subscription *pb.Subscription, feed *gofeed.Feed, now time.Time) error {
	if feed == nil {
		return nil
	}
	items := append([]*gofeed.Item(nil), feed.Items...)
	sort.SliceStable(items, func(i, j int) bool { return rssItemTime(items[i], now).After(rssItemTime(items[j], now)) })
	created := 0
	for _, item := range items {
		if created >= rssMaxItems {
			break
		}
		itemKey := strings.TrimSpace(item.GUID)
		if itemKey == "" {
			itemKey = strings.TrimSpace(item.Link)
		}
		if itemKey == "" {
			itemKey = item.Title + "\x00" + rssItemTime(item, now).Format(time.RFC3339Nano)
		}
		entryID := model.UniqueKeyFrom("rss", subscription.Url, itemKey)
		if _, err := model.GetEntry(s.rdb, entryID.String()); err == nil {
			continue
		} else if !errors.Is(err, model.ErrNotFound) {
			return fmt.Errorf("check RSS entry %s: %w", entryID, err)
		}
		link := safeRSSLink(item.Link, subscription.Url)
		body := item.Content
		if body == "" {
			body = item.Description
		}
		entry := &pb.Entry{
			Id: entryID.String(), Url: link, Date: rssItemTime(item, now).UTC().Format(time.RFC3339),
			Title: item.Title, Body: util.DefaultSanitize(body), RawLink: link,
			ProfileUuid: subscription.FeedUuid, FeedUuid: subscription.FeedUuid, Type: "rss",
			Via: &pb.Via{Name: subscription.Title, Url: subscription.Url},
		}
		if _, err := s.PostEntry(ctx, entry); err != nil {
			return err
		}
		created++
	}
	return nil
}

func rssItemTime(item *gofeed.Item, fallback time.Time) time.Time {
	if item != nil && item.PublishedParsed != nil {
		return item.PublishedParsed.UTC()
	}
	if item != nil && item.UpdatedParsed != nil {
		return item.UpdatedParsed.UTC()
	}
	return fallback.UTC()
}

func safeRSSLink(raw, fallback string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != "" && parsed.User == nil {
		return parsed.String()
	}
	return fallback
}

func (s *ApiServer) rssHostLock(raw string) (*sync.Mutex, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" {
		return nil, errors.New("invalid RSS host")
	}
	host := strings.ToLower(parsed.Hostname())
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(host))
	return &s.rssHostLocks[hash.Sum32()%uint32(len(s.rssHostLocks))], nil
}

func fetchRSS(ctx context.Context, subscription *pb.Subscription, state *pb.SubscriptionState) (*rssFetchResult, error) {
	// Do not inherit HTTP_PROXY: a proxy would receive the original target and
	// bypass the resolver/IP checks performed by safeRSSDialContext.
	transport := &http.Transport{DialContext: safeRSSDialContext}
	client := &http.Client{Transport: transport, Timeout: rssHTTPTimeout}
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many RSS redirects")
		}
		return validateRSSURL(req.URL)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, subscription.Url, nil)
	if err != nil {
		return nil, err
	}
	if err := validateRSSURL(req.URL); err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "ffdb-bot/1.0")
	if state.Etag != "" {
		req.Header.Set("If-None-Match", state.Etag)
	}
	if state.LastModified != "" {
		req.Header.Set("If-Modified-Since", state.LastModified)
	}
	resp, err := client.Do(req)
	if err != nil {
		var requestErr *url.Error
		if errors.As(err, &requestErr) {
			return nil, fmt.Errorf("RSS request failed: %w", requestErr.Err)
		}
		return nil, err
	}
	defer resp.Body.Close()
	result := &rssFetchResult{status: resp.StatusCode, etag: resp.Header.Get("ETag"), lastModified: resp.Header.Get("Last-Modified")}
	if resp.StatusCode == http.StatusNotModified {
		return result, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("RSS HTTP status %d", resp.StatusCode)
	}
	limited := io.LimitReader(resp.Body, rssMaxBodyBytes+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if len(body) > rssMaxBodyBytes {
		return nil, errors.New("RSS response exceeds 5 MiB")
	}
	result.feed, err = gofeed.NewParser().ParseString(string(body))
	return result, err
}

func validateRSSURL(target *url.URL) error {
	if target == nil || (target.Scheme != "http" && target.Scheme != "https") || target.Hostname() == "" || target.User != nil {
		return errors.New("RSS URL must be public http or https")
	}
	return nil
}

func safeRSSDialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, err
	}
	for _, address := range addresses {
		if rssPublicIP(address) {
			return (&net.Dialer{}).DialContext(ctx, network, net.JoinHostPort(address.String(), port))
		}
	}
	return nil, fmt.Errorf("RSS host %q has no public address", host)
}

func rssPublicIP(address netip.Addr) bool {
	if address.Is4() && netip.MustParsePrefix("100.64.0.0/10").Contains(address) {
		return false
	}
	return address.IsValid() && !address.IsLoopback() && !address.IsPrivate() && !address.IsLinkLocalUnicast() &&
		!address.IsLinkLocalMulticast() && !address.IsMulticast() && !address.IsUnspecified()
}

func rssNextInterval(empty uint32) time.Duration {
	interval := rssFetchInterval
	for i := uint32(0); i < empty && interval < rssFetchMaxInterval; i++ {
		interval *= 2
	}
	return min(interval, rssFetchMaxInterval)
}

func longRSSBackoff(failures uint32) time.Duration {
	interval := time.Hour
	for i := uint32(1); i < failures && interval < rssFetchMaxInterval; i++ {
		interval *= 2
	}
	return min(interval, rssFetchMaxInterval)
}
