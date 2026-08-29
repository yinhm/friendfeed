package server

import (
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"

	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
	"google.golang.org/protobuf/proto"
)

type profileLookup struct {
	profile *pb.Profile
	err     error
}

// profileResolver deduplicates stable-UUID profile reads within one request.
// It intentionally has no lifetime beyond its caller's feed construction.
type profileResolver struct {
	mdb     *store.Store
	results map[uuid.UUID]profileLookup
}

func newProfileResolver(mdb *store.Store) *profileResolver {
	return &profileResolver{
		mdb:     mdb,
		results: make(map[uuid.UUID]profileLookup),
	}
}

func (r *profileResolver) profile(profileUUID uuid.UUID) (*pb.Profile, error) {
	if profileUUID == uuid.Nil {
		return nil, errors.New("profile uuid is zero")
	}
	if result, ok := r.results[profileUUID]; ok {
		return result.profile, result.err
	}
	profile, err := model.GetProfileFromUuid(r.mdb, profileUUID)
	r.results[profileUUID] = profileLookup{profile: profile, err: err}
	return profile, err
}

func FormatFeedEntry(mdb *store.Store, req *pb.FeedRequest, entry *pb.Entry) error {
	return formatFeedEntryWithResolver(newProfileResolver(mdb), req, entry)
}

func formatFeedEntryWithResolver(resolver *profileResolver, req *pb.FeedRequest, entry *pb.Entry) error {
	if _, err := fmtEntryProfilesWithResolver(resolver, entry); err != nil {
		return err
	}
	// fmtComments(req, entry)
	// fmtLikes(req, entry)
	return nil
}

// fmtEntryProfiles resolves the entry author profile and refreshes
// the denormalized entry.From snapshot from it, then refreshes every
// comment and like actor reference via fmtCommentOrLike. Returns the
// resolved author profile.
func fmtEntryProfiles(mdb *store.Store, entry *pb.Entry) (*pb.Profile, error) {
	return fmtEntryProfilesWithResolver(newProfileResolver(mdb), entry)
}

// ErrInvalidEntryIdentity marks entries that can never be rendered because
// they carry no usable stable author identity. Unlike a transient lookup
// failure, this condition is permanent.
var ErrInvalidEntryIdentity = errors.New("entry has no stable author identity")

func fmtEntryProfilesWithResolver(resolver *profileResolver, entry *pb.Entry) (*pb.Profile, error) {
	// Refetch the author profile. Resolve by the stable ProfileUuid, NOT by
	// the denormalized From.Id: From.Id is a snapshot taken when the entry
	// was posted and goes stale if the author later renames their profile
	// ID. Resolving by id would then fail and 404 the entire feed.
	var profile *pb.Profile
	var err error
	stableAuthor := false
	if entry.ProfileUuid != "" {
		profileUUID, uerr := uuid.FromString(entry.ProfileUuid)
		if uerr != nil || profileUUID == uuid.Nil {
			// The zero UUID parses but is not a valid identity (same
			// contract as feedFromProfile/permOwnedBy/fmtCommentOrLike).
			return nil, fmt.Errorf("%w: entry ProfileUuid is invalid", ErrInvalidEntryIdentity)
		}
		profile, err = resolver.profile(profileUUID)
		stableAuthor = true
	} else if entry.From != nil {
		// Legacy entries without ProfileUuid fall back to id lookup.
		profile, err = model.GetProfileFromUserId(resolver.mdb, entry.From.Id)
	} else {
		return nil, fmt.Errorf("%w: entry has neither ProfileUuid nor From", ErrInvalidEntryIdentity)
	}
	if err != nil {
		// The author profile is gone (deleted, or never existed — e.g.
		// archived/imported entries). Comment and like refs carry their own
		// stable UUIDs and must still be hydrated; only the author refresh
		// is skipped. The error is still returned so callers that treat a
		// missing author as fatal (FetchEntry, ForwardFetchFeed) keep their
		// behavior, while lenient callers render the rest.
		for _, cmt := range entry.Comments {
			if cmt != nil {
				fmtCommentOrLikeWithResolver(resolver, cmt.From)
			}
		}
		for _, like := range entry.Likes {
			if like != nil {
				fmtCommentOrLikeWithResolver(resolver, like.From)
			}
		}
		return nil, err
	}

	if entry.From == nil {
		entry.From = &pb.Feed{}
	}
	// Refresh denormalized display fields from the canonical profile so a
	// renamed ID, updated name or picture render correctly for historical
	// entries.
	entry.From.Id = profile.Id
	entry.From.Name = profile.Name
	entry.From.Picture = profile.Picture
	entry.From.Type = profile.Type
	// Stamp the identity UUID only when the profile came from the stable
	// ProfileUuid. The legacy id fallback resolves through a recyclable
	// id; stamping there could misattribute the entry to whoever currently
	// owns that id.
	if stableAuthor {
		entry.From.Uuid = profile.Uuid
	}

	for _, cmt := range entry.Comments {
		if cmt != nil {
			fmtCommentOrLikeWithResolver(resolver, cmt.From)
		}
	}
	for _, like := range entry.Likes {
		if like != nil {
			fmtCommentOrLikeWithResolver(resolver, like.From)
		}
	}
	return profile, nil
}

// fmtCommentOrLike refreshes a denormalized comment/like actor reference
// from the canonical profile. UUID is the ONLY key: a legacy ref without
// one keeps its snapshot even if its From.Id currently resolves — the id
// may have been recycled by another user, so resolving it could
// misattribute the record. A ref whose UUID is malformed, or whose
// profile no longer exists, also keeps its snapshot: a single
// unresolvable reference must never fail the whole feed.
func fmtCommentOrLike(mdb *store.Store, from *pb.Feed) {
	fmtCommentOrLikeWithResolver(newProfileResolver(mdb), from)
}

func fmtCommentOrLikeWithResolver(resolver *profileResolver, from *pb.Feed) {
	if from == nil || from.Uuid == "" {
		return
	}
	profileUUID, err := uuid.FromString(from.Uuid)
	if err != nil {
		return
	}
	// The zero UUID parses but is not a valid identity (same contract as
	// permOwnedBy); never hydrate a ref to it.
	if profileUUID == uuid.Nil {
		return
	}
	profile, err := resolver.profile(profileUUID)
	if err != nil || profile == nil {
		return
	}
	from.Id = profile.Id
	from.Name = profile.Name
	from.Picture = profile.Picture
	from.Type = profile.Type
}

func BuildGraph(info *pb.Feedinfo) *pb.Graph {
	graph := &pb.Graph{
		Following: make(map[string]*pb.Profile),
		Followers: make(map[string]*pb.Profile),
		Admins:    make(map[string]*pb.Profile),
		Services:  make(map[string]*pb.FeedService),
	}
	// FIXME: subscriptions may huge
	// for _, item := range info.Subscriptions {
	// 	graph.Subscriptions[item.Id] = item
	// }
	for _, item := range info.Admins {
		graph.Admins[item.Id] = item
	}
	for _, item := range info.Services {
		graph.Services[item.Id] = item
	}
	return graph
}

func RandomPictureFromWallpaper(db *store.Store, profile *pb.Profile) string {
	profileUUID, err := uuid.FromString(profile.Uuid)
	if err != nil {
		slog.Debug("RandomPictureFromWallpaper", "err", err)
		return ""
	}
	profile, err = model.GetProfileFromUuid(db, profileUUID)
	if err != nil {
		slog.Debug("RandomPictureFromWallpaper: no profile", "err", err)
		return ""
	}

	// no update if not empty
	if profile.Picture != "" {
		return profile.Picture
	}

	bingUuid := model.UniqueKeyFrom("bing", "wallpaper")
	preKey := model.NewUUIDKey(model.TableEntryIndex, bingUuid)
	slog.Info("RandomPictureFromWallpaper", "pre_key", preKey.String())

	url := ""
	_, _ = db.ForwardScan(preKey, func(i int, k, v []byte) error {
		_, entryUUID, _, err := model.ParseEntryIndexKey(k)
		if err != nil {
			return err
		}
		entry := new(pb.Entry)
		rawdata, err := db.Get(model.Entry.PrefixAppend(entryUUID.Bytes()))
		if errors.Is(err, store.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := proto.Unmarshal(rawdata, entry); err != nil {
			return err
		}

		dice := rand.Intn(10)
		slog.Debug("entry key", "key", hex.EncodeToString(k), "dice", dice)
		if dice == 5 && len(entry.Thumbnails) > 0 && entry.Thumbnails[0] != nil {
			url = entry.Thumbnails[0].Url
			return &store.Error{Msg: "ok", Code: store.StopIteration} // stop scan
		}
		return nil
	})

	return url
}
