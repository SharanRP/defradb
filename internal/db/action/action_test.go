// Copyright 2026 Democratized Data Foundation
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.txt.
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0, included in the file
// licenses/APL.txt.

package action

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sourcenetwork/corekv"
	"github.com/sourcenetwork/corekv/memory"
	"github.com/sourcenetwork/defradb/client"
	"github.com/sourcenetwork/defradb/event"
	"github.com/sourcenetwork/defradb/internal/datastore"
	"github.com/sourcenetwork/defradb/internal/db/lock"
	"github.com/sourcenetwork/immutable"
)

type mockDeleteErrorStore struct {
	corekv.ReaderWriter
	deleteErr error
}

func (m *mockDeleteErrorStore) Delete(ctx context.Context, key []byte) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}
	return m.ReaderWriter.Delete(ctx, key)
}

func TestComplete_Success(t *testing.T) {
	ctx := context.Background()
	rootstore := memory.NewDatastore(ctx)
	lockSet := lock.NewLockSet()
	multistore := datastore.NewMultistore(rootstore, lockSet, immutable.None[int]())

	events := event.NewChannelBus(10, 10)
	defer events.Close()

	sub, err := events.Subscribe(event.ActionExecutionName)
	require.NoError(t, err)
	defer events.Unsubscribe(sub)

	collectionID := "test_collection"
	action := client.TruncateAction

	err = Register(ctx, multistore, events, collectionID, action)
	require.NoError(t, err)

	// Consume the registration event
	select {
	case <-sub.Message():
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Timeout waiting for register event")
	}

	err = Complete(ctx, multistore, events, collectionID, action)
	assert.NoError(t, err)

	// Verify event was published
	select {
	case msg := <-sub.Message():
		exec, ok := msg.Data.(event.ActionExecution)
		require.True(t, ok)
		assert.Equal(t, collectionID, exec.CollectionID)
		assert.Equal(t, action, exec.Action)
		assert.Equal(t, client.CompletedActionStatus, exec.Status)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Timeout waiting for complete event")
	}

	// Verify it was deleted from status store
	status, err := getStatus(multistore, collectionID, action)
	require.NoError(t, err)
	assert.Equal(t, client.NoneActionStatus, status)
}

func TestComplete_DeleteError(t *testing.T) {
	ctx := context.Background()
	expectedErr := errors.New("delete failed")
	rootstore := &mockDeleteErrorStore{
		ReaderWriter: memory.NewDatastore(ctx),
		deleteErr:    expectedErr,
	}
	lockSet := lock.NewLockSet()
	multistore := datastore.NewMultistore(rootstore, lockSet, immutable.None[int]())

	events := event.NewChannelBus(10, 10)
	defer events.Close()

	sub, err := events.Subscribe(event.ActionExecutionName)
	require.NoError(t, err)
	defer events.Unsubscribe(sub)

	collectionID := "test_collection"
	action := client.TruncateAction

	err = Register(ctx, multistore, events, collectionID, action)
	require.NoError(t, err)

	// Consume the registration event
	select {
	case <-sub.Message():
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Timeout waiting for register event")
	}

	err = Complete(ctx, multistore, events, collectionID, action)
	assert.ErrorIs(t, err, expectedErr)

	// Verify NO complete event was published
	select {
	case msg := <-sub.Message():
		t.Fatalf("Expected no complete event, but got: %v", msg)
	case <-time.After(100 * time.Millisecond):
		// Success: no event was published
	}

	// Verify key still exists (its status is InProgressActionStatus)
	// Temporarily disable error to read from the store using getStatus
	rootstore.deleteErr = nil
	status, err := getStatus(multistore, collectionID, action)
	require.NoError(t, err)
	assert.Equal(t, client.InProgressActionStatus, status)
}
