package server

import (
	"context"
	"encoding/json"
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
	"github.com/yinhm/friendfeed/media"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	taskqueue "github.com/yinhm/friendfeed/task"
	"github.com/yinhm/friendfeed/util"
	"google.golang.org/protobuf/proto"
)

const (
	serviceFetchTaskType    = "service.fetch"
	feedServiceSeedTaskType = "feed_service.seed"
	rssUserAgent            = "Mozilla/5.0 (compatible; FriendFeed/1.0; +https://friendfeed.me/)"
	rssScheduleInterval     = time.Minute
	serviceFetchInterval    = 30 * time.Minute
	serviceFetchMaxInterval = 24 * time.Hour
	servicePermanentLimit   = 6
	servicePermanentWindow  = 7 * 24 * time.Hour
	rssHTTPTimeout          = 15 * time.Second
	rssMaxBodyBytes         = 5 << 20
	rssMaxItems             = 25
)

type serviceFetchResult struct {
	feed              *gofeed.Feed
	status            int
	etag              string
	lastModified      string
	finalURL          string
	permanentRedirect bool
}

type serviceSourceError struct {
	err       error
	permanent bool
}

func (e *serviceSourceError) Error() string { return e.err.Error() }
func (e *serviceSourceError) Unwrap() error { return e.err }

type serviceFetcher func(context.Context, *pb.Service, *pb.ServiceState) (*serviceFetchResult, error)

func serviceTaskDefinition(taskType string, handler taskqueue.Handler) taskqueue.Definition {
	return taskqueue.Definition{
		ValidatePayload: func(payload []byte, version uint32) error {
			if version != 1 {
				return fmt.Errorf("unsupported payload version %d", version)
			}
			var serviceRaw string
			switch taskType {
			case serviceFetchTaskType:
				message := new(pb.ServiceFetchPayload)
				if err := proto.Unmarshal(payload, message); err != nil {
					return err
				}
				serviceRaw = message.ServiceUuid
			case feedServiceSeedTaskType:
				message := new(pb.FeedServiceSeedPayload)
				if err := proto.Unmarshal(payload, message); err != nil {
					return err
				}
				serviceRaw = message.ServiceUuid
				target, err := uuid.FromString(message.TargetFeedUuid)
				if err != nil || target == uuid.Nil || message.ServiceId == "" {
					return errors.New("valid target_feed_uuid and service_id are required")
				}
			default:
				return fmt.Errorf("unsupported service task type %q", taskType)
			}
			serviceID, err := uuid.FromString(serviceRaw)
			if err != nil || serviceID == uuid.Nil {
				return errors.New("valid service_uuid is required")
			}
			return nil
		},
		MaxAttempts: 3, LeaseDuration: 2 * time.Minute, MaxLease: 30 * time.Minute,
		BackoffBase: time.Minute, BackoffCap: 30 * time.Minute, Handler: handler,
	}
}

func NewTaskRegistry(serviceHandler, homeRebuildHandler taskqueue.Handler) (*taskqueue.Registry, error) {
	return taskqueue.NewRegistry(map[string]taskqueue.Definition{
		serviceFetchTaskType:    serviceTaskDefinition(serviceFetchTaskType, serviceHandler),
		feedServiceSeedTaskType: serviceTaskDefinition(feedServiceSeedTaskType, serviceHandler),
		homeRebuildTaskType:     homeRebuildTaskDefinition(homeRebuildHandler),
	})
}

func (s *ApiServer) ServiceScheduleLoop() {
	if !s.beginBackgroundJob() {
		return
	}
	defer s.wg.Done()
	ticker := time.NewTicker(rssScheduleInterval)
	defer ticker.Stop()
	for {
		if err := s.scheduleDueServices(s.taskCtx, s.rssNow()); err != nil &&
			!errors.Is(err, context.Canceled) && !errors.Is(err, taskqueue.ErrClosed) {
			slog.Error("Service scheduler scan failed", "error", err)
		}
		select {
		case <-s.taskCtx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *ApiServer) scheduleDueServices(ctx context.Context, now time.Time) error {
	return model.ServiceState.Iter(s.rdb, func(key, value []byte) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		state := new(pb.ServiceState)
		if err := proto.Unmarshal(value, state); err != nil {
			slog.Error("skip corrupt ServiceState", "key", fmt.Sprintf("%x", key), "error", err)
			return nil
		}
		if state.NextFetchMs > now.UnixMilli() {
			return nil
		}
		if state.Status == model.ServiceStatusDead {
			return nil
		}
		serviceID, err := uuid.FromString(state.ServiceUuid)
		if err != nil || serviceID == uuid.Nil {
			slog.Error("skip ServiceState with invalid UUID", "key", fmt.Sprintf("%x", key))
			return nil
		}
		bindings, err := model.ListServiceFeedBindings(s.rdb, serviceID)
		if err != nil || len(bindings) == 0 {
			return err
		}
		payload, err := proto.Marshal(&pb.ServiceFetchPayload{ServiceUuid: state.ServiceUuid})
		if err != nil {
			return err
		}
		dueWindow := state.NextFetchMs / rssScheduleInterval.Milliseconds()
		_, err = s.tasks.Enqueue(ctx, taskqueue.Spec{
			Type: serviceFetchTaskType, Payload: payload, PayloadVersion: 1,
			IdempotencyKey: fmt.Sprintf("%s:%d", state.ServiceUuid, dueWindow),
		})
		return err
	})
}

func (s *ApiServer) handleServiceTask(ctx context.Context, task *pb.Task) error {
	var serviceID uuid.UUID
	var bindings []model.ServiceFeedBinding
	seed := task.Type == feedServiceSeedTaskType
	if seed {
		payload := new(pb.FeedServiceSeedPayload)
		if err := proto.Unmarshal(task.Payload, payload); err != nil {
			return err
		}
		var err error
		serviceID, err = uuid.FromString(payload.ServiceUuid)
		if err != nil {
			return err
		}
		target, err := uuid.FromString(payload.TargetFeedUuid)
		if err != nil {
			return err
		}
		binding, err := model.GetFeedService(s.rdb, target, payload.ServiceId)
		if errors.Is(err, model.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if binding.ServiceUuid != serviceID.String() || !binding.Enabled {
			return nil
		}
		bindings = []model.ServiceFeedBinding{{TargetFeedUUID: target, ServiceID: payload.ServiceId}}
	} else {
		payload := new(pb.ServiceFetchPayload)
		if err := proto.Unmarshal(task.Payload, payload); err != nil {
			return err
		}
		var err error
		serviceID, err = uuid.FromString(payload.ServiceUuid)
		if err != nil {
			return err
		}
		bindings, err = model.ListServiceFeedBindings(s.rdb, serviceID)
		if err != nil || len(bindings) == 0 {
			return err
		}
	}
	service, err := model.GetService(s.rdb, serviceID)
	if errors.Is(err, model.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	state, err := model.GetServiceState(s.rdb, serviceID)
	if err != nil {
		return err
	}
	if !seed && state.Status == model.ServiceStatusDead {
		return nil
	}
	if !seed && state.NextFetchMs > task.CreatedAtMs {
		return nil
	}
	fetchState := state
	if seed {
		fetchState = &pb.ServiceState{ServiceUuid: serviceID.String()}
	}
	hostLock, err := s.rssHostLock(model.ServiceFetchURL(service))
	if err != nil {
		return err
	}
	hostLock.Lock()
	defer hostLock.Unlock()
	result, err := s.serviceFetch(ctx, service, fetchState)
	now := s.rssNow().UTC()
	if err != nil {
		if task.Attempts >= task.MaxAttempts {
			wasDead := state.Status == model.ServiceStatusDead
			if wasDead {
				// Only a seed probe reaches here with a dead state (non-seed
				// dead sources return early above). A failed probe must still
				// re-enter scheduling, so restart the permanent-failure
				// candidate window instead of letting the already-expired
				// window snap straight back to dead with no next fetch.
				state.PermanentFailures = 0
				state.PermanentFailureSinceMs = 0
				state.DeadAtMs = 0
			}
			applyServiceFetchFailure(state, result, err, now)
			becameDead := !wasDead && state.Status == model.ServiceStatusDead
			if becameDead {
				if persistErr := s.persistDeadServiceFailure(ctx, serviceID, state); persistErr != nil {
					return fmt.Errorf("fetch failed (%v), persist dead transition: %w", err, persistErr)
				}
			} else if putErr := model.PutServiceState(s.rdb, serviceID, state); putErr != nil {
				return fmt.Errorf("fetch failed (%v), persist failure: %w", err, putErr)
			}
			// The source lifecycle now owns this resolved failure. Completing the
			// exhausted Task prevents normal dead/degraded sources from filling
			// TaskDone with an unbounded stream of DEAD executions.
			return nil
		}
		return err
	}
	if service.Kind == model.WebFeedServiceKind && result.permanentRedirect && result.finalURL != "" && result.finalURL != model.ServiceFetchURL(service) {
		service.FetchUrl = result.finalURL
		service.UpdatedAtMs = now.UnixMilli()
		if _, err := model.Service.Put(s.rdb, serviceID.Bytes(), service); err != nil {
			return err
		}
	}
	if result.status != http.StatusNotModified {
		if result.feed != nil && strings.TrimSpace(result.feed.Title) != "" {
			service.Title = strings.TrimSpace(result.feed.Title)
			service.UpdatedAtMs = now.UnixMilli()
			if _, err := model.Service.Put(s.rdb, serviceID.Bytes(), service); err != nil {
				return err
			}
		}
		var deliveryErrors []error
		for _, ref := range bindings {
			binding, err := model.GetFeedService(s.rdb, ref.TargetFeedUUID, ref.ServiceID)
			if errors.Is(err, model.ErrNotFound) {
				continue
			}
			if err != nil {
				deliveryErrors = append(deliveryErrors, fmt.Errorf("load FeedService %s/%s: %w", ref.TargetFeedUUID, ref.ServiceID, err))
				continue
			}
			if !binding.Enabled || binding.ServiceUuid != serviceID.String() {
				continue
			}
			if _, err := model.GetProfileFromUuid(s.rdb, ref.TargetFeedUUID); err != nil {
				if errors.Is(err, model.ErrNotFound) || errors.Is(err, model.ErrProfileDeleted) {
					if _, disableErr := model.SetFeedServiceEnabled(s.rdb, ref.TargetFeedUUID, ref.ServiceID, false); disableErr != nil && !errors.Is(disableErr, model.ErrNotFound) {
						deliveryErrors = append(deliveryErrors, fmt.Errorf("disable stale FeedService %s/%s: %w", ref.TargetFeedUUID, ref.ServiceID, disableErr))
					} else {
						slog.Warn("disabled FeedService for missing target", "service_uuid", serviceID, "target_feed_uuid", ref.TargetFeedUUID, "service_id", ref.ServiceID)
					}
					continue
				}
				deliveryErrors = append(deliveryErrors, fmt.Errorf("load FeedService target %s: %w", ref.TargetFeedUUID, err))
				continue
			}
			if result.feed != nil && strings.TrimSpace(result.feed.Title) != "" {
				binding, err = model.UpdateFeedServiceName(s.rdb, ref.TargetFeedUUID, ref.ServiceID, result.feed.Title)
				if err != nil {
					deliveryErrors = append(deliveryErrors, fmt.Errorf("update FeedService %s/%s title: %w", ref.TargetFeedUUID, ref.ServiceID, err))
					continue
				}
			}
			if err := s.importServiceItems(ctx, service, binding, ref.TargetFeedUUID, result.feed, now); err != nil {
				deliveryErrors = append(deliveryErrors, fmt.Errorf("deliver Service %s to %s/%s: %w", serviceID, ref.TargetFeedUUID, ref.ServiceID, err))
			}
		}
		if len(deliveryErrors) != 0 {
			if task.Attempts >= task.MaxAttempts {
				state.DeliveryFailures++
				state.Status = model.ServiceStatusActive
				state.LastFetchMs = now.UnixMilli()
				state.LastSuccessMs = now.UnixMilli()
				state.HttpStatus = int32(result.status)
				state.ConsecutiveFailures = 0
				state.PermanentFailures = 0
				state.PermanentFailureSinceMs = 0
				state.DeadAtMs = 0
				state.LastError = ""
				state.NextFetchMs = now.Add(longServiceBackoff(state.DeliveryFailures)).UnixMilli()
				if putErr := model.PutServiceState(s.rdb, serviceID, state); putErr != nil {
					return fmt.Errorf("delivery failed (%v), persist cooldown: %w", errors.Join(deliveryErrors...), putErr)
				}
				slog.Warn("Service delivery entered cooldown", "service_uuid", serviceID, "failures", state.DeliveryFailures, "next_fetch_ms", state.NextFetchMs)
				return nil
			}
			return errors.Join(deliveryErrors...)
		}
	}
	state.LastFetchMs = now.UnixMilli()
	state.LastSuccessMs = now.UnixMilli()
	state.HttpStatus = int32(result.status)
	state.Etag = result.etag
	state.LastModified = result.lastModified
	state.ConsecutiveFailures = 0
	state.PermanentFailures = 0
	state.PermanentFailureSinceMs = 0
	state.DeliveryFailures = 0
	state.Status = model.ServiceStatusActive
	state.DeadAtMs = 0
	state.LastError = ""
	if result.status == http.StatusNotModified || result.feed == nil || len(result.feed.Items) == 0 {
		state.EmptyFetches++
	} else {
		state.EmptyFetches = 0
	}
	state.NextFetchMs = now.Add(serviceNextInterval(service.Kind, state.EmptyFetches)).UnixMilli()
	return model.PutServiceState(s.rdb, serviceID, state)
}

func applyServiceFetchFailure(state *pb.ServiceState, result *serviceFetchResult, fetchErr error, now time.Time) {
	state.LastFetchMs = now.UnixMilli()
	state.LastError = truncateServiceError(fetchErr)
	if result != nil {
		state.HttpStatus = int32(result.status)
	}
	var sourceErr *serviceSourceError
	if errors.As(fetchErr, &sourceErr) && sourceErr.permanent {
		if state.PermanentFailures == 0 || state.PermanentFailureSinceMs == 0 {
			state.PermanentFailureSinceMs = now.UnixMilli()
		}
		state.PermanentFailures++
		state.ConsecutiveFailures = 0
		state.Status = model.ServiceStatusDegraded
		failureAge := now.Sub(time.UnixMilli(state.PermanentFailureSinceMs))
		if state.PermanentFailures >= servicePermanentLimit && failureAge >= servicePermanentWindow {
			state.Status = model.ServiceStatusDead
			state.DeadAtMs = now.UnixMilli()
			state.NextFetchMs = 0
			return
		}
		state.NextFetchMs = now.Add(longServiceBackoff(state.PermanentFailures)).UnixMilli()
		return
	}
	state.ConsecutiveFailures++
	state.PermanentFailures = 0
	state.PermanentFailureSinceMs = 0
	state.Status = model.ServiceStatusDegraded
	state.NextFetchMs = now.Add(longServiceBackoff(state.ConsecutiveFailures)).UnixMilli()
}

func truncateServiceError(err error) string {
	if err == nil {
		return ""
	}
	text := err.Error()
	if len(text) > 512 {
		text = text[:512]
	}
	return text
}

func (s *ApiServer) importServiceItems(ctx context.Context, service *pb.Service, binding *pb.FeedService, target uuid.UUID, feed *gofeed.Feed, now time.Time) error {
	if feed == nil {
		return nil
	}
	items := append([]*gofeed.Item(nil), feed.Items...)
	sort.SliceStable(items, func(i, j int) bool { return serviceItemTime(items[i], now).After(serviceItemTime(items[j], now)) })
	if len(items) > rssMaxItems {
		items = items[:rssMaxItems]
	}
	sort.SliceStable(items, func(i, j int) bool { return serviceItemTime(items[i], now).Before(serviceItemTime(items[j], now)) })
	for _, item := range items {
		itemKey := strings.TrimSpace(item.GUID)
		if itemKey == "" {
			itemKey = strings.TrimSpace(item.Link)
		}
		if itemKey == "" {
			itemKey = item.Title + "\x00" + serviceItemTime(item, now).Format(time.RFC3339Nano)
		}
		entryID := model.UniqueKeyFrom("external-entry", target.String(), service.Uuid, itemKey)
		if _, err := model.GetEntry(s.rdb, entryID.String()); err == nil {
			continue
		} else if !errors.Is(err, model.ErrNotFound) {
			return fmt.Errorf("check Service entry %s: %w", entryID, err)
		}
		link := safeRSSLink(item.Link, service.CanonicalUrl)
		body := item.Content
		if body == "" {
			body = item.Description
		}
		entry := &pb.Entry{
			Id: entryID.String(), Url: link, Date: serviceItemTime(item, now).Format(time.RFC3339),
			Title: item.Title, Body: util.DefaultSanitize(body), RawLink: link,
			ProfileUuid: target.String(), FeedUuid: target.String(), Type: "service",
			Via: &pb.Via{Name: binding.Name, Url: service.SiteUrl},
		}
		if service.Kind == model.BingWallpaperServiceKind {
			if err := s.attachBingWallpaperMedia(entry, item); err != nil {
				return err
			}
		}
		if _, err := s.postEntry(ctx, entry, true); err != nil {
			return err
		}
	}
	return nil
}

func (s *ApiServer) attachBingWallpaperMedia(entry *pb.Entry, item *gofeed.Item) error {
	if item == nil || len(item.Enclosures) != 1 || strings.TrimSpace(item.Enclosures[0].URL) == "" {
		return errors.New("Bing Wallpaper item has no image")
	}
	imageURL := item.Enclosures[0].URL
	parsed, err := url.Parse(imageURL)
	if err != nil || parsed.Scheme != "https" || !strings.EqualFold(parsed.Hostname(), "www.bing.com") {
		return errors.New("Bing Wallpaper image URL is invalid")
	}
	object := &media.Object{Filename: "bing-wallpaper.jpg", Url: imageURL}
	if _, err := s.fs.Fetch(object); err != nil {
		return fmt.Errorf("fetch Bing Wallpaper image: %w", err)
	}
	detected := http.DetectContentType(object.Content)
	if !strings.HasPrefix(detected, "image/") {
		return fmt.Errorf("Bing Wallpaper returned %s instead of an image", detected)
	}
	object.MimeType = detected
	if _, err := s.fs.Post(object); err != nil {
		return fmt.Errorf("store Bing Wallpaper image: %w", err)
	}
	object.Url = s.mediaBaseURL + "/" + strings.TrimLeft(object.Path, "/")
	entry.Url = object.Url
	entry.RawLink = object.Url
	thumbnail, err := s.fs.Thumbnail(object)
	if err != nil {
		return fmt.Errorf("thumbnail Bing Wallpaper image: %w", err)
	}
	if thumbnail.Url == "" {
		thumbnail.Url = s.mediaBaseURL + "/" + strings.TrimLeft(thumbnail.Path, "/")
	}
	entry.Thumbnails = []*pb.Thumbnail{{
		Url: thumbnail.Url, Link: object.Url, Width: thumbnail.Width, Height: thumbnail.Height,
	}}
	return nil
}

func serviceItemTime(item *gofeed.Item, fallback time.Time) time.Time {
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
		return nil, errors.New("invalid Service host")
	}
	host := strings.ToLower(parsed.Hostname())
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(host))
	return &s.rssHostLocks[hash.Sum32()%uint32(len(s.rssHostLocks))], nil
}

func fetchServiceHTTP(ctx context.Context, service *pb.Service, state *pb.ServiceState) (*serviceFetchResult, error) {
	fetchURL := model.ServiceFetchURL(service)
	if service.Kind == model.BingWallpaperServiceKind && fetchURL != model.BingWallpaperFetchURL {
		return nil, &serviceSourceError{err: errors.New("Bing Wallpaper fetch URL is not canonical"), permanent: true}
	}
	transport := &http.Transport{DialContext: safeRSSDialContext}
	client := &http.Client{Transport: transport, Timeout: rssHTTPTimeout}
	permanentRedirect := false
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many Service redirects")
		}
		if err := validateRSSURL(req.URL); err != nil {
			return err
		}
		if service.Kind == model.BingWallpaperServiceKind && !strings.EqualFold(req.URL.Hostname(), "www.bing.com") {
			return errors.New("Bing Wallpaper redirect left www.bing.com")
		}
		if req.Response != nil && (req.Response.StatusCode == http.StatusMovedPermanently || req.Response.StatusCode == http.StatusPermanentRedirect) {
			permanentRedirect = true
		}
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fetchURL, nil)
	if err != nil {
		return nil, err
	}
	if err := validateRSSURL(req.URL); err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", rssUserAgent)
	if state != nil && state.Etag != "" {
		req.Header.Set("If-None-Match", state.Etag)
	}
	if state != nil && state.LastModified != "" {
		req.Header.Set("If-Modified-Since", state.LastModified)
	}
	resp, err := client.Do(req)
	if err != nil {
		var requestErr *url.Error
		if errors.As(err, &requestErr) {
			return nil, fmt.Errorf("Service request failed: %w", requestErr.Err)
		}
		return nil, err
	}
	defer resp.Body.Close()
	finalURL, err := model.NormalizeServiceURL(resp.Request.URL.String())
	if err != nil {
		return nil, err
	}
	result := &serviceFetchResult{status: resp.StatusCode, etag: resp.Header.Get("ETag"), lastModified: resp.Header.Get("Last-Modified"), finalURL: finalURL, permanentRedirect: permanentRedirect}
	if resp.StatusCode == http.StatusNotModified {
		return result, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return result, &serviceSourceError{err: fmt.Errorf("Service HTTP status %d", resp.StatusCode), permanent: permanentHTTPStatus(resp.StatusCode)}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, rssMaxBodyBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > rssMaxBodyBytes {
		return result, &serviceSourceError{err: errors.New("Service response exceeds 5 MiB"), permanent: true}
	}
	if service.Kind == model.BingWallpaperServiceKind {
		result.feed, err = parseBingWallpaper(body)
	} else {
		result.feed, err = gofeed.NewParser().ParseString(string(body))
	}
	if err != nil {
		return result, &serviceSourceError{err: fmt.Errorf("parse Service feed: %w", err), permanent: true}
	}
	return result, nil
}

type bingWallpaperResponse struct {
	Images []struct {
		FullStartDate string `json:"fullstartdate"`
		URLBase       string `json:"urlbase"`
		Copyright     string `json:"copyright"`
	} `json:"images"`
}

func parseBingWallpaper(body []byte) (*gofeed.Feed, error) {
	var response bingWallpaperResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("decode Bing Wallpaper response: %w", err)
	}
	if len(response.Images) == 0 || len(response.Images) > 10 {
		return nil, fmt.Errorf("Bing Wallpaper response has %d images", len(response.Images))
	}
	feed := &gofeed.Feed{Title: "Bing Wallpaper"}
	for _, image := range response.Images {
		published, err := time.ParseInLocation("200601021504", image.FullStartDate, time.FixedZone("UTC+8", 8*60*60))
		if err != nil || !validBingWallpaperURLBase(image.URLBase) {
			return nil, errors.New("Bing Wallpaper item is invalid")
		}
		imageURL := "https://www.bing.com" + image.URLBase + "_UHD.jpg"
		published = published.UTC()
		copyright := strings.TrimSpace(image.Copyright)
		feed.Items = append(feed.Items, &gofeed.Item{
			GUID: image.URLBase, Title: copyright, Description: copyright, Link: imageURL,
			PublishedParsed: &published,
			Enclosures:      []*gofeed.Enclosure{{URL: imageURL, Type: "image/jpeg"}},
		})
	}
	return feed, nil
}

func validBingWallpaperURLBase(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	query := parsed.Query()
	if parsed.Path != "/th" || parsed.Fragment != "" || len(query) != 1 || len(query["id"]) != 1 {
		return false
	}
	id := query.Get("id")
	if !strings.HasPrefix(id, "OHR.") || len(id) <= len("OHR.") {
		return false
	}
	for _, char := range id[len("OHR."):] {
		if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') && (char < '0' || char > '9') && char != '-' && char != '_' {
			return false
		}
	}
	return true
}

func permanentHTTPStatus(status int) bool {
	return status >= 400 && status < 500 && status != http.StatusRequestTimeout && status != http.StatusTooEarly && status != http.StatusTooManyRequests
}

func validateRSSURL(target *url.URL) error {
	if target == nil || (target.Scheme != "http" && target.Scheme != "https") || target.Hostname() == "" || target.User != nil {
		return errors.New("Service URL must be public http or https")
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
	return nil, fmt.Errorf("Service host %q has no public address", host)
}

func rssPublicIP(address netip.Addr) bool {
	if address.Is4() && netip.MustParsePrefix("100.64.0.0/10").Contains(address) {
		return false
	}
	return address.IsValid() && !address.IsLoopback() && !address.IsPrivate() && !address.IsLinkLocalUnicast() &&
		!address.IsLinkLocalMulticast() && !address.IsMulticast() && !address.IsUnspecified()
}

func serviceNextInterval(kind string, empty uint32) time.Duration {
	if kind == model.BingWallpaperServiceKind {
		return serviceFetchMaxInterval
	}
	interval := serviceFetchInterval
	for i := uint32(0); i < empty && interval < serviceFetchMaxInterval; i++ {
		interval *= 2
	}
	return min(interval, serviceFetchMaxInterval)
}

func longServiceBackoff(failures uint32) time.Duration {
	interval := time.Hour
	for i := uint32(1); i < failures && interval < serviceFetchMaxInterval; i++ {
		interval *= 2
	}
	return min(interval, serviceFetchMaxInterval)
}
