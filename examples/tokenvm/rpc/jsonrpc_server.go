// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpc

import (
	"log"
	"net/http"
	"strings"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/boltdb/bolt"

	"github.com/ava-labs/hypersdk/chain"
	"github.com/ava-labs/hypersdk/examples/tokenvm/genesis"
	"github.com/ava-labs/hypersdk/examples/tokenvm/orderbook"
	"github.com/ava-labs/hypersdk/examples/tokenvm/utils"
)

type JSONRPCServer struct {
	c Controller
}

func NewJSONRPCServer(c Controller) *JSONRPCServer {
	return &JSONRPCServer{c}
}

type GenesisReply struct {
	Genesis *genesis.Genesis `json:"genesis"`
}

func (j *JSONRPCServer) Genesis(_ *http.Request, _ *struct{}, reply *GenesisReply) (err error) {
	reply.Genesis = j.c.Genesis()
	return nil
}

type TxArgs struct {
	TxID ids.ID `json:"txId"`
}

type TxReply struct {
	Timestamp int64            `json:"timestamp"`
	Success   bool             `json:"success"`
	Units     chain.Dimensions `json:"units"`
	Fee       uint64           `json:"fee"`
}

func (j *JSONRPCServer) Tx(req *http.Request, args *TxArgs, reply *TxReply) error {
	ctx, span := j.c.Tracer().Start(req.Context(), "Server.Tx")
	defer span.End()

	found, t, success, units, fee, err := j.c.GetTransaction(ctx, args.TxID)
	if err != nil {
		return err
	}
	if !found {
		return ErrTxNotFound
	}
	reply.Timestamp = t
	reply.Success = success
	reply.Units = units
	reply.Fee = fee
	return nil
}

type AssetArgs struct {
	Asset ids.ID `json:"asset"`
}

type AssetReply struct {
	Symbol   []byte `json:"symbol"`
	Decimals uint8  `json:"decimals"`
	Metadata []byte `json:"metadata"`
	Supply   uint64 `json:"supply"`
	Owner    string `json:"owner"`
	Warp     bool   `json:"warp"`
}

func (j *JSONRPCServer) Asset(req *http.Request, args *AssetArgs, reply *AssetReply) error {
	ctx, span := j.c.Tracer().Start(req.Context(), "Server.Asset")
	defer span.End()

	exists, symbol, decimals, metadata, supply, owner, warp, err := j.c.GetAssetFromState(ctx, args.Asset)
	if err != nil {
		return err
	}
	if !exists {
		return ErrAssetNotFound
	}
	reply.Symbol = symbol
	reply.Decimals = decimals
	reply.Metadata = metadata
	reply.Supply = supply
	reply.Owner = utils.Address(owner)
	reply.Warp = warp
	return err
}

type BalanceArgs struct {
	Address string `json:"address"`
	Asset   ids.ID `json:"asset"`
}

type BalanceReply struct {
	Amount uint64 `json:"amount"`
}

func (j *JSONRPCServer) Balance(req *http.Request, args *BalanceArgs, reply *BalanceReply) error {
	ctx, span := j.c.Tracer().Start(req.Context(), "Server.Balance")
	defer span.End()

	addr, err := utils.ParseAddress(args.Address)
	if err != nil {
		return err
	}
	balance, err := j.c.GetBalanceFromState(ctx, addr, args.Asset)
	if err != nil {
		return err
	}
	reply.Amount = balance
	return err
}

type OrdersArgs struct {
	Pair string `json:"pair"`
}

type OrdersReply struct {
	Orders []*orderbook.Order `json:"orders"`
}

func (j *JSONRPCServer) Orders(req *http.Request, args *OrdersArgs, reply *OrdersReply) error {
	_, span := j.c.Tracer().Start(req.Context(), "Server.Orders")
	defer span.End()

	reply.Orders = j.c.Orders(args.Pair, ordersToSend)
	return nil
}

type GetOrderArgs struct {
	OrderID ids.ID `json:"orderID"`
}

type GetOrderReply struct {
	Order *orderbook.Order `json:"order"`
}

func (j *JSONRPCServer) GetOrder(req *http.Request, args *GetOrderArgs, reply *GetOrderReply) error {
	ctx, span := j.c.Tracer().Start(req.Context(), "Server.GetOrder")
	defer span.End()

	exists, in, inTick, out, outTick, remaining, owner, err := j.c.GetOrderFromState(ctx, args.OrderID)
	if err != nil {
		return err
	}
	if !exists {
		return ErrOrderNotFound
	}
	reply.Order = &orderbook.Order{
		ID:        args.OrderID,
		Owner:     utils.Address(owner),
		InAsset:   in,
		InTick:    inTick,
		OutAsset:  out,
		OutTick:   outTick,
		Remaining: remaining,
	}
	return nil
}

type LoanArgs struct {
	Destination ids.ID `json:"destination"`
	Asset       ids.ID `json:"asset"`
}

type LoanReply struct {
	Amount uint64 `json:"amount"`
}

func (j *JSONRPCServer) Loan(req *http.Request, args *LoanArgs, reply *LoanReply) error {
	ctx, span := j.c.Tracer().Start(req.Context(), "Server.Loan")
	defer span.End()

	amount, err := j.c.GetLoanFromState(ctx, args.Asset, args.Destination)
	if err != nil {
		return err
	}
	reply.Amount = amount
	return nil
}

type MyNFTArgs struct {
	WalletAddress string `json:"address"`
	ID            ids.ID `json:"id"`
}

type NFTRef struct {
	TransactionHash []byte `json:"transactionHash"`
	ID              []byte `json:"id"`
	Metadata        []byte `json:"metadata"`
	Owner           []byte `json:"owner"`
	URL             []byte `json:"url"`
}

type MyNFTReply struct {
	Nfts []NFTRef `json:"nfts"`
}

func (j *JSONRPCServer) GetMyNFTs(req *http.Request, args *MyNFTArgs, reply *MyNFTReply) error {

	_, span := j.c.Tracer().Start(req.Context(), "Server.NFT")
	defer span.End()

	addr, err := utils.ParseAddress(args.WalletAddress)
	if err != nil {
		return err
	}

	db, err := bolt.Open("tokenvm.db", 0600, nil)

	if err != nil {
		log.Fatal(err)
	}

	var NFTs []NFTRef

	_err := db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte("nftbucket"))
		if bucket == nil {
			return nil // Bucket does not exist
		}

		c := bucket.Cursor()

		for k, v := c.First(); k != nil; k, v = c.Next() {
			// k is the key, v is the value

			nft_data := strings.Split(string(v), ",")

			if nft_data[2] == utils.Address(addr) {

				buffNFT := NFTRef{
					TransactionHash: k,
					ID:              []byte(nft_data[0]),
					Metadata:        []byte(nft_data[1]),
					Owner:           []byte(nft_data[2]),
					URL:             []byte(nft_data[3]),
				}

				NFTs = append(NFTs, buffNFT)
			}
		}

		return nil
	})

	if _err != nil {
		log.Fatal(_err)
	}

	reply.Nfts = NFTs
	return err
}
