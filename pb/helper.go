package pb

import (
	"fmt"

	"github.com/gofrs/uuid"
)

// ProfileNewlyCreatedHeader is the gRPC response header key PutOAuth uses to
// signal that the login created the profile on this call. It travels in
// header metadata instead of the Profile message because Profile is a
// persisted type and must not carry transient RPC state.
const ProfileNewlyCreatedHeader = "x-profile-newly-created"

// validUUID and sameUUID mirror model.permOwnedBy's identity rules for UI
// command hints. Keep both packages aligned: malformed, missing, and zero
// UUIDs must fail closed, without falling back to a recyclable profile ID.
// They live here to avoid making pb depend on model.
func validUUID(value string) (uuid.UUID, bool) {
	id, err := uuid.FromString(value)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, false
	}
	return id, true
}

func sameUUID(value, other string) bool {
	id, ok := validUUID(value)
	if !ok {
		return false
	}
	otherID, ok := validUUID(other)
	return ok && id == otherID
}

func (e *Entry) RebuildCommand(profile *Profile, graph *Graph) {
	if e == nil {
		return
	}
	e.Commands = []string{}
	if profile == nil {
		return
	}
	if _, ok := validUUID(profile.Uuid); !ok {
		return
	}

	ownerOrSuper := profile.IsSuper
	commands := []string{"comment"}
	if graph != nil {
		if _, ok := graph.Admins[profile.Id]; ok {
			ownerOrSuper = true
		}
	}
	// FIXME: subscriptions may huge
	// if _, ok := graph.Subscriptions[author]; ok {
	// 	// private check
	// }
	if sameUUID(e.ProfileUuid, profile.Uuid) {
		ownerOrSuper = true
	} else {
		// liked?
		liked := false
		for _, like := range e.Likes {
			if like != nil && sameUUID(like.GetFrom().GetUuid(), profile.Uuid) {
				liked = true
				break
			}
		}
		if liked {
			commands = append(commands, "unlike")
		} else {
			commands = append(commands, "like")
		}
	}

	// place it in the end
	if ownerOrSuper {
		commands = append(commands, "edit", "delete")
	}
	e.Commands = commands
}

func (e *Entry) RebuildCommentsCommand(profile *Profile, graph *Graph) {
	if e == nil {
		return
	}
	viewerValid := profile != nil
	if viewerValid {
		_, viewerValid = validUUID(profile.Uuid)
	}
	entryOwner := viewerValid && sameUUID(e.ProfileUuid, profile.Uuid)
	canModerate := viewerValid && (entryOwner || profile.IsSuper)

	for _, cmt := range e.Comments {
		if cmt == nil {
			continue
		}
		cmt.Commands = []string{}
		if !viewerValid {
			continue
		}
		if sameUUID(cmt.GetFrom().GetUuid(), profile.Uuid) {
			cmt.Commands = append(cmt.Commands, "edit", "delete")
		} else if canModerate {
			cmt.Commands = append(cmt.Commands, "delete")
		}
	}
}

func (e *Entry) FormatComments(max int32) {
	// collapse comments
	length := len(e.Comments)
	if max == 0 && length > 4 {
		collapsing := &Comment{
			Body:        fmt.Sprintf("%d more comments", length-2),
			Num:         int32(length - 2),
			Placeholder: true,
		}
		e.Comments = []*Comment{e.Comments[0], collapsing, e.Comments[length-1]}
	}
}

func (e *Entry) FormatLikes(max int32) {
	// collapse likes
	length := len(e.Likes)
	if max == 0 && length > 4 {
		collapsing := &Like{
			Body:        fmt.Sprintf("%d other people", length-3),
			Num:         int32(length - 3),
			Placeholder: true,
		}
		e.Likes = e.Likes[:3]
		e.Likes = append(e.Likes, collapsing)
	}
}
