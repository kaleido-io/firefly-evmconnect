// Copyright © 2026 Kaleido, Inc.
//
// SPDX-License-Identifier: Apache-2.0
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package ethereum

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/hyperledger-firefly/common/pkg/config"
	"github.com/hyperledger-firefly/common/pkg/fftypes"
	"github.com/hyperledger-firefly/evmconnect/pkg/ethrpc"
	"github.com/hyperledger-firefly/signer/pkg/ethtypes"
	"github.com/hyperledger-firefly/transaction-manager/pkg/ffcapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// TestEventStreamLightModeCheckpointFollowsChainHead demonstrates the event-listening impact of a
// light-mode block listener whose GetHighestBlock does not follow the chain head.
//
// It drives a REAL block listener (light mode, polling eth_blockNumber) under a REAL event stream
// with one listener, and observes only what FFTM observes: the checkpoint returned by
// EventListenerHWM. FFTM persists that value every checkpointInterval (default 1m) whenever no
// events flow, and on restart it is the block the stream resumes from.
//
// The chain starts at startHeight and then advances by more than catchupThreshold (500) plus
// checkpointBlockGap (50), standing in for a process that has been up for a long time. With a
// healthy connector the reported checkpoint follows the head (about 50 blocks behind it), so a
// restart resumes near the head. With the frozen head the checkpoint stays pinned at the height the
// process started at, however long it runs, so a restart must replay every block since process start
// in catchup pages before it can see anything at the head - which is the "missing events after
// restart, then a 15 day replay" symptom.
func TestEventStreamLightModeCheckpointFollowsChainHead(t *testing.T) {
	const startHeight int64 = 1000
	const laterHeight int64 = startHeight + 700 // > DefaultEventsCatchupThreshold + DefaultEventsCheckpointBlockGap

	var chainHead atomic.Int64
	chainHead.Store(startHeight)

	ctx, c, mRPC, done := newTestConnectorWithNoBlockerFilterDefaultMocks(t, func(conf config.Section) {
		conf.Set(ChainTrackingMode, string(ffcapi.ChainTrackingModeLight))
		conf.Set(BlockPollingInterval, "5ms") // real block listener loop, polling eth_blockNumber
	})

	// The node: a chain whose head is whatever the test says it is
	mRPC.On("CallRPC", mock.Anything, mock.Anything, "eth_blockNumber").Return(nil).Run(func(args mock.Arguments) {
		*args[1].(*ethtypes.HexInteger) = *ethtypes.NewHexInteger64(chainHead.Load())
	}).Maybe()
	mRPC.On("CallRPC", mock.Anything, mock.Anything, "eth_getBlockByNumber", mock.Anything, false).Return(nil).Maybe()
	// Block filter used by the block listener loop (light mode still polls it before eth_blockNumber)
	mRPC.On("CallRPC", mock.Anything, mock.Anything, "eth_newBlockFilter").Return(nil).Run(func(args mock.Arguments) {
		*args[1].(*string) = testBlockFilterID1
	}).Maybe()
	mRPC.On("CallRPC", mock.Anything, mock.Anything, "eth_getFilterChanges", testBlockFilterID1).Return(nil).Run(func(args mock.Arguments) {
		*args[1].(*[]ethtypes.HexBytes0xPrefix) = nil
	}).Maybe()
	// Event polling: no events ever, whichever mechanism the stream uses (server-side filter or eth_getLogs)
	mRPC.On("CallRPC", mock.Anything, mock.Anything, "eth_newFilter", mock.Anything).Return(nil).Run(func(args mock.Arguments) {
		*args[1].(*string) = testLogsFilterID1
	}).Maybe()
	mRPC.On("CallRPC", mock.Anything, mock.Anything, "eth_getFilterLogs", testLogsFilterID1).Return(nil).Run(func(args mock.Arguments) {
		*args[1].(*[]*ethrpc.LogJSONRPC) = []*ethrpc.LogJSONRPC{}
	}).Maybe()
	mRPC.On("CallRPC", mock.Anything, mock.Anything, "eth_getFilterChanges", testLogsFilterID1).Return(nil).Run(func(args mock.Arguments) {
		*args[1].(*[]*ethrpc.LogJSONRPC) = []*ethrpc.LogJSONRPC{}
	}).Maybe()
	mRPC.On("CallRPC", mock.Anything, mock.Anything, "eth_getLogs", mock.Anything).Return(nil).Run(func(args mock.Arguments) {
		*args[1].(*[]*ethrpc.LogJSONRPC) = []*ethrpc.LogJSONRPC{}
	}).Maybe()
	mRPC.On("CallRPC", mock.Anything, mock.Anything, "eth_uninstallFilter", mock.Anything).Return(nil).Run(func(args mock.Arguments) {
		*args[1].(*bool) = true
	}).Maybe()

	// The stream's block channel, as FFTM's confirmation manager would drain it. We remember the last
	// head number delivered, which is the "live" view of the chain that keeps confirmations working.
	blocks := make(chan *ffcapi.BlockHashEvent, 16)
	var headSeenByFFTM atomic.Int64
	go func() {
		for {
			select {
			case b := <-blocks:
				headSeenByFFTM.Store(int64(b.HeadBlockNumber)) //nolint:gosec
			case <-ctx.Done():
				return
			}
		}
	}()

	// One listener, already checkpointed at the start height - as FFTM would hand it back to us
	esID := fftypes.NewUUID()
	lID := fftypes.NewUUID()
	c.chainID = "12345" // avoid net_version during enrich
	c.eventFilterPollingInterval = 1 * time.Millisecond
	c.retry.MaximumDelay = 1 * time.Microsecond
	_, _, err := c.EventStreamStart(ctx, &ffcapi.EventStreamStartRequest{
		ID:            esID,
		StreamContext: ctx,
		EventStream:   make(chan *ffcapi.ListenerEvent),
		BlockListener: blocks,
		InitialListeners: []*ffcapi.EventListenerAddRequest{{
			StreamID:   esID,
			ListenerID: lID,
			Name:       "settlement-listener",
			EventListenerOptions: ffcapi.EventListenerOptions{
				FromBlock: "0",
				Filters: []fftypes.JSONAny{*fftypes.JSONAnyPtr(`{
					"address": "0x5600fF383458ae30dE902D096bA89f7F81f0a2fC",
					"event": ` + abiTransferEvent + `
				}`)},
				Options: fftypes.JSONAnyPtr(`{}`),
			},
			Checkpoint: &listenerCheckpoint{Block: startHeight, TransactionIndex: -1, LogIndex: -1},
		}},
	})
	require.NoError(t, err)
	defer func() {
		done()
		_, _, err := c.EventStreamStopped(ctx, &ffcapi.EventStreamStoppedRequest{ID: esID})
		assert.NoError(t, err)
	}()

	hwmBlock := func() int64 {
		res, _, err := c.EventListenerHWM(ctx, &ffcapi.EventListenerHWMRequest{StreamID: esID, ListenerID: lID})
		require.NoError(t, err)
		return res.Checkpoint.(*listenerCheckpoint).Block
	}

	// At start the checkpoint FFTM would persist is the start height
	assert.Equal(t, startHeight, hwmBlock())

	// Time passes: the chain moves on by 700 blocks while the process keeps running
	chainHead.Store(laterHeight)

	// FFTM's confirmation manager is told about the new head - so confirmations keep working and the
	// stream looks healthy from the outside
	require.Eventually(t, func() bool {
		return headSeenByFFTM.Load() == laterHeight
	}, 2*time.Second, 5*time.Millisecond, "FFTM never received the new chain head from the block listener")

	// The checkpoint the event stream reports to FFTM must follow the head too. This is what FFTM writes
	// to its store every minute, and what a restart resumes from.
	if !assert.Eventually(t, func() bool {
		return hwmBlock() > startHeight
	}, 2*time.Second, 5*time.Millisecond) {
		t.Errorf("event listener checkpoint frozen at %d while the chain head is %d: "+
			"FFTM would persist %d, and a restart would have to replay %d blocks in catchup pages (threshold %d) "+
			"before delivering anything at the head",
			hwmBlock(), laterHeight, hwmBlock(), laterHeight-hwmBlock(), c.catchupThreshold)
		return
	}

	// Healthy: the checkpoint sits just behind the head, so a restart is a no-op catchup
	cp := hwmBlock()
	assert.LessOrEqual(t, cp, laterHeight)
	assert.Less(t, laterHeight-cp, c.catchupThreshold,
		"a restart from checkpoint %d with head %d would still need a catchup replay", cp, laterHeight)
}
