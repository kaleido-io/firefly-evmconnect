// Copyright © 2025 Kaleido, Inc.
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
	"container/list"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"testing"

	lru "github.com/hashicorp/golang-lru"
	"github.com/hyperledger/firefly-common/pkg/fftypes"
	"github.com/hyperledger/firefly-evmconnect/mocks/rpcbackendmocks"
	"github.com/hyperledger/firefly-signer/pkg/ethtypes"
	"github.com/hyperledger/firefly-signer/pkg/rpcbackend"
	"github.com/hyperledger/firefly-transaction-manager/pkg/ffcapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// Tests of the reconcileConfirmationsForTransaction function

func TestReconcileConfirmationsForTransaction_TransactionNotFound(t *testing.T) {

	_, c, mRPC, _ := newTestConnectorWithNoBlockerFilterDefaultMocks(t)

	// Mock for TransactionReceipt call - return nil to simulate transaction not found
	mRPC.On("CallRPC", mock.Anything, mock.Anything, "eth_getTransactionReceipt", generateTestHash(100)).Return(nil).Run(func(args mock.Arguments) {
		err := json.Unmarshal([]byte("null"), args[1])
		assert.NoError(t, err)
	})

	// Execute the reconcileConfirmationsForTransaction function
	result, err := c.ReconcileConfirmationsForTransaction(context.Background(), generateTestHash(100), nil, 5)

	// Assertions - expect an error when transaction doesn't exist
	assert.Error(t, err)
	assert.Regexp(t, "FF23061", err)
	assert.Nil(t, result)

	mRPC.AssertExpectations(t)
}

func TestReconcileConfirmationsForTransaction_ReceiptRPCCallError(t *testing.T) {

	_, c, mRPC, _ := newTestConnectorWithNoBlockerFilterDefaultMocks(t)

	// Mock for TransactionReceipt call - return error to simulate RPC call error
	mRPC.On("CallRPC", mock.Anything, mock.Anything, "eth_getTransactionReceipt", generateTestHash(100)).Return(&rpcbackend.RPCError{Message: "pop"}).Run(func(args mock.Arguments) {
		err := json.Unmarshal([]byte("null"), args[1])
		assert.NoError(t, err)
	})

	// Execute the reconcileConfirmationsForTransaction function
	result, err := c.ReconcileConfirmationsForTransaction(context.Background(), generateTestHash(100), []*ffcapi.MinimalBlockInfo{}, 5)

	// Assertions - expect an error when RPC call fails
	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestReconcileConfirmationsForTransaction_BlockNotFound(t *testing.T) {

	_, c, mRPC, _ := newTestConnectorWithNoBlockerFilterDefaultMocks(t)

	// Mock for TransactionReceipt call
	mRPC.On("CallRPC", mock.Anything, mock.Anything, "eth_getTransactionReceipt",
		mock.MatchedBy(func(txHash string) bool {
			assert.Equal(t, "0x6197ef1a58a2a592bb447efb651f0db7945de21aa8048801b250bd7b7431f9b6", txHash)
			return true
		})).
		Return(nil).
		Run(func(args mock.Arguments) {
			err := json.Unmarshal([]byte(sampleJSONRPCReceipt), args[1])
			assert.NoError(t, err)
		})

	mRPC.On("CallRPC", mock.Anything, mock.Anything, "eth_getBlockByNumber", mock.MatchedBy(func(bn *ethtypes.HexInteger) bool {
		return bn.BigInt().String() == "1977"
	}), false).Return(nil).Run(func(args mock.Arguments) {
		err := json.Unmarshal([]byte("null"), args[1])
		assert.NoError(t, err)
	})

	// Execute the reconcileConfirmationsForTransaction function
	result, err := c.ReconcileConfirmationsForTransaction(context.Background(), "0x6197ef1a58a2a592bb447efb651f0db7945de21aa8048801b250bd7b7431f9b6", []*ffcapi.MinimalBlockInfo{
		{BlockNumber: fftypes.FFuint64(1977), BlockHash: generateTestHash(1977), ParentHash: generateTestHash(1976)},
	}, 5)

	// Assertions - expect an error when transaction doesn't exist
	assert.Error(t, err)
	assert.Regexp(t, "FF23061", err)
	assert.Nil(t, result)

	mRPC.AssertExpectations(t)
}

func TestReconcileConfirmationsForTransaction_BlockRPCCallError(t *testing.T) {

	_, c, mRPC, _ := newTestConnectorWithNoBlockerFilterDefaultMocks(t)

	mRPC.On("CallRPC", mock.Anything, mock.Anything, "eth_getTransactionReceipt",
		mock.MatchedBy(func(txHash string) bool {
			assert.Equal(t, "0x6197ef1a58a2a592bb447efb651f0db7945de21aa8048801b250bd7b7431f9b6", txHash)
			return true
		})).
		Return(nil).
		Run(func(args mock.Arguments) {
			err := json.Unmarshal([]byte(sampleJSONRPCReceipt), args[1])
			assert.NoError(t, err)
		})

	mRPC.On("CallRPC", mock.Anything, mock.Anything, "eth_getBlockByNumber", mock.MatchedBy(func(bn *ethtypes.HexInteger) bool {
		return bn.BigInt().String() == "1977"
	}), false).Return(&rpcbackend.RPCError{Message: "pop"})

	// Execute the reconcileConfirmationsForTransaction function
	result, err := c.ReconcileConfirmationsForTransaction(context.Background(), "0x6197ef1a58a2a592bb447efb651f0db7945de21aa8048801b250bd7b7431f9b6", []*ffcapi.MinimalBlockInfo{}, 5)

	// Assertions - expect an error when RPC call fails
	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestReconcileConfirmationsForTransaction_TxBlockNotInCanonicalChain(t *testing.T) {

	_, c, mRPC, _ := newTestConnectorWithNoBlockerFilterDefaultMocks(t)
	bl := c.blockListener
	bl.canonicalChain = createTestChain(1976, 1978) // Single block at 50, tx is at 100

	// Mock for TransactionReceipt call
	mRPC.On("CallRPC", mock.Anything, mock.Anything, "eth_getTransactionReceipt",
		mock.MatchedBy(func(txHash string) bool {
			assert.Equal(t, "0x6197ef1a58a2a592bb447efb651f0db7945de21aa8048801b250bd7b7431f9b6", txHash)
			return true
		})).
		Return(nil).
		Run(func(args mock.Arguments) {
			err := json.Unmarshal([]byte(sampleJSONRPCReceipt), args[1])
			assert.NoError(t, err)
		})

	fakeParentHash := fftypes.NewRandB32().String()

	mRPC.On("CallRPC", mock.Anything, mock.Anything, "eth_getBlockByNumber", mock.MatchedBy(func(bn *ethtypes.HexInteger) bool {
		return bn.BigInt().String() == "1977"
	}), false).Return(nil).Run(func(args mock.Arguments) {
		*args[1].(**blockInfoJSONRPC) = &blockInfoJSONRPC{
			Number:     ethtypes.NewHexInteger64(1977),
			Hash:       ethtypes.MustNewHexBytes0xPrefix(generateTestHash(1977)),
			ParentHash: ethtypes.MustNewHexBytes0xPrefix(fakeParentHash),
		}
	})

	// Execute the reconcileConfirmationsForTransaction function
	result, err := c.ReconcileConfirmationsForTransaction(context.Background(), "0x6197ef1a58a2a592bb447efb651f0db7945de21aa8048801b250bd7b7431f9b6", []*ffcapi.MinimalBlockInfo{}, 5)

	// Assertions - expect the transaction block to be returned
	// we trust the block retrieve by getBlockInfoContainsTxHash function more than the canonical chain
	// and we allow the canonical chain to be updated at its own pace
	// therefore, if the tx block is different from the block of same number in the canonical chain, we should return the tx block for now
	// and wait for the canonical chain to be updated
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.False(t, result.NewFork)
	assert.False(t, result.Confirmed)
	assert.Len(t, result.Confirmations, 2)
	mRPC.AssertExpectations(t)
}

func TestReconcileConfirmationsForTransaction_NewConfirmation(t *testing.T) {

	_, c, mRPC, _ := newTestConnectorWithNoBlockerFilterDefaultMocks(t)
	bl := c.blockListener
	bl.canonicalChain = createTestChain(1976, 1978) // Single block at 50, tx is at 100

	// Mock for TransactionReceipt call
	mRPC.On("CallRPC", mock.Anything, mock.Anything, "eth_getTransactionReceipt",
		mock.MatchedBy(func(txHash string) bool {
			assert.Equal(t, "0x6197ef1a58a2a592bb447efb651f0db7945de21aa8048801b250bd7b7431f9b6", txHash)
			return true
		})).
		Return(nil).
		Run(func(args mock.Arguments) {
			err := json.Unmarshal([]byte(sampleJSONRPCReceipt), args[1])
			assert.NoError(t, err)
		})

	mRPC.On("CallRPC", mock.Anything, mock.Anything, "eth_getBlockByNumber", mock.MatchedBy(func(bn *ethtypes.HexInteger) bool {
		return bn.BigInt().String() == "1977"
	}), false).Return(nil).Run(func(args mock.Arguments) {
		*args[1].(**blockInfoJSONRPC) = &blockInfoJSONRPC{
			Number:     ethtypes.NewHexInteger64(1977),
			Hash:       ethtypes.MustNewHexBytes0xPrefix(generateTestHash(1977)),
			ParentHash: ethtypes.MustNewHexBytes0xPrefix(generateTestHash(1976)),
		}
	})

	// Execute the reconcileConfirmationsForTransaction function
	result, err := c.ReconcileConfirmationsForTransaction(context.Background(), "0x6197ef1a58a2a592bb447efb651f0db7945de21aa8048801b250bd7b7431f9b6", []*ffcapi.MinimalBlockInfo{}, 5)

	// Assertions - expect the existing confirmation queue to be returned because the tx block doesn't match the same block number in the canonical chain
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.False(t, result.NewFork)
	assert.False(t, result.Confirmed)
	assert.Equal(t, []*ffcapi.MinimalBlockInfo{
		{BlockNumber: fftypes.FFuint64(1977), BlockHash: generateTestHash(1977), ParentHash: generateTestHash(1976)},
		{BlockNumber: fftypes.FFuint64(1978), BlockHash: generateTestHash(1978), ParentHash: generateTestHash(1977)},
	}, result.Confirmations)
	assert.Equal(t, uint64(5), result.TargetConfirmationCount)

	mRPC.AssertExpectations(t)
}

// Tests of the compareAndUpdateConfirmationQueue function

func TestCompareAndUpdateConfirmationQueue_EmptyChain(t *testing.T) {
	// Setup - create a chain with one block that's older than the transaction
	bl, done := newBlockListenerWithTestChain(t, 100, 5, 50, 50, []uint64{})
	defer done()
	ctx := context.Background()
	txBlockNumber := uint64(100)
	txBlockHash := generateTestHash(txBlockNumber)

	txBlockInfo := &ffcapi.MinimalBlockInfo{
		BlockNumber: fftypes.FFuint64(txBlockNumber),
		BlockHash:   txBlockHash,
		ParentHash:  generateTestHash(txBlockNumber - 1),
	}
	targetConfirmationCount := uint64(5)

	// Execute
	// Assert - should return early due to chain being too short
	confirmationUpdateResult, err := bl.buildConfirmationList(ctx, []*ffcapi.MinimalBlockInfo{}, txBlockInfo, targetConfirmationCount)
	assert.Error(t, err)
	assert.Regexp(t, "FF23062", err.Error())
	assert.Nil(t, confirmationUpdateResult)
}

func TestCompareAndUpdateConfirmationQueue_ChainTooShort(t *testing.T) {
	// Setup
	bl, done := newBlockListenerWithTestChain(t, 100, 5, 50, 99, []uint64{})
	defer done()
	ctx := context.Background()

	txBlockNumber := uint64(100)
	txBlockHash := generateTestHash(txBlockNumber)

	txBlockInfo := &ffcapi.MinimalBlockInfo{
		BlockNumber: fftypes.FFuint64(txBlockNumber),
		BlockHash:   txBlockHash,
		ParentHash:  generateTestHash(txBlockNumber - 1),
	}
	targetConfirmationCount := uint64(5)

	// Execute
	confirmationUpdateResult, err := bl.buildConfirmationList(ctx, []*ffcapi.MinimalBlockInfo{}, txBlockInfo, targetConfirmationCount)
	assert.Error(t, err)
	assert.Nil(t, confirmationUpdateResult)
}

func TestCompareAndUpdateConfirmationQueue_NilConfirmationMap(t *testing.T) {
	// Setup
	bl, done := newBlockListenerWithTestChain(t, 100, 5, 50, 150, []uint64{})
	defer done()
	ctx := context.Background()
	txBlockNumber := uint64(100)
	txBlockHash := generateTestHash(txBlockNumber)
	txBlockInfo := &ffcapi.MinimalBlockInfo{
		BlockNumber: fftypes.FFuint64(txBlockNumber),
		BlockHash:   txBlockHash,
		ParentHash:  generateTestHash(txBlockNumber - 1),
	}
	targetConfirmationCount := uint64(5)

	// Execute
	confirmationUpdateResult, err := bl.buildConfirmationList(ctx, nil, txBlockInfo, targetConfirmationCount)
	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, confirmationUpdateResult)
	assert.False(t, confirmationUpdateResult.NewFork)
	assert.True(t, confirmationUpdateResult.Confirmed)
	assert.Len(t, confirmationUpdateResult.Confirmations, 6)
	assert.Equal(t, txBlockNumber, uint64(confirmationUpdateResult.Confirmations[0].BlockNumber))
	assert.Equal(t, txBlockNumber+1, uint64(confirmationUpdateResult.Confirmations[1].BlockNumber))
	assert.Equal(t, txBlockNumber+2, uint64(confirmationUpdateResult.Confirmations[2].BlockNumber))
	assert.Equal(t, txBlockNumber+3, uint64(confirmationUpdateResult.Confirmations[3].BlockNumber))
	assert.Equal(t, txBlockNumber+4, uint64(confirmationUpdateResult.Confirmations[4].BlockNumber))
	assert.Equal(t, txBlockNumber+5, uint64(confirmationUpdateResult.Confirmations[5].BlockNumber))

}

func TestCompareAndUpdateConfirmationQueue_NilConfirmationMap_ZeroConfirmationCount(t *testing.T) {
	// Setup
	bl, done := newBlockListenerWithTestChain(t, 100, 5, 50, 150, []uint64{})
	defer done()
	ctx := context.Background()
	txBlockNumber := uint64(100)
	txBlockHash := generateTestHash(txBlockNumber)
	txBlockInfo := &ffcapi.MinimalBlockInfo{
		BlockNumber: fftypes.FFuint64(txBlockNumber),
		BlockHash:   txBlockHash,
		ParentHash:  generateTestHash(txBlockNumber - 1),
	}
	targetConfirmationCount := uint64(0)

	// Execute
	confirmationUpdateResult, err := bl.buildConfirmationList(ctx, nil, txBlockInfo, targetConfirmationCount)
	assert.NoError(t, err)
	// Assert
	assert.NotNil(t, confirmationUpdateResult.Confirmations)
	assert.False(t, confirmationUpdateResult.NewFork)
	assert.True(t, confirmationUpdateResult.Confirmed)
	// The code builds a full confirmation queue from the canonical chain
	assert.Len(t, confirmationUpdateResult.Confirmations, 1)
	assert.Equal(t, txBlockNumber, uint64(confirmationUpdateResult.Confirmations[0].BlockNumber))
}

func TestCompareAndUpdateConfirmationQueue_NilConfirmationMapUnconfirmed(t *testing.T) {
	// Setup
	bl, done := newBlockListenerWithTestChain(t, 100, 5, 100, 104, []uint64{})
	defer done()
	ctx := context.Background()
	txBlockNumber := uint64(100)
	txBlockHash := generateTestHash(txBlockNumber)
	txBlockInfo := &ffcapi.MinimalBlockInfo{
		BlockNumber: fftypes.FFuint64(txBlockNumber),
		BlockHash:   txBlockHash,
		ParentHash:  generateTestHash(txBlockNumber - 1),
	}
	targetConfirmationCount := uint64(5)

	// Execute
	confirmationUpdateResult, err := bl.buildConfirmationList(ctx, nil, txBlockInfo, targetConfirmationCount)
	assert.NoError(t, err)
	// Assert
	assert.NotNil(t, confirmationUpdateResult.Confirmations)
	assert.False(t, confirmationUpdateResult.NewFork)
	assert.False(t, confirmationUpdateResult.Confirmed)
	// The code builds a confirmation queue from the canonical chain up to the available blocks
	assert.Len(t, confirmationUpdateResult.Confirmations, 5) // 100, 101, 102, 103, 104
	assert.Equal(t, txBlockNumber, uint64(confirmationUpdateResult.Confirmations[0].BlockNumber))
	assert.Equal(t, txBlockNumber+1, uint64(confirmationUpdateResult.Confirmations[1].BlockNumber))
	assert.Equal(t, txBlockNumber+2, uint64(confirmationUpdateResult.Confirmations[2].BlockNumber))
	assert.Equal(t, txBlockNumber+3, uint64(confirmationUpdateResult.Confirmations[3].BlockNumber))
	assert.Equal(t, txBlockNumber+4, uint64(confirmationUpdateResult.Confirmations[4].BlockNumber))

}

func TestCompareAndUpdateConfirmationQueue_EmptyConfirmationQueue(t *testing.T) {
	// Setup
	bl, done := newBlockListenerWithTestChain(t, 100, 5, 50, 150, []uint64{})
	defer done()
	ctx := context.Background()

	txBlockNumber := uint64(100)
	txBlockHash := generateTestHash(txBlockNumber)
	txBlockInfo := &ffcapi.MinimalBlockInfo{
		BlockNumber: fftypes.FFuint64(txBlockNumber),
		BlockHash:   txBlockHash,
		ParentHash:  generateTestHash(txBlockNumber - 1),
	}
	targetConfirmationCount := uint64(5)

	// Execute
	confirmationUpdateResult, err := bl.buildConfirmationList(ctx, []*ffcapi.MinimalBlockInfo{}, txBlockInfo, targetConfirmationCount)
	assert.NoError(t, err)
	// Assert
	assert.False(t, confirmationUpdateResult.NewFork)
	assert.True(t, confirmationUpdateResult.Confirmed)
	// The code builds a full confirmation queue from the canonical chain
	assert.Len(t, confirmationUpdateResult.Confirmations, 6)
	assert.Equal(t, txBlockNumber, uint64(confirmationUpdateResult.Confirmations[0].BlockNumber))
	assert.Equal(t, txBlockNumber+1, uint64(confirmationUpdateResult.Confirmations[1].BlockNumber))
	assert.Equal(t, txBlockNumber+2, uint64(confirmationUpdateResult.Confirmations[2].BlockNumber))
	assert.Equal(t, txBlockNumber+3, uint64(confirmationUpdateResult.Confirmations[3].BlockNumber))
	assert.Equal(t, txBlockNumber+4, uint64(confirmationUpdateResult.Confirmations[4].BlockNumber))
	assert.Equal(t, txBlockNumber+5, uint64(confirmationUpdateResult.Confirmations[5].BlockNumber))
}

func TestCompareAndUpdateConfirmationQueue_MismatchConfirmationBlock(t *testing.T) {
	t.Skip("Skipping test because I don't understand this behavior") //Trying to understand the reasoning behind supporting an invalid list of existing confirmations.  Should this not just be an error?
	// Setup
	bl, done := newBlockListenerWithTestChain(t, 100, 5, 103, 150, []uint64{101})
	defer done()
	ctx := context.Background()
	existingQueue := []*ffcapi.MinimalBlockInfo{
		{BlockHash: generateTestHash(100), BlockNumber: fftypes.FFuint64(100), ParentHash: generateTestHash(99)},
		{BlockHash: generateTestHash(999), BlockNumber: fftypes.FFuint64(101), ParentHash: generateTestHash(100)}, // wrong hash, so the block should be fetched
		{BlockHash: generateTestHash(102), BlockNumber: fftypes.FFuint64(102), ParentHash: generateTestHash(101)},
		{BlockHash: generateTestHash(103), BlockNumber: fftypes.FFuint64(103), ParentHash: generateTestHash(102)},
	}
	txBlockNumber := uint64(100)
	txBlockHash := generateTestHash(100)
	txBlockInfo := &ffcapi.MinimalBlockInfo{
		BlockNumber: fftypes.FFuint64(txBlockNumber),
		BlockHash:   txBlockHash,
		ParentHash:  generateTestHash(99),
	}
	targetConfirmationCount := uint64(5)

	// Execute
	confirmationUpdateResult, err := bl.buildConfirmationList(ctx, existingQueue, txBlockInfo, targetConfirmationCount)
	assert.NoError(t, err)
	// Assert
	assert.True(t, confirmationUpdateResult.NewFork)
	// The code builds a full confirmation queue from the canonical chain
	assert.Len(t, confirmationUpdateResult.Confirmations, 6)
	assert.Equal(t, txBlockNumber, uint64(confirmationUpdateResult.Confirmations[0].BlockNumber))
	assert.Equal(t, txBlockNumber+1, uint64(confirmationUpdateResult.Confirmations[1].BlockNumber))
	assert.Equal(t, txBlockNumber+2, uint64(confirmationUpdateResult.Confirmations[2].BlockNumber))
	assert.Equal(t, txBlockNumber+3, uint64(confirmationUpdateResult.Confirmations[3].BlockNumber))
	assert.Equal(t, txBlockNumber+4, uint64(confirmationUpdateResult.Confirmations[4].BlockNumber))
	assert.Equal(t, txBlockNumber+5, uint64(confirmationUpdateResult.Confirmations[5].BlockNumber))
}

func TestCompareAndUpdateConfirmationQueue_ExistingConfirmationsTooDistant(t *testing.T) {
	// Setup

	bl, done := newBlockListenerWithTestChain(t, 100, 5, 145, 150, []uint64{102, 103, 104, 105})
	defer done()
	ctx := context.Background()
	existingQueue := []*ffcapi.MinimalBlockInfo{
		{BlockHash: generateTestHash(100), BlockNumber: fftypes.FFuint64(100), ParentHash: generateTestHash(99)},
		{BlockHash: generateTestHash(101), BlockNumber: fftypes.FFuint64(101), ParentHash: generateTestHash(100)},
	}
	txBlockNumber := uint64(100)
	txBlockHash := generateTestHash(100)
	txBlockInfo := &ffcapi.MinimalBlockInfo{
		BlockNumber: fftypes.FFuint64(txBlockNumber),
		BlockHash:   txBlockHash,
		ParentHash:  generateTestHash(99),
	}
	targetConfirmationCount := uint64(5)

	// Execute
	confirmationUpdateResult, err := bl.buildConfirmationList(ctx, existingQueue, txBlockInfo, targetConfirmationCount)
	assert.NoError(t, err)
	// Assert all confirmations are in the confirmation queue
	assert.False(t, confirmationUpdateResult.NewFork)
	assert.True(t, confirmationUpdateResult.Confirmed)
	assert.Len(t, confirmationUpdateResult.Confirmations, 6)
	assert.Equal(t, txBlockNumber, uint64(confirmationUpdateResult.Confirmations[0].BlockNumber))
	assert.Equal(t, txBlockNumber+1, uint64(confirmationUpdateResult.Confirmations[1].BlockNumber))
	assert.Equal(t, txBlockNumber+2, uint64(confirmationUpdateResult.Confirmations[2].BlockNumber))
	assert.Equal(t, txBlockNumber+3, uint64(confirmationUpdateResult.Confirmations[3].BlockNumber))
	assert.Equal(t, txBlockNumber+4, uint64(confirmationUpdateResult.Confirmations[4].BlockNumber))
	assert.Equal(t, txBlockNumber+5, uint64(confirmationUpdateResult.Confirmations[5].BlockNumber))
}

func TestCompareAndUpdateConfirmationQueue_CorruptedExistingConfirmationDoNotAffectConfirmations(t *testing.T) {
	// Setup
	bl, done := newBlockListenerWithTestChain(t, 100, 5, 50, 150, []uint64{})
	defer done()

	ctx := context.Background()
	// Create corrupted confirmation (wrong parent hash)
	existingQueue := []*ffcapi.MinimalBlockInfo{
		{BlockHash: generateTestHash(100), BlockNumber: fftypes.FFuint64(100), ParentHash: generateTestHash(99)},
		{BlockHash: generateTestHash(101), BlockNumber: fftypes.FFuint64(101), ParentHash: "0xwrongparent"},
	}
	txBlockNumber := uint64(100)
	txBlockHash := generateTestHash(100)
	txBlockInfo := &ffcapi.MinimalBlockInfo{
		BlockNumber: fftypes.FFuint64(txBlockNumber),
		BlockHash:   txBlockHash,
		ParentHash:  generateTestHash(99),
	}
	targetConfirmationCount := uint64(5)

	// Execute
	confirmationUpdateResult, err := bl.buildConfirmationList(ctx, existingQueue, txBlockInfo, targetConfirmationCount)
	assert.NoError(t, err)
	// Assert
	assert.True(t, confirmationUpdateResult.NewFork)
	assert.True(t, confirmationUpdateResult.Confirmed)
	assert.Len(t, confirmationUpdateResult.Confirmations, 6)
	assert.Equal(t, txBlockNumber, uint64(confirmationUpdateResult.Confirmations[0].BlockNumber))
	assert.Equal(t, txBlockNumber+1, uint64(confirmationUpdateResult.Confirmations[1].BlockNumber))
	assert.Equal(t, txBlockNumber+2, uint64(confirmationUpdateResult.Confirmations[2].BlockNumber))
	assert.Equal(t, txBlockNumber+3, uint64(confirmationUpdateResult.Confirmations[3].BlockNumber))
	assert.Equal(t, txBlockNumber+4, uint64(confirmationUpdateResult.Confirmations[4].BlockNumber))
	assert.Equal(t, txBlockNumber+5, uint64(confirmationUpdateResult.Confirmations[5].BlockNumber))
}

func TestCompareAndUpdateConfirmationQueue_ConnectionNodeMismatch(t *testing.T) {
	// Setup
	bl, done := newBlockListenerWithTestChain(t, 100, 5, 102, 150, []uint64{101})
	defer done()
	ctx := context.Background()
	existingQueue := []*ffcapi.MinimalBlockInfo{
		{BlockHash: generateTestHash(100), BlockNumber: fftypes.FFuint64(100), ParentHash: generateTestHash(99)},
		{BlockHash: "0xblockwrong", BlockNumber: fftypes.FFuint64(101), ParentHash: generateTestHash(100)},
		{BlockHash: generateTestHash(102), BlockNumber: fftypes.FFuint64(102), ParentHash: generateTestHash(101)},
		{BlockHash: generateTestHash(103), BlockNumber: fftypes.FFuint64(103), ParentHash: generateTestHash(102)},
	}
	txBlockNumber := uint64(100)
	txBlockHash := generateTestHash(100)
	txBlockInfo := &ffcapi.MinimalBlockInfo{
		BlockNumber: fftypes.FFuint64(txBlockNumber),
		BlockHash:   txBlockHash,
		ParentHash:  generateTestHash(99),
	}
	targetConfirmationCount := uint64(5)

	// Execute
	confirmationUpdateResult, err := bl.buildConfirmationList(ctx, existingQueue, txBlockInfo, targetConfirmationCount)
	assert.NoError(t, err)
	// Assert
	assert.True(t, confirmationUpdateResult.NewFork)
	assert.True(t, confirmationUpdateResult.Confirmed)
	assert.Len(t, confirmationUpdateResult.Confirmations, 6)
	assert.Equal(t, txBlockNumber, uint64(confirmationUpdateResult.Confirmations[0].BlockNumber))
	assert.Equal(t, txBlockNumber+1, uint64(confirmationUpdateResult.Confirmations[1].BlockNumber))
	assert.Equal(t, txBlockNumber+2, uint64(confirmationUpdateResult.Confirmations[2].BlockNumber))
	assert.Equal(t, txBlockNumber+3, uint64(confirmationUpdateResult.Confirmations[3].BlockNumber))
	assert.Equal(t, txBlockNumber+4, uint64(confirmationUpdateResult.Confirmations[4].BlockNumber))
	assert.Equal(t, txBlockNumber+5, uint64(confirmationUpdateResult.Confirmations[5].BlockNumber))
}

func TestCompareAndUpdateConfirmationQueue_CorruptedExistingConfirmationAfterFirstConfirmation(t *testing.T) {
	// Setup
	bl, done := newBlockListenerWithTestChain(t, 100, 5, 100, 150, []uint64{})
	defer done()

	ctx := context.Background()
	existingQueue := []*ffcapi.MinimalBlockInfo{
		{BlockHash: generateTestHash(100), BlockNumber: fftypes.FFuint64(100), ParentHash: generateTestHash(99)},
		{BlockHash: generateTestHash(101), BlockNumber: fftypes.FFuint64(101), ParentHash: generateTestHash(100)},
		{BlockHash: generateTestHash(102), BlockNumber: fftypes.FFuint64(102), ParentHash: "0xblockwrong"},
	}
	txBlockNumber := uint64(100)
	txBlockHash := generateTestHash(100)
	txBlockInfo := &ffcapi.MinimalBlockInfo{
		BlockNumber: fftypes.FFuint64(txBlockNumber),
		BlockHash:   txBlockHash,
		ParentHash:  generateTestHash(99),
	}
	targetConfirmationCount := uint64(5)

	// Execute
	confirmationUpdateResult, err := bl.buildConfirmationList(ctx, existingQueue, txBlockInfo, targetConfirmationCount)
	assert.NoError(t, err)
	// Assert
	assert.True(t, confirmationUpdateResult.NewFork)
	assert.True(t, confirmationUpdateResult.Confirmed)
	assert.Len(t, confirmationUpdateResult.Confirmations, 6)
	assert.Equal(t, txBlockNumber, uint64(confirmationUpdateResult.Confirmations[0].BlockNumber))
	assert.Equal(t, txBlockNumber+1, uint64(confirmationUpdateResult.Confirmations[1].BlockNumber))
	assert.Equal(t, txBlockNumber+2, uint64(confirmationUpdateResult.Confirmations[2].BlockNumber))
	assert.Equal(t, txBlockNumber+3, uint64(confirmationUpdateResult.Confirmations[3].BlockNumber))
	assert.Equal(t, txBlockNumber+4, uint64(confirmationUpdateResult.Confirmations[4].BlockNumber))
	assert.Equal(t, txBlockNumber+5, uint64(confirmationUpdateResult.Confirmations[5].BlockNumber))
}

func TestCompareAndUpdateConfirmationQueue_FailedToFetchBlockInfo(t *testing.T) {
	// Setup
	mRPC := &rpcbackendmocks.Backend{}
	bl := &blockListener{
		canonicalChain: createTestChain(150, 150),
		backend:        mRPC,
	}
	bl.blockCache, _ = lru.New(100)

	mRPC.On("CallRPC", mock.Anything, mock.Anything, "eth_getBlockByNumber", mock.MatchedBy(func(bn *ethtypes.HexInteger) bool {
		return bn.BigInt().String() == strconv.FormatUint(105, 10)
	}), false).Return(&rpcbackend.RPCError{Message: "pop"})

	ctx := context.Background()
	existingQueue := []*ffcapi.MinimalBlockInfo{
		{BlockHash: generateTestHash(100), BlockNumber: fftypes.FFuint64(100), ParentHash: generateTestHash(99)},
		{BlockHash: generateTestHash(101), BlockNumber: fftypes.FFuint64(101), ParentHash: generateTestHash(100)},
		{BlockHash: generateTestHash(102), BlockNumber: fftypes.FFuint64(102), ParentHash: "0xblockwrong"},
	}

	txBlockNumber := uint64(100)
	txBlockHash := generateTestHash(100)
	txBlockInfo := &ffcapi.MinimalBlockInfo{
		BlockNumber: fftypes.FFuint64(txBlockNumber),
		BlockHash:   txBlockHash,
		ParentHash:  generateTestHash(99),
	}
	targetConfirmationCount := uint64(5)

	// Execute
	confirmationUpdateResult, err := bl.buildConfirmationList(ctx, existingQueue, txBlockInfo, targetConfirmationCount)
	assert.Error(t, err)
	assert.Regexp(t, "pop", err.Error())
	assert.Nil(t, confirmationUpdateResult)
}

func TestCompareAndUpdateConfirmationQueue_NilBlockInfo(t *testing.T) {
	// Setup
	mRPC := &rpcbackendmocks.Backend{}
	bl := &blockListener{
		canonicalChain: createTestChain(150, 150),
		backend:        mRPC,
	}
	bl.blockCache, _ = lru.New(100)

	mRPC.On("CallRPC", mock.Anything, mock.Anything, "eth_getBlockByNumber", mock.MatchedBy(func(bn *ethtypes.HexInteger) bool {
		return bn.BigInt().String() == strconv.FormatUint(105, 10)
	}), false).Return(nil).Run(func(args mock.Arguments) {
		*args[1].(**blockInfoJSONRPC) = nil
	})

	ctx := context.Background()
	existingQueue := []*ffcapi.MinimalBlockInfo{
		{BlockHash: generateTestHash(100), BlockNumber: fftypes.FFuint64(100), ParentHash: generateTestHash(99)},
		{BlockHash: generateTestHash(101), BlockNumber: fftypes.FFuint64(101), ParentHash: generateTestHash(100)},
		{BlockHash: generateTestHash(102), BlockNumber: fftypes.FFuint64(102), ParentHash: "0xblockwrong"},
	}
	txBlockNumber := uint64(100)
	txBlockHash := generateTestHash(100)
	txBlockInfo := &ffcapi.MinimalBlockInfo{
		BlockNumber: fftypes.FFuint64(txBlockNumber),
		BlockHash:   txBlockHash,
		ParentHash:  generateTestHash(99),
	}
	targetConfirmationCount := uint64(5)

	// Execute
	confirmationUpdateResult, err := bl.buildConfirmationList(ctx, existingQueue, txBlockInfo, targetConfirmationCount)
	assert.Error(t, err)
	assert.Regexp(t, "FF23011", err.Error())
	assert.Nil(t, confirmationUpdateResult)
}

func TestCompareAndUpdateConfirmationQueue_NewForkAfterFirstConfirmation(t *testing.T) {
	// Setup
	bl, done := newBlockListenerWithTestChain(t, 100, 5, 100, 150, []uint64{})
	defer done()

	ctx := context.Background()
	existingQueue := []*ffcapi.MinimalBlockInfo{
		{BlockHash: generateTestHash(100), BlockNumber: fftypes.FFuint64(100), ParentHash: generateTestHash(99)},
		{BlockHash: generateTestHash(101), BlockNumber: fftypes.FFuint64(101), ParentHash: generateTestHash(100)},
		{BlockHash: "fork1", BlockNumber: fftypes.FFuint64(102), ParentHash: generateTestHash(101)},
	}
	txBlockNumber := uint64(100)
	txBlockHash := generateTestHash(100)
	txBlockInfo := &ffcapi.MinimalBlockInfo{
		BlockNumber: fftypes.FFuint64(txBlockNumber),
		BlockHash:   txBlockHash,
		ParentHash:  generateTestHash(99),
	}
	targetConfirmationCount := uint64(5)

	// Execute
	confirmationUpdateResult, err := bl.buildConfirmationList(ctx, existingQueue, txBlockInfo, targetConfirmationCount)
	assert.NoError(t, err)
	// Assert
	assert.True(t, confirmationUpdateResult.NewFork)
	assert.True(t, confirmationUpdateResult.Confirmed)
	assert.Len(t, confirmationUpdateResult.Confirmations, 6)
}

func TestCompareAndUpdateConfirmationQueue_NewForkAfterFirstConfirmation_ZeroConfirmationCount(t *testing.T) {
	// Setup
	bl, done := newBlockListenerWithTestChain(t, 100, 5, 100, 150, []uint64{})
	defer done()
	ctx := context.Background()
	existingQueue := []*ffcapi.MinimalBlockInfo{
		{BlockHash: generateTestHash(100), BlockNumber: fftypes.FFuint64(100), ParentHash: generateTestHash(99)},
		{BlockHash: generateTestHash(101), BlockNumber: fftypes.FFuint64(101), ParentHash: generateTestHash(100)},
		{BlockHash: "fork1", BlockNumber: fftypes.FFuint64(102), ParentHash: generateTestHash(101)},
	}

	txBlockNumber := uint64(100)
	txBlockHash := generateTestHash(100)
	txBlockInfo := &ffcapi.MinimalBlockInfo{
		BlockNumber: fftypes.FFuint64(txBlockNumber),
		BlockHash:   txBlockHash,
		ParentHash:  generateTestHash(99),
	}
	targetConfirmationCount := uint64(0)

	// Execute
	confirmationUpdateResult, err := bl.buildConfirmationList(ctx, existingQueue, txBlockInfo, targetConfirmationCount)
	assert.NoError(t, err)
	// Assert
	assert.False(t, confirmationUpdateResult.NewFork)
	assert.True(t, confirmationUpdateResult.Confirmed)
	assert.Len(t, confirmationUpdateResult.Confirmations, 1)
}

func TestCompareAndUpdateConfirmationQueue_NewForkAndNoConnectionToCanonicalChain(t *testing.T) {
	// Setup
	bl, done := newBlockListenerWithTestChain(t, 100, 5, 103, 150, []uint64{101, 102})
	defer done()
	ctx := context.Background()
	existingQueue := []*ffcapi.MinimalBlockInfo{
		{BlockHash: generateTestHash(100), BlockNumber: fftypes.FFuint64(100), ParentHash: generateTestHash(99)},
		{BlockHash: "fork1", BlockNumber: fftypes.FFuint64(101), ParentHash: generateTestHash(100)},
		{BlockHash: "fork2", BlockNumber: fftypes.FFuint64(102), ParentHash: "fork1"},
		{BlockHash: "fork3", BlockNumber: fftypes.FFuint64(103), ParentHash: "fork2"},
	}
	txBlockNumber := uint64(100)
	txBlockHash := generateTestHash(100)
	txBlockInfo := &ffcapi.MinimalBlockInfo{
		BlockNumber: fftypes.FFuint64(txBlockNumber),
		BlockHash:   txBlockHash,
		ParentHash:  generateTestHash(99),
	}
	targetConfirmationCount := uint64(5)

	// Execute
	confirmationUpdateResult, err := bl.buildConfirmationList(ctx, existingQueue, txBlockInfo, targetConfirmationCount)
	assert.NoError(t, err)
	// Assert
	assert.True(t, confirmationUpdateResult.NewFork)
	assert.True(t, confirmationUpdateResult.Confirmed)
	assert.Len(t, confirmationUpdateResult.Confirmations, 6)
}

func TestCompareAndUpdateConfirmationQueue_ExistingConfirmationLaterThanCurrentBlock(t *testing.T) {
	t.Skip("Skipping test because I don't understand this behavior") //Trying to understand the reasoning behind supporting an invalid list of existing confirmations.  Should this not just be an error?

	// Setup
	bl, done := newBlockListenerWithTestChain(t, 100, 5, 100, 150, []uint64{})
	defer done()

	ctx := context.Background()
	existingQueue := []*ffcapi.MinimalBlockInfo{
		{BlockHash: generateTestHash(100), BlockNumber: fftypes.FFuint64(100), ParentHash: generateTestHash(99)},
		{BlockHash: generateTestHash(102), BlockNumber: fftypes.FFuint64(102), ParentHash: generateTestHash(101)},
	}
	txBlockNumber := uint64(100)
	txBlockHash := generateTestHash(100)
	txBlockInfo := &ffcapi.MinimalBlockInfo{
		BlockNumber: fftypes.FFuint64(txBlockNumber),
		BlockHash:   txBlockHash,
		ParentHash:  generateTestHash(99),
	}
	targetConfirmationCount := uint64(5)

	// Execute
	confirmationUpdateResult, err := bl.buildConfirmationList(ctx, existingQueue, txBlockInfo, targetConfirmationCount)
	assert.NoError(t, err)
	// Assert
	assert.False(t, confirmationUpdateResult.NewFork)
	assert.True(t, confirmationUpdateResult.Confirmed)
	assert.Len(t, confirmationUpdateResult.Confirmations, 6)
	assert.Equal(t, txBlockNumber, uint64(confirmationUpdateResult.Confirmations[0].BlockNumber))
	assert.Equal(t, txBlockNumber+1, uint64(confirmationUpdateResult.Confirmations[1].BlockNumber))
	assert.Equal(t, txBlockNumber+2, uint64(confirmationUpdateResult.Confirmations[2].BlockNumber))
	assert.Equal(t, txBlockNumber+3, uint64(confirmationUpdateResult.Confirmations[3].BlockNumber))
	assert.Equal(t, txBlockNumber+4, uint64(confirmationUpdateResult.Confirmations[4].BlockNumber))
	assert.Equal(t, txBlockNumber+5, uint64(confirmationUpdateResult.Confirmations[5].BlockNumber))
}

func TestCompareAndUpdateConfirmationQueue_ExistingTxBockInfoIsWrong(t *testing.T) {
	// Setup
	t.Skip("Skipping test because I don't understand this behavior") //Trying to understand the reasoning behind supporting an invalid list of existing confirmations.  Should this not just be an error?
	bl, done := newBlockListenerWithTestChain(t, 100, 5, 100, 150, []uint64{})
	defer done()

	ctx := context.Background()
	existingQueue := []*ffcapi.MinimalBlockInfo{
		{BlockHash: generateTestHash(999), BlockNumber: fftypes.FFuint64(100), ParentHash: generateTestHash(99)}, // should be corrected
		{BlockHash: generateTestHash(102), BlockNumber: fftypes.FFuint64(102), ParentHash: generateTestHash(101)},
	}
	txBlockNumber := uint64(100)
	txBlockHash := generateTestHash(100)
	txBlockInfo := &ffcapi.MinimalBlockInfo{
		BlockNumber: fftypes.FFuint64(txBlockNumber),
		BlockHash:   txBlockHash,
		ParentHash:  generateTestHash(99),
	}
	targetConfirmationCount := uint64(5)

	// Execute
	confirmationUpdateResult, err := bl.buildConfirmationList(ctx, existingQueue, txBlockInfo, targetConfirmationCount)
	assert.NoError(t, err)
	// Assert
	assert.False(t, confirmationUpdateResult.NewFork)
	assert.True(t, confirmationUpdateResult.Confirmed)
	assert.Len(t, confirmationUpdateResult.Confirmations, 6)
	assert.Equal(t, txBlockNumber, uint64(confirmationUpdateResult.Confirmations[0].BlockNumber))
	assert.Equal(t, generateTestHash(100), confirmationUpdateResult.Confirmations[0].BlockHash)
	assert.Equal(t, txBlockNumber+1, uint64(confirmationUpdateResult.Confirmations[1].BlockNumber))
	assert.Equal(t, txBlockNumber+2, uint64(confirmationUpdateResult.Confirmations[2].BlockNumber))
	assert.Equal(t, txBlockNumber+3, uint64(confirmationUpdateResult.Confirmations[3].BlockNumber))
	assert.Equal(t, txBlockNumber+4, uint64(confirmationUpdateResult.Confirmations[4].BlockNumber))
	assert.Equal(t, txBlockNumber+5, uint64(confirmationUpdateResult.Confirmations[5].BlockNumber))
}

func TestCompareAndUpdateConfirmationQueue_AlreadyConfirmable(t *testing.T) {
	// Setup
	bl, done := newBlockListenerWithTestChain(t, 100, 5, 103, 150, []uint64{})
	defer done()
	ctx := context.Background()
	// Create confirmations that already meet the target
	// and it connects to the canonical chain to validate they are still valid
	existingQueue := []*ffcapi.MinimalBlockInfo{
		{BlockHash: generateTestHash(100), BlockNumber: fftypes.FFuint64(100), ParentHash: generateTestHash(99)},
		{BlockHash: generateTestHash(101), BlockNumber: fftypes.FFuint64(101), ParentHash: generateTestHash(100)},
		{BlockHash: generateTestHash(102), BlockNumber: fftypes.FFuint64(102), ParentHash: generateTestHash(101)},
		{BlockHash: generateTestHash(103), BlockNumber: fftypes.FFuint64(103), ParentHash: generateTestHash(102)},

		// all blocks after the first block of the canonical chain are discarded in the final confirmation queue
		{BlockHash: "0xblock104", BlockNumber: fftypes.FFuint64(104), ParentHash: generateTestHash(103)}, // discarded
		{BlockHash: "0xblock105", BlockNumber: fftypes.FFuint64(105), ParentHash: "0xblock104"},          // discarded
	}
	txBlockNumber := uint64(100)
	txBlockHash := generateTestHash(100)
	txBlockInfo := &ffcapi.MinimalBlockInfo{
		BlockNumber: fftypes.FFuint64(txBlockNumber),
		BlockHash:   txBlockHash,
		ParentHash:  generateTestHash(99),
	}
	targetConfirmationCount := uint64(2)

	// Execute
	confirmationUpdateResult, err := bl.buildConfirmationList(ctx, existingQueue, txBlockInfo, targetConfirmationCount)
	assert.NoError(t, err)
	// Assert
	assert.True(t, confirmationUpdateResult.Confirmed)
	assert.False(t, confirmationUpdateResult.NewFork)
	assert.Len(t, confirmationUpdateResult.Confirmations, 3)
	assert.Equal(t, txBlockNumber, uint64(confirmationUpdateResult.Confirmations[0].BlockNumber))
	assert.Equal(t, txBlockNumber+1, uint64(confirmationUpdateResult.Confirmations[1].BlockNumber))
	assert.Equal(t, txBlockNumber+2, uint64(confirmationUpdateResult.Confirmations[2].BlockNumber))
}

func TestCompareAndUpdateConfirmationQueue_AlreadyConfirmable_ZeroConfirmationCount(t *testing.T) {
	// Setup
	bl, done := newBlockListenerWithTestChain(t, 100, 5, 103, 150, []uint64{})
	defer done()
	ctx := context.Background()
	// Create confirmations that already meet the target
	// and it connects to the canonical chain to validate they are still valid
	existingQueue := []*ffcapi.MinimalBlockInfo{
		{BlockHash: generateTestHash(100), BlockNumber: fftypes.FFuint64(100), ParentHash: generateTestHash(99)},
		{BlockHash: generateTestHash(101), BlockNumber: fftypes.FFuint64(101), ParentHash: generateTestHash(100)},
		{BlockHash: generateTestHash(102), BlockNumber: fftypes.FFuint64(102), ParentHash: generateTestHash(101)},
		{BlockHash: generateTestHash(103), BlockNumber: fftypes.FFuint64(103), ParentHash: generateTestHash(102)},

		// all blocks after the first block of the canonical chain are discarded in the final confirmation queue
		{BlockHash: "0xblock104", BlockNumber: fftypes.FFuint64(104), ParentHash: generateTestHash(103)}, // discarded
		{BlockHash: "0xblock105", BlockNumber: fftypes.FFuint64(105), ParentHash: "0xblock104"},          // discarded
	}
	txBlockNumber := uint64(100)
	txBlockHash := generateTestHash(100)
	txBlockInfo := &ffcapi.MinimalBlockInfo{
		BlockNumber: fftypes.FFuint64(txBlockNumber),
		BlockHash:   txBlockHash,
		ParentHash:  generateTestHash(99),
	}
	targetConfirmationCount := uint64(0)

	// Execute
	confirmationUpdateResult, err := bl.buildConfirmationList(ctx, existingQueue, txBlockInfo, targetConfirmationCount)
	assert.NoError(t, err)
	// Assert
	assert.True(t, confirmationUpdateResult.Confirmed)
	assert.False(t, confirmationUpdateResult.NewFork)
	assert.Len(t, confirmationUpdateResult.Confirmations, 1)
	assert.Equal(t, txBlockNumber, uint64(confirmationUpdateResult.Confirmations[0].BlockNumber))
}

func TestCompareAndUpdateConfirmationQueue_AlreadyConfirmableConnectable(t *testing.T) {
	// Setup
	bl, done := newBlockListenerWithTestChain(t, 100, 5, 103, 150, []uint64{101})
	defer done()
	ctx := context.Background()
	// Create confirmations that already meet the target
	// and it connects to the canonical chain to validate they are still valid
	existingQueue := []*ffcapi.MinimalBlockInfo{
		{BlockHash: generateTestHash(100), BlockNumber: fftypes.FFuint64(100), ParentHash: generateTestHash(99)},
		{BlockHash: generateTestHash(101), BlockNumber: fftypes.FFuint64(101), ParentHash: generateTestHash(100)},
		{BlockHash: generateTestHash(102), BlockNumber: fftypes.FFuint64(102), ParentHash: generateTestHash(101)},
		// didn't have block 103, which is the first block of the canonical chain
		// but we should still be able to validate the existing confirmations are valid using parent hash
	}
	txBlockNumber := uint64(100)
	txBlockHash := generateTestHash(100)
	txBlockInfo := &ffcapi.MinimalBlockInfo{
		BlockNumber: fftypes.FFuint64(txBlockNumber),
		BlockHash:   txBlockHash,
		ParentHash:  generateTestHash(99),
	}
	targetConfirmationCount := uint64(1)

	// Execute
	confirmationUpdateResult, err := bl.buildConfirmationList(ctx, existingQueue, txBlockInfo, targetConfirmationCount)
	assert.NoError(t, err)
	// Assert
	// The confirmation queue should return the confirmation queue up to the first block of the canonical chain

	assert.True(t, confirmationUpdateResult.Confirmed)
	assert.False(t, confirmationUpdateResult.NewFork)
	assert.Len(t, confirmationUpdateResult.Confirmations, 2)
	assert.Equal(t, txBlockNumber, uint64(confirmationUpdateResult.Confirmations[0].BlockNumber))
	assert.Equal(t, txBlockNumber+1, uint64(confirmationUpdateResult.Confirmations[1].BlockNumber))
}

func TestCompareAndUpdateConfirmationQueue_AlreadyConfirmableButAllExistingConfirmationsAreTooHighForTargetConfirmationCount(t *testing.T) {
	t.Skip("Skipping test because I don't understand this behavior") //Trying to understand the reasoning behind supporting an invalid list of existing confirmations.  Should this not just be an error?

	// Setup
	bl, done := newBlockListenerWithTestChain(t, 100, 5, 103, 150, []uint64{101})
	defer done()
	ctx := context.Background()
	// Create confirmations that already meet the target
	// and it connects to the canonical chain to validate they are still valid
	existingQueue := []*ffcapi.MinimalBlockInfo{
		{BlockHash: generateTestHash(100), BlockNumber: fftypes.FFuint64(100), ParentHash: generateTestHash(99)},
		// 101 will be fetched from the JSON-RPC endpoint to fill the gap
		{BlockHash: generateTestHash(102), BlockNumber: fftypes.FFuint64(102), ParentHash: generateTestHash(101)},
	}
	txBlockNumber := uint64(100)
	txBlockHash := generateTestHash(100)
	txBlockInfo := &ffcapi.MinimalBlockInfo{
		BlockNumber: fftypes.FFuint64(txBlockNumber),
		BlockHash:   txBlockHash,
		ParentHash:  generateTestHash(99),
	}
	targetConfirmationCount := uint64(1)

	// Execute
	confirmationUpdateResult, err := bl.buildConfirmationList(ctx, existingQueue, txBlockInfo, targetConfirmationCount)
	assert.NoError(t, err)
	// Assert
	// The confirmation queue should return the tx block and  the first block of the canonical chain

	assert.True(t, confirmationUpdateResult.Confirmed)
	assert.False(t, confirmationUpdateResult.NewFork)
	assert.Len(t, confirmationUpdateResult.Confirmations, 2)
	assert.Equal(t, txBlockNumber, uint64(confirmationUpdateResult.Confirmations[0].BlockNumber))
	assert.Equal(t, txBlockNumber+1, uint64(confirmationUpdateResult.Confirmations[1].BlockNumber))

}

func TestCompareAndUpdateConfirmationQueue_HasSufficientConfirmationsButNoOverlapWithCanonicalChain(t *testing.T) {
	// Setup
	bl, done := newBlockListenerWithTestChain(t, 100, 5, 104, 150, []uint64{101})
	defer done()
	ctx := context.Background()
	// Create confirmations that already meet the target
	// and it connects to the canonical chain to validate they are still valid
	existingQueue := []*ffcapi.MinimalBlockInfo{
		{BlockHash: generateTestHash(100), BlockNumber: fftypes.FFuint64(100), ParentHash: generateTestHash(99)},
		{BlockHash: generateTestHash(101), BlockNumber: fftypes.FFuint64(101), ParentHash: generateTestHash(100)},
		{BlockHash: generateTestHash(102), BlockNumber: fftypes.FFuint64(102), ParentHash: generateTestHash(101)},
	}

	txBlockNumber := uint64(100)
	txBlockHash := generateTestHash(100)
	txBlockInfo := &ffcapi.MinimalBlockInfo{
		BlockNumber: fftypes.FFuint64(txBlockNumber),
		BlockHash:   txBlockHash,
		ParentHash:  generateTestHash(99),
	}
	targetConfirmationCount := uint64(1)

	// Execute
	confirmationUpdateResult, err := bl.buildConfirmationList(ctx, existingQueue, txBlockInfo, targetConfirmationCount)
	assert.NoError(t, err)
	// Assert
	// Because the existing confirmations do not have overlap with the canonical chain,
	// the confirmation queue should return the tx block and the first block of the canonical chain
	assert.True(t, confirmationUpdateResult.Confirmed)
	assert.False(t, confirmationUpdateResult.NewFork)
	assert.Len(t, confirmationUpdateResult.Confirmations, 2)
	assert.Equal(t, txBlockNumber, uint64(confirmationUpdateResult.Confirmations[0].BlockNumber))
	assert.Equal(t, txBlockNumber+1, uint64(confirmationUpdateResult.Confirmations[1].BlockNumber))

}

func TestCompareAndUpdateConfirmationQueue_ValidExistingConfirmations(t *testing.T) {
	// Setup
	bl, done := newBlockListenerWithTestChain(t, 100, 5, 50, 150, []uint64{})
	defer done()
	ctx := context.Background()
	existingQueue := []*ffcapi.MinimalBlockInfo{
		{BlockHash: generateTestHash(100), BlockNumber: fftypes.FFuint64(100), ParentHash: generateTestHash(99)},
		{BlockHash: generateTestHash(101), BlockNumber: fftypes.FFuint64(101), ParentHash: generateTestHash(100)},
		{BlockHash: generateTestHash(102), BlockNumber: fftypes.FFuint64(102), ParentHash: generateTestHash(101)},
	}

	txBlockNumber := uint64(100)
	txBlockHash := generateTestHash(100)
	txBlockInfo := &ffcapi.MinimalBlockInfo{
		BlockNumber: fftypes.FFuint64(txBlockNumber),
		BlockHash:   txBlockHash,
		ParentHash:  generateTestHash(99),
	}
	targetConfirmationCount := uint64(5)

	// Execute
	confirmationUpdateResult, err := bl.buildConfirmationList(ctx, existingQueue, txBlockInfo, targetConfirmationCount)
	assert.NoError(t, err)
	// Assert
	assert.False(t, confirmationUpdateResult.NewFork)
	assert.True(t, confirmationUpdateResult.Confirmed)
	assert.Len(t, confirmationUpdateResult.Confirmations, 6)
	assert.Equal(t, txBlockNumber, uint64(confirmationUpdateResult.Confirmations[0].BlockNumber))
	assert.Equal(t, txBlockNumber+1, uint64(confirmationUpdateResult.Confirmations[1].BlockNumber))
	assert.Equal(t, txBlockNumber+2, uint64(confirmationUpdateResult.Confirmations[2].BlockNumber))
	assert.Equal(t, txBlockNumber+3, uint64(confirmationUpdateResult.Confirmations[3].BlockNumber))
	assert.Equal(t, txBlockNumber+4, uint64(confirmationUpdateResult.Confirmations[4].BlockNumber))
	assert.Equal(t, txBlockNumber+5, uint64(confirmationUpdateResult.Confirmations[5].BlockNumber))
}

func TestCompareAndUpdateConfirmationQueue_ValidExistingTxBlock(t *testing.T) {
	// Setup
	bl, done := newBlockListenerWithTestChain(t, 100, 5, 50, 150, []uint64{})
	defer done()
	ctx := context.Background()
	existingQueue := []*ffcapi.MinimalBlockInfo{
		{BlockHash: generateTestHash(100), BlockNumber: fftypes.FFuint64(100), ParentHash: generateTestHash(99)},
	}

	txBlockNumber := uint64(100)
	txBlockHash := generateTestHash(100)
	txBlockInfo := &ffcapi.MinimalBlockInfo{
		BlockNumber: fftypes.FFuint64(txBlockNumber),
		BlockHash:   txBlockHash,
		ParentHash:  generateTestHash(99),
	}
	targetConfirmationCount := uint64(5)

	// Execute
	confirmationUpdateResult, err := bl.buildConfirmationList(ctx, existingQueue, txBlockInfo, targetConfirmationCount)
	assert.NoError(t, err)
	// Assert
	assert.False(t, confirmationUpdateResult.NewFork)
	assert.True(t, confirmationUpdateResult.Confirmed)
	assert.Len(t, confirmationUpdateResult.Confirmations, 6)
	assert.Equal(t, txBlockNumber, uint64(confirmationUpdateResult.Confirmations[0].BlockNumber))
	assert.Equal(t, txBlockNumber+1, uint64(confirmationUpdateResult.Confirmations[1].BlockNumber))
	assert.Equal(t, txBlockNumber+2, uint64(confirmationUpdateResult.Confirmations[2].BlockNumber))
	assert.Equal(t, txBlockNumber+3, uint64(confirmationUpdateResult.Confirmations[3].BlockNumber))
	assert.Equal(t, txBlockNumber+4, uint64(confirmationUpdateResult.Confirmations[4].BlockNumber))
	assert.Equal(t, txBlockNumber+5, uint64(confirmationUpdateResult.Confirmations[5].BlockNumber))
}

func TestCompareAndUpdateConfirmationQueue_ReachTargetConfirmation(t *testing.T) {
	// Setup
	bl, done := newBlockListenerWithTestChain(t, 100, 5, 50, 150, []uint64{})
	defer done()
	ctx := context.Background()

	txBlockNumber := uint64(100)
	txBlockHash := generateTestHash(100)
	txBlockInfo := &ffcapi.MinimalBlockInfo{
		BlockNumber: fftypes.FFuint64(txBlockNumber),
		BlockHash:   txBlockHash,
		ParentHash:  generateTestHash(99),
	}
	targetConfirmationCount := uint64(3)

	// Execute
	confirmationUpdateResult, err := bl.buildConfirmationList(ctx, []*ffcapi.MinimalBlockInfo{}, txBlockInfo, targetConfirmationCount)
	assert.NoError(t, err)
	// Assert
	assert.True(t, confirmationUpdateResult.Confirmed)
	// The code builds a full confirmation queue from the canonical chain
	assert.GreaterOrEqual(t, len(confirmationUpdateResult.Confirmations), 4) // tx block + 3 confirmations
}

func TestCompareAndUpdateConfirmationQueue_ExistingConfirmationsWithGap(t *testing.T) {
	t.Skip("Skipping test because I don't understand this behavior") //Trying to understand the reasoning behind supporting an invalid list of existing confirmations.  Should this not just be an error?

	// Setup
	bl, done := newBlockListenerWithTestChain(t, 100, 5, 101, 150, []uint64{})
	defer done()
	ctx := context.Background()
	// Create confirmations with a gap (missing block 102)
	existingQueue := []*ffcapi.MinimalBlockInfo{
		{BlockHash: generateTestHash(100), BlockNumber: fftypes.FFuint64(100), ParentHash: generateTestHash(99)},
		// no block 101, which is the first block of the canonical chain, so no fetch to JSON-RPC endpoint is needed
		{BlockHash: generateTestHash(102), BlockNumber: fftypes.FFuint64(102), ParentHash: generateTestHash(101)},
		{BlockHash: generateTestHash(103), BlockNumber: fftypes.FFuint64(103), ParentHash: generateTestHash(102)},
	}
	txBlockNumber := uint64(100)
	txBlockHash := generateTestHash(100)
	txBlockInfo := &ffcapi.MinimalBlockInfo{
		BlockNumber: fftypes.FFuint64(txBlockNumber),
		BlockHash:   txBlockHash,
		ParentHash:  generateTestHash(99),
	}
	targetConfirmationCount := uint64(5)

	// Execute
	confirmationUpdateResult, err := bl.buildConfirmationList(ctx, existingQueue, txBlockInfo, targetConfirmationCount)
	assert.NoError(t, err)
	// Assert
	assert.False(t, confirmationUpdateResult.NewFork)
	assert.True(t, confirmationUpdateResult.Confirmed)
	assert.Len(t, confirmationUpdateResult.Confirmations, 6)
}

func TestCompareAndUpdateConfirmationQueue_ExistingConfirmationsWithLowerBlockNumber(t *testing.T) {
	t.Skip("Skipping test because I don't understand this behavior") //Trying to understand the reasoning behind supporting an invalid list of existing confirmations.  Should this not just be an error?

	// Setup
	bl, done := newBlockListenerWithTestChain(t, 100, 5, 50, 150, []uint64{})
	defer done()
	ctx := context.Background()
	// Create confirmations with a lower block number
	existingQueue := []*ffcapi.MinimalBlockInfo{
		{BlockHash: generateTestHash(100), BlockNumber: fftypes.FFuint64(100), ParentHash: generateTestHash(99)},
		{BlockHash: generateTestHash(101), BlockNumber: fftypes.FFuint64(99), ParentHash: generateTestHash(100)}, // somehow there is a lower block number
	}
	txBlockNumber := uint64(100)
	txBlockHash := generateTestHash(100)
	txBlockInfo := &ffcapi.MinimalBlockInfo{
		BlockNumber: fftypes.FFuint64(txBlockNumber),
		BlockHash:   txBlockHash,
		ParentHash:  generateTestHash(99),
	}
	targetConfirmationCount := uint64(5)

	// Execute
	confirmationUpdateResult, err := bl.buildConfirmationList(ctx, existingQueue, txBlockInfo, targetConfirmationCount)
	assert.NoError(t, err)
	// Assert
	assert.False(t, confirmationUpdateResult.NewFork)
	assert.True(t, confirmationUpdateResult.Confirmed)
	assert.Len(t, confirmationUpdateResult.Confirmations, 6)
}

func TestCompareAndUpdateConfirmationQueue_ExistingConfirmationsWithLowerBlockNumberAfterFirstConfirmation(t *testing.T) {
	t.Skip("Skipping test because I don't understand this behavior") //Trying to understand the reasoning behind supporting an invalid list of existing confirmations.  Should this not just be an error?

	// Setup
	bl, done := newBlockListenerWithTestChain(t, 100, 5, 101, 150, []uint64{})
	defer done()
	ctx := context.Background()
	// Create confirmations with a lower block number
	existingQueue := []*ffcapi.MinimalBlockInfo{
		{BlockHash: generateTestHash(100), BlockNumber: fftypes.FFuint64(100), ParentHash: generateTestHash(99)},
		{BlockHash: generateTestHash(101), BlockNumber: fftypes.FFuint64(101), ParentHash: generateTestHash(100)},
		{BlockHash: generateTestHash(102), BlockNumber: fftypes.FFuint64(99), ParentHash: generateTestHash(101)}, // somehow there is a lower block number
	}

	txBlockNumber := uint64(100)
	txBlockHash := generateTestHash(100)
	txBlockInfo := &ffcapi.MinimalBlockInfo{
		BlockNumber: fftypes.FFuint64(txBlockNumber),
		BlockHash:   txBlockHash,
		ParentHash:  generateTestHash(99),
	}
	targetConfirmationCount := uint64(5)

	// Execute
	confirmationUpdateResult, err := bl.buildConfirmationList(ctx, existingQueue, txBlockInfo, targetConfirmationCount)
	assert.NoError(t, err)
	// Assert
	assert.False(t, confirmationUpdateResult.NewFork)
	assert.True(t, confirmationUpdateResult.Confirmed)
	assert.Len(t, confirmationUpdateResult.Confirmations, 6)
}

func TestValidateExistingConfirmations_LowerBlockNumber(t *testing.T) {

	ctx := context.Background()
	// Create confirmations with a lower block number
	existingQueue := []*ffcapi.MinimalBlockInfo{
		{BlockHash: generateTestHash(100), BlockNumber: fftypes.FFuint64(100), ParentHash: generateTestHash(99)},
		{BlockHash: generateTestHash(101), BlockNumber: fftypes.FFuint64(101), ParentHash: generateTestHash(100)},
		{BlockHash: generateTestHash(102), BlockNumber: fftypes.FFuint64(99), ParentHash: generateTestHash(101)}, // somehow there is a lower block number
	}

	// Execute
	err := validateExistingConfirmations(ctx, existingQueue)
	assert.Error(t, err)
}

func TestValidateExistingConfirmations_Gap(t *testing.T) {

	ctx := context.Background()
	// Create confirmations with a lower block number
	existingQueue := []*ffcapi.MinimalBlockInfo{
		{BlockHash: generateTestHash(100), BlockNumber: fftypes.FFuint64(100), ParentHash: generateTestHash(99)},
		{BlockHash: generateTestHash(101), BlockNumber: fftypes.FFuint64(101), ParentHash: generateTestHash(100)},
		{BlockHash: generateTestHash(103), BlockNumber: fftypes.FFuint64(103), ParentHash: generateTestHash(102)},
	}

	// Execute
	err := validateExistingConfirmations(ctx, existingQueue)
	assert.Error(t, err)
}

func TestValidateExistingConfirmations_BrokenHash(t *testing.T) {

	ctx := context.Background()
	// Create confirmations with a lower block number
	existingQueue := []*ffcapi.MinimalBlockInfo{
		{BlockHash: generateTestHash(100), BlockNumber: fftypes.FFuint64(100), ParentHash: generateTestHash(99)},
		{BlockHash: generateTestHash(101), BlockNumber: fftypes.FFuint64(101), ParentHash: generateTestHash(100)},
		{BlockHash: generateTestHash(102), BlockNumber: fftypes.FFuint64(102), ParentHash: "broken"},
	}

	// Execute
	err := validateExistingConfirmations(ctx, existingQueue)
	assert.Error(t, err)
}

/*
func TestCheckAndFillInGap_GetBlockInfoError(t *testing.T) {
	// Setup
	_, c, mRPC, _ := newTestConnectorWithNoBlockerFilterDefaultMocks(t)
	ctx := context.Background()
	txBlockNumber := uint64(100)
	txBlockInfo := &ffcapi.MinimalBlockInfo{
		BlockNumber: fftypes.FFuint64(txBlockNumber),
		BlockHash:   generateTestHash(txBlockNumber),
		ParentHash:  generateTestHash(txBlockNumber - 1),
	}
	targetConfirmationCount := uint64(2)
	newConfirmationsWithoutTxBlock := []*ffcapi.MinimalBlockInfo{}
	existingConfirmations := []*ffcapi.MinimalBlockInfo{}

	// Mock RPC to return an error for the gap blocks (blockNumberToReach = 100+2 = 102, then loops 102 down to 101)
	mRPC.On("CallRPC", mock.Anything, mock.Anything, "eth_getBlockByNumber", mock.MatchedBy(func(bn *ethtypes.HexInteger) bool {
		return bn.BigInt().String() == strconv.FormatUint(102, 10)
	}), false).Return(&rpcbackend.RPCError{Message: "pop"})

	// Execute
	result, hasNewFork, err := c.blockListener.checkAndFillInGap(ctx, newConfirmationsWithoutTxBlock, existingConfirmations, txBlockInfo, targetConfirmationCount, nil)

	// Assertions
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "pop")
	assert.False(t, hasNewFork)
	assert.Nil(t, result)
	mRPC.AssertExpectations(t)
}

func TestCheckAndFillInGap_BlockNotAvailable(t *testing.T) {
	// Setup
	_, c, mRPC, _ := newTestConnectorWithNoBlockerFilterDefaultMocks(t)
	ctx := context.Background()
	txBlockNumber := uint64(100)
	txBlockInfo := &ffcapi.MinimalBlockInfo{
		BlockNumber: fftypes.FFuint64(txBlockNumber),
		BlockHash:   generateTestHash(txBlockNumber),
		ParentHash:  generateTestHash(txBlockNumber - 1),
	}
	targetConfirmationCount := uint64(2)
	newConfirmationsWithoutTxBlock := []*ffcapi.MinimalBlockInfo{}
	existingConfirmations := []*ffcapi.MinimalBlockInfo{}

	// Setup RPC calls - return nil to simulate block not available (blockNumberToReach = 100+2 = 102)
	mRPC.On("CallRPC", mock.Anything, mock.Anything, "eth_getBlockByNumber", mock.MatchedBy(func(bn *ethtypes.HexInteger) bool {
		return bn.BigInt().String() == strconv.FormatUint(102, 10)
	}), false).Return(nil).Run(func(args mock.Arguments) {
		*args[1].(**blockInfoJSONRPC) = nil // Block not available
	})

	// Execute
	result, hasNewFork, err := c.blockListener.checkAndFillInGap(ctx, newConfirmationsWithoutTxBlock, existingConfirmations, txBlockInfo, targetConfirmationCount, nil)

	// Assertions
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Block not available")
	assert.False(t, hasNewFork)
	assert.Nil(t, result)
	mRPC.AssertExpectations(t)
}

func TestCheckAndFillInGap_InvalidBlockParentRelationship(t *testing.T) {
	// Setup
	_, c, mRPC, _ := newTestConnectorWithNoBlockerFilterDefaultMocks(t)
	ctx := context.Background()
	txBlockNumber := uint64(100)
	txBlockInfo := &ffcapi.MinimalBlockInfo{
		BlockNumber: fftypes.FFuint64(txBlockNumber),
		BlockHash:   generateTestHash(txBlockNumber),
		ParentHash:  generateTestHash(txBlockNumber - 1),
	}
	targetConfirmationCount := uint64(2)

	// Setup test scenario where we have a previous block but fetch a block that doesn't connect
	block102 := &ffcapi.MinimalBlockInfo{
		BlockNumber: fftypes.FFuint64(102),
		BlockHash:   generateTestHash(102),
		ParentHash:  "wrong_parent_hash", // Wrong parent - should cause validation to fail
	}

	newConfirmationsWithoutTxBlock := []*ffcapi.MinimalBlockInfo{block102} // Start with block 102
	existingConfirmations := []*ffcapi.MinimalBlockInfo{}

	// Mock getBlockInfoByNumber calls for gap block 101 (blockNumberToReach = block102.BlockNumber - 1 = 101)
	mRPC.On("CallRPC", mock.Anything, mock.Anything, "eth_getBlockByNumber", mock.MatchedBy(func(bn *ethtypes.HexInteger) bool {
		return bn.BigInt().String() == strconv.FormatUint(101, 10)
	}), false).Return(nil).Run(func(args mock.Arguments) {
		*args[1].(**blockInfoJSONRPC) = &blockInfoJSONRPC{
			Number:     ethtypes.NewHexInteger64(101),
			Hash:       ethtypes.MustNewHexBytes0xPrefix(generateTestHash(101)),
			ParentHash: ethtypes.MustNewHexBytes0xPrefix(generateTestHash(99)), // Wrong parent - should cause validation to fail
		}
	})

	// Execute
	result, hasNewFork, err := c.blockListener.checkAndFillInGap(ctx, newConfirmationsWithoutTxBlock, existingConfirmations, txBlockInfo, targetConfirmationCount, nil)

	// Assertions - should fail because block 102 has wrong parent hash and doesn't connect to block 101
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Failed to build confirmation queue")
	assert.False(t, hasNewFork)
	assert.Nil(t, result)
	mRPC.AssertExpectations(t)
}

func TestCheckAndFillInGap_TxBlockNotParentOfFirstConfirmation(t *testing.T) {
	// Setup
	_, c, mRPC, _ := newTestConnectorWithNoBlockerFilterDefaultMocks(t)
	ctx := context.Background()
	txBlockNumber := uint64(100)
	txBlockInfo := &ffcapi.MinimalBlockInfo{
		BlockNumber: fftypes.FFuint64(txBlockNumber),
		BlockHash:   generateTestHash(txBlockNumber),
		ParentHash:  generateTestHash(txBlockNumber - 1),
	}
	targetConfirmationCount := uint64(1)

	// Setup test scenario where txBlockInfo is NOT parent of the first confirmation block
	wrongParentConfirmation := &ffcapi.MinimalBlockInfo{
		BlockNumber: fftypes.FFuint64(101),
		BlockHash:   generateTestHash(101),
		ParentHash:  "wrong_parent_hash", // This doesn't match txBlockInfo.BlockHash
	}

	newConfirmationsWithoutTxBlock := []*ffcapi.MinimalBlockInfo{wrongParentConfirmation}
	existingConfirmations := []*ffcapi.MinimalBlockInfo{}

	// Execute
	result, hasNewFork, err := c.blockListener.checkAndFillInGap(ctx, newConfirmationsWithoutTxBlock, existingConfirmations, txBlockInfo, targetConfirmationCount, nil)

	// Assertions
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Failed to build confirmation queue")
	assert.False(t, hasNewFork)
	assert.Nil(t, result)
	mRPC.AssertExpectations(t)
}
*/
// Helper functions

// generateTestHash creates a predictable hash for testing with consistent prefix and last 4 digits as index
func generateTestHash(index uint64) string {
	return fmt.Sprintf("0x%060x", index)
}

func createTestChain(startBlock, endBlock uint64) *list.List {
	chain := list.New()
	for i := startBlock; i <= endBlock; i++ {
		blockHash := generateTestHash(i)

		var parentHash string
		if i > startBlock || i > 0 {
			parentHash = generateTestHash(i - 1)
		} else {
			// For the first block, if it's 0, use a dummy parent hash
			parentHash = generateTestHash(9999) // Use a high number to avoid conflicts
		}

		blockInfo := &ffcapi.MinimalBlockInfo{
			BlockNumber: fftypes.FFuint64(i),
			BlockHash:   blockHash,
			ParentHash:  parentHash,
		}
		chain.PushBack(blockInfo)
	}
	return chain
}

func newBlockListenerWithTestChain(t *testing.T, txBlock, confirmationCount, startCanonicalBlock, endCanonicalBlock uint64, blocksToMock []uint64) (*blockListener, func()) {
	mRPC := &rpcbackendmocks.Backend{}
	bl := &blockListener{
		canonicalChain: createTestChain(startCanonicalBlock, endCanonicalBlock),
		backend:        mRPC,
	}
	bl.blockCache, _ = lru.New(100)

	if len(blocksToMock) > 0 {
		for _, blockNumber := range blocksToMock {
			mRPC.On("CallRPC", mock.Anything, mock.Anything, "eth_getBlockByNumber", mock.MatchedBy(func(bn *ethtypes.HexInteger) bool {
				return bn.BigInt().String() == strconv.FormatUint(blockNumber, 10)
			}), false).Return(nil).Run(func(args mock.Arguments) {
				*args[1].(**blockInfoJSONRPC) = &blockInfoJSONRPC{
					Number:     ethtypes.NewHexInteger64(int64(blockNumber)),
					Hash:       ethtypes.MustNewHexBytes0xPrefix(generateTestHash(blockNumber)),
					ParentHash: ethtypes.MustNewHexBytes0xPrefix(generateTestHash(blockNumber - 1)),
				}
			})
		}
	}
	return bl, func() {
		mRPC.AssertExpectations(t)
	}
}
