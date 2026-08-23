package server

import (
	"testing"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
)

func TestEntryVisibilityResolverMatrix(t *testing.T) {
	srv := newServiceServer(t)
	owner := createServiceUser(t, srv, "visibility-owner")
	follower := createServiceUser(t, srv, "visibility-follower")
	outsider := createServiceUser(t, srv, "visibility-outsider")
	super := createServiceUser(t, srv, "visibility-super")
	superProfile, err := model.GetProfileFromUuid(srv.rdb, super)
	require.NoError(t, err)
	superProfile.IsSuper = true
	_, err = model.Profile.Put(srv.rdb, super.Bytes(), superProfile)
	require.NoError(t, err)

	privateProfile, err := model.GetProfileFromUuid(srv.rdb, owner)
	require.NoError(t, err)
	privateProfile.Private = true
	_, err = model.Profile.Put(srv.rdb, owner.Bytes(), privateProfile)
	require.NoError(t, err)

	// The Follow edge is the authority checked for both private user feeds and
	// private Groups.
	require.NoError(t, srv.rdb.Put(
		model.NewKeyFrom(model.Follow.Prefix, follower.Bytes(), owner.Bytes()), []byte("1")))

	entry := &pb.Entry{ProfileUuid: owner.String()}
	tests := []struct {
		name     string
		viewer   string
		decision visibilityDecision
	}{
		{name: "anonymous", decision: visibilityDenied},
		{name: "owner", viewer: owner.String(), decision: visibilityAllowed},
		{name: "follower", viewer: follower.String(), decision: visibilityAllowed},
		{name: "outsider", viewer: outsider.String(), decision: visibilityDenied},
		{name: "super", viewer: super.String(), decision: visibilityAllowed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver, err := newEntryVisibilityResolver(srv, tt.viewer)
			require.NoError(t, err)
			decision, err := resolver.entry(entry)
			require.NoError(t, err)
			require.Equal(t, tt.decision, decision)
		})
	}

	privateProfile.Private = false
	_, err = model.Profile.Put(srv.rdb, owner.Bytes(), privateProfile)
	require.NoError(t, err)
	resolver, err := newEntryVisibilityResolver(srv, "")
	require.NoError(t, err)
	decision, err := resolver.entry(entry)
	require.NoError(t, err)
	require.Equal(t, visibilityAllowed, decision)
}

func TestEntryVisibilityResolverRejectsUnavailableIdentities(t *testing.T) {
	srv := newServiceServer(t)
	viewer := createServiceUser(t, srv, "visibility-deleted-viewer")
	profile, err := model.GetProfileFromUuid(srv.rdb, viewer)
	require.NoError(t, err)
	profile.Deleted = true
	_, err = model.Profile.Put(srv.rdb, viewer.Bytes(), profile)
	require.NoError(t, err)

	_, err = newEntryVisibilityResolver(srv, viewer.String())
	require.Error(t, err)

	resolver, err := newEntryVisibilityResolver(srv, "")
	require.NoError(t, err)
	decision, err := resolver.entry(&pb.Entry{ProfileUuid: uuid.Must(uuid.NewV4()).String()})
	require.NoError(t, err)
	require.Equal(t, visibilityTargetUnavailable, decision)
	decision, err = resolver.entry(&pb.Entry{ProfileUuid: "not-a-uuid"})
	require.NoError(t, err)
	require.Equal(t, visibilityTargetUnavailable, decision)
}
