package pb

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

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
