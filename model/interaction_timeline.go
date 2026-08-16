package model

import (
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/store"
)

const interactionTimelinePositionSize = 8 + uuid.Size

func LikeTimelinePrefix(actor uuid.UUID) store.Key {
	return NewKeyFrom(LikeTimeline.Prefix, actor.Bytes())
}

func CommentTimelinePrefix(actor uuid.UUID) store.Key {
	return NewKeyFrom(CommentTimeline.Prefix, actor.Bytes())
}

func LikeTimelineKey(actor, entry uuid.UUID, created time.Time) (store.Key, error) {
	reverse, err := reverseTimelineMillis(created)
	if err != nil {
		return nil, err
	}
	return NewKeyFrom(LikeTimeline.Prefix, actor.Bytes(), reverse[:], entry.Bytes()), nil
}

func CommentTimelineKey(actor, entry uuid.UUID, created time.Time) (store.Key, error) {
	reverse, err := reverseTimelineMillis(created)
	if err != nil {
		return nil, err
	}
	return NewKeyFrom(CommentTimeline.Prefix, actor.Bytes(), reverse[:], entry.Bytes()), nil
}

func CommentTimelinePositionKey(actor, entry uuid.UUID) store.Key {
	return NewKeyFrom(CommentTimelinePosition.Prefix, actor.Bytes(), entry.Bytes())
}

func EncodeCommentTimelinePosition(created time.Time, comment uuid.UUID) ([]byte, error) {
	reverse, err := reverseTimelineMillis(created)
	if err != nil {
		return nil, err
	}
	value := make([]byte, 0, interactionTimelinePositionSize)
	value = append(value, reverse[:]...)
	value = append(value, comment.Bytes()...)
	return value, nil
}

func DecodeCommentTimelinePosition(raw []byte) (time.Time, uuid.UUID, error) {
	if len(raw) != interactionTimelinePositionSize {
		return time.Time{}, uuid.Nil, fmt.Errorf("invalid CommentTimelinePosition length %d", len(raw))
	}
	ms := ^binary.BigEndian.Uint64(raw[:8])
	if ms > uint64(^uint64(0)>>1) {
		return time.Time{}, uuid.Nil, errors.New("comment timeline timestamp overflows int64")
	}
	comment, err := uuid.FromBytes(raw[8:])
	return time.UnixMilli(int64(ms)).UTC(), comment, err
}

func ParseInteractionTimelineKey(key store.Key, table store.KeyPrefix) (actor, entry uuid.UUID, created time.Time, err error) {
	if table != TableLikeTimeline && table != TableCommentTimeline {
		return actor, entry, created, errors.New("invalid interaction timeline table")
	}
	if len(key) != 4+uuid.Size+interactionTimelinePositionSize {
		return actor, entry, created, fmt.Errorf("invalid interaction timeline key length %d", len(key))
	}
	if binary.BigEndian.Uint32(key[:4]) != uint32(table) {
		return actor, entry, created, errors.New("interaction timeline table mismatch")
	}
	actor, err = uuid.FromBytes(key[4:20])
	if err != nil {
		return
	}
	ms := ^binary.BigEndian.Uint64(key[20:28])
	if ms > uint64(^uint64(0)>>1) {
		err = errors.New("interaction timeline timestamp overflows int64")
		return
	}
	entry, err = uuid.FromBytes(key[28:44])
	created = time.UnixMilli(int64(ms)).UTC()
	return
}
