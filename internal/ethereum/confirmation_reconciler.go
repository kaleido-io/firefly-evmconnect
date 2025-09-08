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

import "github.com/hyperledger/firefly-transaction-manager/pkg/apitypes"

type ConfirmationMapUpdateResult struct {
	*ConfirmationMap
	HasNewFork              bool `json:"hasNewFork"`
	HasNewConfirmation      bool `json:"hasNewConfirmation"`
	Confirmed               bool `json:"confirmed"`
	TargetConfirmationCount int  `json:"targetConfirmationCount"` // the target number of confirmations for this event
}

type ConfirmationMap struct {
	// confirmation map is contains a list of possible confirmations for a transaction
	// the key is the hash of the first block that contains the transaction hash
	ConfirmationQueueMap map[string][]*apitypes.Confirmation `json:"confirmationQueueMap,omitempty"`
	// which block hash that leads a confirmation queue matches the canonical block hash
	CanonicalBlockHash string `json:"canonicalBlockHash,omitempty"`
}
