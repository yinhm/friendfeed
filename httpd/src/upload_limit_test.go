package server

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUploadLimitsArePerActorAndGlobal(t *testing.T) {
	server := &Server{uploadRequests: make(chan struct{}, 8), imageOperations: make(chan struct{}, 2), uploadUsers: make(map[string]int)}
	first, ok := server.beginUpload("actor", false)
	require.True(t, ok)
	second, ok := server.beginUpload("actor", false)
	require.True(t, ok)
	_, ok = server.beginUpload("actor", false)
	require.False(t, ok)
	other, ok := server.beginUpload("other", false)
	require.True(t, ok)
	first()
	second()
	other()
	require.Empty(t, server.uploadUsers)
}
