// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package actions

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/hypersdk/chain"
	"github.com/ava-labs/hypersdk/codec"
	"github.com/ava-labs/hypersdk/consts"
	"github.com/ava-labs/hypersdk/examples/tokenvm/storage"
	zutils "github.com/ava-labs/hypersdk/utils"

	"github.com/ava-labs/hypersdk/state"
)

var _ chain.Action = (*CreateNFT)(nil)

type CreateNFT struct {
	ID       []byte `json:"id"`
	Metadata []byte `json:"metadata"`
	Owner    []byte `json:"owner"`
	URL      []byte `json:"url"`
}

func (*CreateNFT) GetTypeID() uint8 {
	return createNFTID
}

func (*CreateNFT) StateKeys(_ chain.Auth, txID ids.ID) []string {
	return []string{
		string(storage.NFTKey(txID)),
	}
}

func (*CreateNFT) StateKeysMaxChunks() []uint16 {
	return []uint16{storage.NFTChunks}
}

func (*CreateNFT) OutputsWarpMessage() bool {
	return false
}

func (c *CreateNFT) Execute(
	ctx context.Context,
	_ chain.Rules,
	mu state.Mutable,
	_ int64,
	rauth chain.Auth,
	txID ids.ID,
	_ bool,
) (bool, uint64, []byte, *warp.UnsignedMessage, error) {

	if len(c.ID) == 0 {
		return false, CreateNFTComputeUnits, OutputSymbolEmpty, nil, nil
	}
	if len(c.Metadata) == 0 {
		return false, CreateNFTComputeUnits, OutputMetadataEmpty, nil, nil
	}

	// Generate a unique ID for the NFT (You may use a random or unique ID generation method)
	nftID := generateUniqueNFTID()

	if err := storage.SetNFT(ctx, mu, nftID, c.Metadata, c.Owner, string(c.URL)); err != nil {

		return false, CreateNFTComputeUnits, zutils.ErrBytes(err), nil, nil
	}

	return true, CreateNFTComputeUnits, nil, nil, nil
}

// Function to generate a unique NFT ID (Replace this with your ID generation logic)
func generateUniqueNFTID() ids.ID {
	// Implement your unique ID generation logic here
	// For simplicity, you can use a random ID for demonstration purposes
	return ids.GenerateTestID()
}

func (*CreateNFT) MaxComputeUnits(chain.Rules) uint64 {
	return CreateNFTComputeUnits
}

func (c *CreateNFT) Size() int {
	// TODO: add small bytes (smaller int prefix)
	return codec.BytesLen(c.ID) + consts.Uint8Len + codec.BytesLen(c.Metadata) + consts.Uint8Len + codec.BytesLen(c.Owner) + consts.Uint8Len + codec.BytesLen(c.URL)
}

func (c *CreateNFT) Marshal(p *codec.Packer) {
	p.PackBytes(c.ID)
	p.PackBytes(c.URL)
	p.PackBytes(c.Metadata)
	p.PackBytes(c.Owner)
}

func UnmarshalCreateNFT(p *codec.Packer, _ *warp.Message) (chain.Action, error) {
	var create CreateNFT
	p.UnpackBytes(MaxNFTIDSize, true, &create.ID)
	p.UnpackBytes(MaxNFTURLSize, true, &create.URL)
	p.UnpackBytes(MaxOwnerSize, true, &create.Owner)
	p.UnpackBytes(MaxMetadataSize, true, &create.Metadata)
	return &create, p.Err()
}

func (*CreateNFT) ValidRange(chain.Rules) (int64, int64) {
	// Returning -1, -1 means that the action is always valid.
	return -1, -1
}
