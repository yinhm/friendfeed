package model

import (
	"bytes"
	"errors"

	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/store"
)

// ListGroupAdminsPage returns a bounded page of authoritative GroupAdmin
// edges. cursor marks an already-returned admin and is excluded from the next
// page. This exists for notification fanout; the legacy ListGroupAdmins helper
// remains for small synchronous invariants such as last-admin checks.
func ListGroupAdminsPage(db *store.Store, group uuid.UUID, limit int, cursor uuid.UUID) ([]uuid.UUID, string, error) {
	if group == uuid.Nil {
		return nil, "", errors.New("group UUID is required")
	}
	if limit <= 0 {
		limit = 100
	}
	if limit > 200 {
		limit = 200
	}
	prefix := NewKeyFrom(GroupAdmin.Prefix, group.Bytes())
	iter, err := db.NewIterator(prefix)
	if err != nil {
		return nil, "", err
	}
	defer iter.Close()
	if cursor != uuid.Nil {
		cursorKey := NewKeyFrom(prefix, cursor.Bytes())
		iter.SeekGE(cursorKey)
		if iter.Valid() && bytes.Equal(iter.UnsafeRawKey(), cursorKey) {
			iter.Next()
		}
	} else {
		iter.First()
	}

	admins := make([]uuid.UUID, 0, limit)
	var last uuid.UUID
	for iter.Valid() && len(admins) < limit {
		suffix := iter.UnsafeRawKey()[len(prefix):]
		admin, parseErr := uuid.FromBytes(suffix)
		if parseErr == nil {
			admins = append(admins, admin)
			last = admin
		}
		iter.Next()
	}
	if err := iter.Error(); err != nil {
		return nil, "", err
	}
	next := ""
	if iter.Valid() && last != uuid.Nil {
		next = last.String()
	}
	return admins, next, nil
}
