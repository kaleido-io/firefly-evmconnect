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

package ethblocklistener

import (
	"context"
	"testing"

	"github.com/hyperledger-firefly/common/pkg/fftypes"
	"github.com/hyperledger-firefly/evmconnect/mocks/rpcbackendmocks"
	"github.com/hyperledger-firefly/signer/pkg/ethtypes"
	"github.com/hyperledger-firefly/transaction-manager/pkg/ffcapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// TestBlockListenerLightModeHighestBlockTracksHead reproduces the light-mode frozen head condition.
//
// In light mode the listener polls eth_blockNumber and dispatches the head to consumers
// (GetHeadBlockNumber). The event stream, however, derives its high-water mark, its
// catchup gap check and its eth_newFilter fromBlock from GetHighestBlock. If that value is
// only ever set once at startup, a long-running process ends up with an HWM (and therefore a
// persisted checkpoint and filter fromBlock) pinned near the height the process started at,
// however far the chain has moved on. That is what makes a restart replay the whole uptime,
// and what makes a filter rebuild after a log-filter loss issue a since-process-start query.
//
// The chain here advances by more than catchupThreshold (500) + checkpointBlockGap (50) so the
// two head values diverge by an amount the event stream would treat as "needs catchup" if it
// could see it. Both accessors must report the live head.
func TestBlockListenerLightModeHighestBlockTracksHead(t *testing.T) {
	const startHeight uint64 = 1000
	const laterHeight uint64 = startHeight + 700

	startLatch := newTestLatch()
	var bnCall int
	ctx, bl, _, done := newTestBlockListener(t, func(conf *BlockListenerConfig, mRPC *rpcbackendmocks.Backend, _ context.CancelFunc) {
		conf.BlockPollingInterval = shortDelay
		conf.ChainTrackingMode = ffcapi.ChainTrackingModeLight

		mRPC.On("CallRPC", mock.Anything, mock.Anything, "eth_blockNumber").Return(nil).Run(func(args mock.Arguments) {
			bnCall++
			v := startHeight // call 1: establishBlockHeightWithRetry; call 2: first refresh, dispatches startHeight
			if bnCall >= 3 {
				v = laterHeight // process has been up a while: chain has moved well past the start height
			}
			*args[1].(*ethtypes.HexInteger) = *ethtypes.NewHexIntegerU64(v)
		}).Maybe()

		mockNewBlockFilter(mRPC, testBlockFilterID1).Once()

		var getFilterCalls int
		mRPC.On("CallRPC", mock.Anything, mock.Anything, "eth_getFilterChanges", testBlockFilterID1).Return(nil).Run(func(args mock.Arguments) {
			getFilterCalls++
			if getFilterCalls == 1 {
				startLatch.waitComplete()
			}
			*args[1].(*[]ethtypes.HexBytes0xPrefix) = nil
		}).Maybe()
	})
	defer done()

	updates := make(chan *ffcapi.BlockHashEvent, 16)
	bl.AddConsumer(ctx, &BlockUpdateConsumer{
		ID:      fftypes.NewUUID(),
		Ctx:     ctx,
		Updates: updates,
	})
	startLatch.complete()

	// Startup: both views agree on the start height
	ev1 := <-updates
	assert.Equal(t, startHeight, ev1.HeadBlockNumber)
	highest, ok := bl.GetHighestBlock(ctx)
	assert.True(t, ok)
	assert.Equal(t, startHeight, highest)

	// Chain advances: the head dispatched to confirmation consumers moves...
	ev2 := <-updates
	assert.Equal(t, laterHeight, ev2.HeadBlockNumber)
	assert.Equal(t, laterHeight, bl.GetHeadBlockNumber(ctx))

	// ...and the head the event stream reads for its HWM / catchup / filter fromBlock must move with it.
	highest, ok = bl.GetHighestBlock(ctx)
	assert.True(t, ok)
	assert.Equal(t, laterHeight, highest,
		"GetHighestBlock is frozen at the startup height (%d) while the chain head is %d: "+
			"event stream HWM, catchup gap check and eth_newFilter fromBlock are all pinned to process start",
		startHeight, laterHeight)
}
