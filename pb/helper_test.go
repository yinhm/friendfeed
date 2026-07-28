package pb

import (
	"testing"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/assert"
)

func commandTestProfile() *Profile {
	return &Profile{
		Uuid: uuid.Must(uuid.NewV4()).String(),
		Id:   "renamed-user",
	}
}

func TestRebuildCommandUsesStableUUID(t *testing.T) {
	profile := commandTestProfile()

	tests := []struct {
		name  string
		entry *Entry
		want  []string
	}{
		{
			name: "owner after rename",
			entry: &Entry{
				ProfileUuid: profile.Uuid,
				From:        &Feed{Uuid: profile.Uuid, Id: "old-user"},
			},
			want: []string{"comment", "edit", "delete"},
		},
		{
			name: "same id different entry uuid is not owner",
			entry: &Entry{
				ProfileUuid: uuid.Must(uuid.NewV4()).String(),
				From:        &Feed{Id: profile.Id},
			},
			want: []string{"comment", "like"},
		},
		{
			name: "unlike after rename",
			entry: &Entry{
				ProfileUuid: uuid.Must(uuid.NewV4()).String(),
				Likes: []*Like{{
					From: &Feed{Uuid: profile.Uuid, Id: "old-user"},
				}},
			},
			want: []string{"comment", "unlike"},
		},
		{
			name: "same id different like uuid does not grant unlike",
			entry: &Entry{
				ProfileUuid: uuid.Must(uuid.NewV4()).String(),
				Likes: []*Like{{
					From: &Feed{Uuid: uuid.Must(uuid.NewV4()).String(), Id: profile.Id},
				}},
			},
			want: []string{"comment", "like"},
		},
		{
			name: "legacy like does not grant unlike",
			entry: &Entry{
				ProfileUuid: uuid.Must(uuid.NewV4()).String(),
				Likes:       []*Like{{From: &Feed{Id: profile.Id}}},
			},
			want: []string{"comment", "like"},
		},
		{
			name: "malformed like uuid does not grant unlike",
			entry: &Entry{
				ProfileUuid: uuid.Must(uuid.NewV4()).String(),
				Likes:       []*Like{{From: &Feed{Uuid: "not-a-uuid", Id: profile.Id}}},
			},
			want: []string{"comment", "like"},
		},
		{
			name: "zero like uuid does not grant unlike",
			entry: &Entry{
				ProfileUuid: uuid.Must(uuid.NewV4()).String(),
				Likes:       []*Like{{From: &Feed{Uuid: uuid.Nil.String(), Id: profile.Id}}},
			},
			want: []string{"comment", "like"},
		},
		{
			name: "nil like ref does not panic or grant unlike",
			entry: &Entry{
				ProfileUuid: uuid.Must(uuid.NewV4()).String(),
				Likes:       []*Like{{}},
			},
			want: []string{"comment", "like"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.entry.RebuildCommand(profile, nil)
			assert.Equal(t, tt.want, tt.entry.Commands)
		})
	}
}

func TestRebuildCommandRejectsInvalidViewerIdentity(t *testing.T) {
	entry := &Entry{
		ProfileUuid: uuid.Must(uuid.NewV4()).String(),
		Commands:    []string{"stale"},
	}

	for _, profile := range []*Profile{
		nil,
		{Id: "legacy"},
		{Uuid: "not-a-uuid", Id: "malformed"},
		{Uuid: uuid.Nil.String(), Id: "zero"},
	} {
		entry.RebuildCommand(profile, nil)
		assert.Empty(t, entry.Commands)
	}
}

func TestRebuildCommentsCommandUsesStableUUIDPermissions(t *testing.T) {
	profile := commandTestProfile()
	otherUUID := uuid.Must(uuid.NewV4()).String()

	entry := &Entry{
		ProfileUuid: uuid.Must(uuid.NewV4()).String(),
		Comments: []*Comment{
			{From: &Feed{Uuid: otherUUID, Id: profile.Id}},
			{From: &Feed{Id: profile.Id}},
			{From: &Feed{Uuid: "not-a-uuid", Id: profile.Id}},
			{From: &Feed{Uuid: uuid.Nil.String(), Id: profile.Id}},
			{},
			nil,
		},
	}

	entry.RebuildCommentsCommand(profile, nil)

	for i, comment := range entry.Comments {
		if comment == nil {
			continue
		}
		assert.Empty(t, comment.Commands, "comment %d", i)
	}
}

func TestRebuildCommentsCommandSeparatesEditAndDeletePermissions(t *testing.T) {
	author := commandTestProfile()
	entryOwner := commandTestProfile()
	super := commandTestProfile()
	super.IsSuper = true
	comment := func() *Comment {
		return &Comment{From: &Feed{Uuid: author.Uuid, Id: "old-author"}}
	}

	tests := []struct {
		name    string
		profile *Profile
		entry   *Entry
		want    []string
	}{
		{
			name:    "comment author can edit and delete after rename",
			profile: author,
			entry:   &Entry{ProfileUuid: entryOwner.Uuid, Comments: []*Comment{comment()}},
			want:    []string{"edit", "delete"},
		},
		{
			name:    "entry author can delete but not edit",
			profile: entryOwner,
			entry:   &Entry{ProfileUuid: entryOwner.Uuid, Comments: []*Comment{comment()}},
			want:    []string{"delete"},
		},
		{
			name:    "super can delete but not edit",
			profile: super,
			entry:   &Entry{ProfileUuid: entryOwner.Uuid, Comments: []*Comment{comment()}},
			want:    []string{"delete"},
		},
		{
			name:    "other user has no commands",
			profile: commandTestProfile(),
			entry:   &Entry{ProfileUuid: entryOwner.Uuid, Comments: []*Comment{comment()}},
			want:    []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.entry.RebuildCommentsCommand(tt.profile, nil)
			assert.Equal(t, tt.want, tt.entry.Comments[0].Commands)
		})
	}
}

func TestFormatLikesPlaceholderCount(t *testing.T) {
	entry := &Entry{
		Likes: []*Like{{}, {}, {}, {}, {}},
	}

	entry.FormatLikes(0)

	assert.Len(t, entry.Likes, 4)
	placeholder := entry.Likes[3]
	assert.True(t, placeholder.Placeholder)
	assert.Equal(t, "2 other people", placeholder.Body)
	assert.Equal(t, int32(2), placeholder.Num)
}

func TestFormatLikesDoesNotCollapseWhenRequested(t *testing.T) {
	entry := &Entry{
		Likes: []*Like{{}, {}, {}, {}, {}},
	}

	entry.FormatLikes(1)

	assert.Len(t, entry.Likes, 5)
}
