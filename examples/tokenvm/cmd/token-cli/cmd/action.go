// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//nolint:lll
package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/hypersdk/chain"
	"github.com/ava-labs/hypersdk/consts"
	"github.com/ava-labs/hypersdk/examples/tokenvm/actions"
	frpc "github.com/ava-labs/hypersdk/examples/tokenvm/cmd/token-faucet/rpc"
	trpc "github.com/ava-labs/hypersdk/examples/tokenvm/rpc"
	"github.com/ava-labs/hypersdk/examples/tokenvm/storage"
	"github.com/ava-labs/hypersdk/examples/tokenvm/utils"
	"github.com/ava-labs/hypersdk/pubsub"
	"github.com/ava-labs/hypersdk/rpc"
	hutils "github.com/ava-labs/hypersdk/utils"
	"github.com/boltdb/bolt"
	"github.com/spf13/cobra"
)

var actionCmd = &cobra.Command{
	Use: "action",
	RunE: func(*cobra.Command, []string) error {
		return ErrMissingSubcommand
	},
}

var fundFaucetCmd = &cobra.Command{
	Use: "fund-faucet",
	RunE: func(*cobra.Command, []string) error {
		ctx := context.Background()

		// Get faucet
		faucetURI, err := handler.Root().PromptString("faucet URI", 0, consts.MaxInt)
		if err != nil {
			return err
		}
		fcli := frpc.NewJSONRPCClient(faucetURI)
		faucetAddress, err := fcli.FaucetAddress(ctx)
		if err != nil {
			return err
		}

		// Get clients
		_, priv, factory, cli, scli, tcli, err := handler.DefaultActor()
		if err != nil {
			return err
		}

		// Get balance
		_, decimals, balance, _, err := handler.GetAssetInfo(ctx, tcli, priv.PublicKey(), ids.Empty, true)
		if balance == 0 || err != nil {
			return err
		}

		// Select amount
		amount, err := handler.Root().PromptAmount("amount", decimals, balance, nil)
		if err != nil {
			return err
		}

		// Confirm action
		cont, err := handler.Root().PromptContinue()
		if !cont || err != nil {
			return err
		}

		// Generate transaction
		pk, err := utils.ParseAddress(faucetAddress)
		if err != nil {
			return err
		}
		if _, _, err = sendAndWait(ctx, nil, &actions.Transfer{
			To:    pk,
			Asset: ids.Empty,
			Value: amount,
		}, cli, scli, tcli, factory, true); err != nil {
			return err
		}
		hutils.Outf("{{green}}funded faucet:{{/}} %s\n", faucetAddress)
		return nil
	},
}

var transferCmd = &cobra.Command{
	Use: "transfer",
	RunE: func(*cobra.Command, []string) error {
		ctx := context.Background()
		_, priv, factory, cli, scli, tcli, err := handler.DefaultActor()
		if err != nil {
			return err
		}

		// Select token to send
		assetID, err := handler.Root().PromptAsset("assetID", true)
		if err != nil {
			return err
		}
		_, decimals, balance, _, err := handler.GetAssetInfo(ctx, tcli, priv.PublicKey(), assetID, true)
		if balance == 0 || err != nil {
			return err
		}

		// Select recipient
		recipient, err := handler.Root().PromptString("recipient", 1, 64)

		if err != nil {
			return err
		}

		db, err := bolt.Open("tokenvm.db", 0600, nil)

		if err != nil {
			log.Fatal(err)
		}

		if len(recipient) != 64 {
			var alias_addr, _ = ResolveAlias(db, recipient)

			if alias_addr == "" {
				log.Fatal("alias not found")
			}

			recipient = alias_addr

		}

		var recipient_conv, _ = utils.ParseAddress(recipient)

		// Select amount
		amount, err := handler.Root().PromptAmount("amount", decimals, balance, nil)
		if err != nil {
			return err
		}

		// Confirm action
		cont, err := handler.Root().PromptContinue()
		if !cont || err != nil {
			return err
		}

		// Generate transaction
		_, _, err = sendAndWait(ctx, nil, &actions.Transfer{
			To:    recipient_conv,
			Asset: assetID,
			Value: amount,
		}, cli, scli, tcli, factory, true)
		return err
	},
}

var createAssetCmd = &cobra.Command{
	Use: "create-asset",
	RunE: func(*cobra.Command, []string) error {
		ctx := context.Background()
		_, _, factory, cli, scli, tcli, err := handler.DefaultActor()
		if err != nil {
			return err
		}

		// Add symbol to token
		symbol, err := handler.Root().PromptString("symbol", 1, actions.MaxSymbolSize)
		if err != nil {
			return err
		}

		// Add decimal to token
		decimals, err := handler.Root().PromptInt("decimals", actions.MaxDecimals)
		if err != nil {
			return err
		}

		// Add metadata to token
		metadata, err := handler.Root().PromptString("metadata", 1, actions.MaxMetadataSize)
		if err != nil {
			return err
		}

		// Confirm action
		cont, err := handler.Root().PromptContinue()
		if !cont || err != nil {
			return err
		}

		// Generate transaction
		_, _, err = sendAndWait(ctx, nil, &actions.CreateAsset{
			Symbol:   []byte(symbol),
			Decimals: uint8(decimals), // already constrain above to prevent overflow
			Metadata: []byte(metadata),
		}, cli, scli, tcli, factory, true)
		return err
	},
}

var mintAssetCmd = &cobra.Command{
	Use: "mint-asset",
	RunE: func(*cobra.Command, []string) error {
		ctx := context.Background()
		_, priv, factory, cli, scli, tcli, err := handler.DefaultActor()
		if err != nil {
			return err
		}

		// Select token to mint
		assetID, err := handler.Root().PromptAsset("assetID", false)
		if err != nil {
			return err
		}
		exists, symbol, decimals, metadata, supply, owner, warp, err := tcli.Asset(ctx, assetID, false)
		if err != nil {
			return err
		}
		if !exists {
			hutils.Outf("{{red}}%s does not exist{{/}}\n", assetID)
			hutils.Outf("{{red}}exiting...{{/}}\n")
			return nil
		}
		if warp {
			hutils.Outf("{{red}}cannot mint a warped asset{{/}}\n", assetID)
			hutils.Outf("{{red}}exiting...{{/}}\n")
			return nil
		}

		if owner != utils.Address(priv.PublicKey()) {
			hutils.Outf("{{red}}%s is the owner of %s, you are not{{/}}\n", owner, assetID)
			hutils.Outf("{{red}}exiting...{{/}}\n")
			return nil
		}
		hutils.Outf(
			"{{yellow}}symbol:{{/}} %s {{yellow}}decimals:{{/}} %s {{yellow}}metadata:{{/}} %s {{yellow}}supply:{{/}} %d\n",
			string(symbol),
			decimals,
			string(metadata),
			supply,
		)

		// Select recipient
		recipient, err := handler.Root().PromptString("recipient", 1, 64)
		if err != nil {
			return err
		}

		db, err := bolt.Open("tokenvm.db", 0600, nil)

		if err != nil {
			log.Fatal(err)
		}

		if len(recipient) != 64 {
			var alias_addr, _ = ResolveAlias(db, recipient)

			if alias_addr == "" {
				log.Fatal("alias not found")
			}

			recipient = alias_addr

		}

		var recipient_conv, _ = utils.ParseAddress(recipient)

		// Select amount
		amount, err := handler.Root().PromptAmount("amount", decimals, consts.MaxUint64-supply, nil)
		if err != nil {
			return err
		}

		// Confirm action
		cont, err := handler.Root().PromptContinue()
		if !cont || err != nil {
			return err
		}

		// Generate transaction
		_, _, err = sendAndWait(ctx, nil, &actions.MintAsset{
			Asset: assetID,
			To:    recipient_conv,
			Value: amount,
		}, cli, scli, tcli, factory, true)
		return err
	},
}

var closeOrderCmd = &cobra.Command{
	Use: "close-order",
	RunE: func(*cobra.Command, []string) error {
		ctx := context.Background()
		_, _, factory, cli, scli, tcli, err := handler.DefaultActor()
		if err != nil {
			return err
		}

		// Select inbound token
		orderID, err := handler.Root().PromptID("orderID")
		if err != nil {
			return err
		}

		// Select outbound token
		outAssetID, err := handler.Root().PromptAsset("out assetID", true)
		if err != nil {
			return err
		}

		// Confirm action
		cont, err := handler.Root().PromptContinue()
		if !cont || err != nil {
			return err
		}

		// Generate transaction
		_, _, err = sendAndWait(ctx, nil, &actions.CloseOrder{
			Order: orderID,
			Out:   outAssetID,
		}, cli, scli, tcli, factory, true)
		return err
	},
}

var createOrderCmd = &cobra.Command{
	Use: "create-order",
	RunE: func(*cobra.Command, []string) error {
		ctx := context.Background()
		_, priv, factory, cli, scli, tcli, err := handler.DefaultActor()
		if err != nil {
			return err
		}

		// Select inbound token
		inAssetID, err := handler.Root().PromptAsset("in assetID", true)
		if err != nil {
			return err
		}
		exists, symbol, decimals, metadata, supply, _, warp, err := tcli.Asset(ctx, inAssetID, false)
		if err != nil {
			return err
		}
		if inAssetID != ids.Empty {
			if !exists {
				hutils.Outf("{{red}}%s does not exist{{/}}\n", inAssetID)
				hutils.Outf("{{red}}exiting...{{/}}\n")
				return nil
			}
			hutils.Outf(
				"{{yellow}}symbol:{{/}} %s {{yellow}}decimals:{{/}} %d {{yellow}}metadata:{{/}} %s {{yellow}}supply:{{/}} %d {{yellow}}warp:{{/}} %t\n",
				string(symbol),
				decimals,
				string(metadata),
				supply,
				warp,
			)
		}

		// Select in tick
		inTick, err := handler.Root().PromptAmount("in tick", decimals, consts.MaxUint64, nil)
		if err != nil {
			return err
		}

		// Select outbound token
		outAssetID, err := handler.Root().PromptAsset("out assetID", true)
		if err != nil {
			return err
		}
		_, decimals, balance, _, err := handler.GetAssetInfo(ctx, tcli, priv.PublicKey(), outAssetID, true)
		if balance == 0 || err != nil {
			return err
		}

		// Select out tick
		outTick, err := handler.Root().PromptAmount("out tick", decimals, consts.MaxUint64, nil)
		if err != nil {
			return err
		}

		// Select supply
		supply, err = handler.Root().PromptAmount(
			"supply (must be multiple of out tick)",
			decimals,
			balance,
			func(input uint64) error {
				if input%outTick != 0 {
					return ErrNotMultiple
				}
				return nil
			},
		)
		if err != nil {
			return err
		}

		// Confirm action
		cont, err := handler.Root().PromptContinue()
		if !cont || err != nil {
			return err
		}

		// Generate transaction
		_, _, err = sendAndWait(ctx, nil, &actions.CreateOrder{
			In:      inAssetID,
			InTick:  inTick,
			Out:     outAssetID,
			OutTick: outTick,
			Supply:  supply,
		}, cli, scli, tcli, factory, true)
		return err
	},
}

var fillOrderCmd = &cobra.Command{
	Use: "fill-order",
	RunE: func(*cobra.Command, []string) error {
		ctx := context.Background()
		_, priv, factory, cli, scli, tcli, err := handler.DefaultActor()
		if err != nil {
			return err
		}

		// Select inbound token
		inAssetID, err := handler.Root().PromptAsset("in assetID", true)
		if err != nil {
			return err
		}
		inSymbol, inDecimals, balance, _, err := handler.GetAssetInfo(ctx, tcli, priv.PublicKey(), inAssetID, true)
		if balance == 0 || err != nil {
			return err
		}

		// Select outbound token
		outAssetID, err := handler.Root().PromptAsset("out assetID", true)
		if err != nil {
			return err
		}
		outSymbol, outDecimals, _, _, err := handler.GetAssetInfo(ctx, tcli, priv.PublicKey(), outAssetID, false)
		if err != nil {
			return err
		}

		// View orders
		orders, err := tcli.Orders(ctx, actions.PairID(inAssetID, outAssetID))
		if err != nil {
			return err
		}
		if len(orders) == 0 {
			hutils.Outf("{{red}}no available orders{{/}}\n")
			hutils.Outf("{{red}}exiting...{{/}}\n")
			return nil
		}
		hutils.Outf("{{cyan}}available orders:{{/}} %d\n", len(orders))
		max := 20
		if len(orders) < max {
			max = len(orders)
		}
		for i := 0; i < max; i++ {
			order := orders[i]
			hutils.Outf(
				"%d) {{cyan}}Rate(in/out):{{/}} %.4f {{cyan}}InTick:{{/}} %s %s {{cyan}}OutTick:{{/}} %s %s {{cyan}}Remaining:{{/}} %s %s\n", //nolint:lll
				i,
				float64(order.InTick)/float64(order.OutTick),
				hutils.FormatBalance(order.InTick, inDecimals),
				inSymbol,
				hutils.FormatBalance(order.OutTick, outDecimals),
				outSymbol,
				hutils.FormatBalance(order.Remaining, outDecimals),
				outSymbol,
			)
		}

		// Select order
		orderIndex, err := handler.Root().PromptChoice("select order", max)
		if err != nil {
			return err
		}
		order := orders[orderIndex]

		// Select input to trade
		value, err := handler.Root().PromptAmount(
			"value (must be multiple of in tick)",
			inDecimals,
			balance,
			func(input uint64) error {
				if input%order.InTick != 0 {
					return ErrNotMultiple
				}
				multiples := input / order.InTick
				requiredRemainder := order.OutTick * multiples
				if requiredRemainder > order.Remaining {
					return ErrInsufficientSupply
				}
				return nil
			},
		)
		if err != nil {
			return err
		}
		multiples := value / order.InTick
		outAmount := multiples * order.OutTick
		hutils.Outf(
			"{{orange}}in:{{/}} %s %s {{orange}}out:{{/}} %s %s\n",
			hutils.FormatBalance(value, inDecimals),
			inSymbol,
			hutils.FormatBalance(outAmount, outDecimals),
			outSymbol,
		)

		// Confirm action
		cont, err := handler.Root().PromptContinue()
		if !cont || err != nil {
			return err
		}

		owner, err := utils.ParseAddress(order.Owner)
		if err != nil {
			return err
		}
		_, _, err = sendAndWait(ctx, nil, &actions.FillOrder{
			Order: order.ID,
			Owner: owner,
			In:    inAssetID,
			Out:   outAssetID,
			Value: value,
		}, cli, scli, tcli, factory, true)
		return err
	},
}

func performImport(
	ctx context.Context,
	scli *rpc.JSONRPCClient,
	dcli *rpc.JSONRPCClient,
	dscli *rpc.WebSocketClient,
	dtcli *trpc.JSONRPCClient,
	exportTxID ids.ID,
	factory chain.AuthFactory,
) error {
	// Select TxID (if not provided)
	var err error
	if exportTxID == ids.Empty {
		exportTxID, err = handler.Root().PromptID("export txID")
		if err != nil {
			return err
		}
	}

	// Generate warp signature (as long as >= 80% stake)
	var (
		msg                     *warp.Message
		subnetWeight, sigWeight uint64
	)
	for ctx.Err() == nil {
		msg, subnetWeight, sigWeight, err = scli.GenerateAggregateWarpSignature(ctx, exportTxID)
		if sigWeight >= (subnetWeight*4)/5 && err == nil {
			break
		}
		if err == nil {
			hutils.Outf(
				"{{yellow}}waiting for signature weight:{{/}} %d {{yellow}}observed:{{/}} %d\n",
				subnetWeight,
				sigWeight,
			)
		} else {
			hutils.Outf("{{red}}encountered error:{{/}} %v\n", err)
		}
		cont, err := handler.Root().PromptBool("try again")
		if err != nil {
			return err
		}
		if !cont {
			hutils.Outf("{{red}}exiting...{{/}}\n")
			return nil
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	wt, err := actions.UnmarshalWarpTransfer(msg.UnsignedMessage.Payload)
	if err != nil {
		return err
	}
	outputAssetID := wt.Asset
	if !wt.Return {
		outputAssetID = actions.ImportedAssetID(wt.Asset, msg.SourceChainID)
	}
	hutils.Outf(
		"%s {{yellow}}to:{{/}} %s {{yellow}}source assetID:{{/}} %s {{yellow}}source symbol:{{/}} %s {{yellow}}output assetID:{{/}} %s {{yellow}}value:{{/}} %s {{yellow}}reward:{{/}} %s {{yellow}}return:{{/}} %t\n",
		hutils.ToID(
			msg.UnsignedMessage.Payload,
		),
		utils.Address(wt.To),
		wt.Asset,
		wt.Symbol,
		outputAssetID,
		hutils.FormatBalance(wt.Value, wt.Decimals),
		hutils.FormatBalance(wt.Reward, wt.Decimals),
		wt.Return,
	)
	if wt.SwapIn > 0 {
		_, outSymbol, outDecimals, _, _, _, _, err := dtcli.Asset(ctx, wt.AssetOut, false)
		if err != nil {
			return err
		}
		hutils.Outf(
			"{{yellow}}asset in:{{/}} %s {{yellow}}swap in:{{/}} %s {{yellow}}asset out:{{/}} %s {{yellow}}symbol out:{{/}} %s {{yellow}}swap out:{{/}} %s {{yellow}}swap expiry:{{/}} %d\n",
			outputAssetID,
			hutils.FormatBalance(wt.SwapIn, wt.Decimals),
			wt.AssetOut,
			outSymbol,
			hutils.FormatBalance(wt.SwapOut, outDecimals),
			wt.SwapExpiry,
		)
	}
	hutils.Outf(
		"{{yellow}}signature weight:{{/}} %d {{yellow}}total weight:{{/}} %d\n",
		sigWeight,
		subnetWeight,
	)

	// Select fill
	var fill bool
	if wt.SwapIn > 0 {
		fill, err = handler.Root().PromptBool("fill")
		if err != nil {
			return err
		}
	}
	if !fill && wt.SwapExpiry > time.Now().UnixMilli() {
		return ErrMustFill
	}

	// Generate transaction
	_, _, err = sendAndWait(ctx, msg, &actions.ImportAsset{
		Fill: fill,
	}, dcli, dscli, dtcli, factory, true)
	return err
}

var importAssetCmd = &cobra.Command{
	Use: "import-asset",
	RunE: func(*cobra.Command, []string) error {
		ctx := context.Background()
		currentChainID, _, factory, dcli, dscli, dtcli, err := handler.DefaultActor()
		if err != nil {
			return err
		}

		// Select source
		_, uris, err := handler.Root().PromptChain("sourceChainID", set.Of(currentChainID))
		if err != nil {
			return err
		}
		scli := rpc.NewJSONRPCClient(uris[0])

		// Perform import
		return performImport(ctx, scli, dcli, dscli, dtcli, ids.Empty, factory)
	},
}

var exportAssetCmd = &cobra.Command{
	Use: "export-asset",
	RunE: func(*cobra.Command, []string) error {
		ctx := context.Background()
		currentChainID, priv, factory, cli, scli, tcli, err := handler.DefaultActor()
		if err != nil {
			return err
		}

		// Select token to send
		assetID, err := handler.Root().PromptAsset("assetID", true)
		if err != nil {
			return err
		}
		_, decimals, balance, sourceChainID, err := handler.GetAssetInfo(ctx, tcli, priv.PublicKey(), assetID, true)
		if balance == 0 || err != nil {
			return err
		}

		// Select recipient
		recipient, err := handler.Root().PromptAddress("recipient")
		if err != nil {
			return err
		}

		// Select amount
		amount, err := handler.Root().PromptAmount("amount", decimals, balance, nil)
		if err != nil {
			return err
		}

		// Determine return
		var ret bool
		if sourceChainID != ids.Empty {
			ret = true
		}

		// Select reward
		reward, err := handler.Root().PromptAmount("reward", decimals, balance-amount, nil)
		if err != nil {
			return err
		}

		// Determine destination
		destination := sourceChainID
		if !ret {
			destination, _, err = handler.Root().PromptChain("destination", set.Of(currentChainID))
			if err != nil {
				return err
			}
		}

		// Determine if swap in
		swap, err := handler.Root().PromptBool("swap on import")
		if err != nil {
			return err
		}
		var (
			swapIn     uint64
			assetOut   ids.ID
			swapOut    uint64
			swapExpiry int64
		)
		if swap {
			swapIn, err = handler.Root().PromptAmount("swap in", decimals, amount, nil)
			if err != nil {
				return err
			}
			assetOut, err = handler.Root().PromptAsset("asset out (on destination)", true)
			if err != nil {
				return err
			}
			uris, err := handler.Root().GetChain(destination)
			if err != nil {
				return err
			}
			networkID, _, _, err := cli.Network(ctx)
			if err != nil {
				return err
			}
			dcli := trpc.NewJSONRPCClient(uris[0], networkID, destination)
			_, decimals, _, _, err := handler.GetAssetInfo(ctx, dcli, priv.PublicKey(), assetOut, false)
			if err != nil {
				return err
			}
			swapOut, err = handler.Root().PromptAmount(
				"swap out (on destination, no decimals)",
				decimals,
				consts.MaxUint64,
				nil,
			)
			if err != nil {
				return err
			}
			swapExpiry, err = handler.Root().PromptTime("swap expiry")
			if err != nil {
				return err
			}
		}

		// Confirm action
		cont, err := handler.Root().PromptContinue()
		if !cont || err != nil {
			return err
		}

		// Generate transaction
		success, txID, err := sendAndWait(ctx, nil, &actions.ExportAsset{
			To:          recipient,
			Asset:       assetID,
			Value:       amount,
			Return:      ret,
			Reward:      reward,
			SwapIn:      swapIn,
			AssetOut:    assetOut,
			SwapOut:     swapOut,
			SwapExpiry:  swapExpiry,
			Destination: destination,
		}, cli, scli, tcli, factory, true)
		if err != nil {
			return err
		}
		if !success {
			return errors.New("not successful")
		}

		// Perform import
		imp, err := handler.Root().PromptBool("perform import on destination")
		if err != nil {
			return err
		}
		if imp {
			uris, err := handler.Root().GetChain(destination)
			if err != nil {
				return err
			}
			networkID, _, _, err := cli.Network(ctx)
			if err != nil {
				return err
			}
			dscli, err := rpc.NewWebSocketClient(uris[0], rpc.DefaultHandshakeTimeout, pubsub.MaxPendingMessages, pubsub.MaxReadMessageSize)
			if err != nil {
				return err
			}
			if err := performImport(ctx, cli, rpc.NewJSONRPCClient(uris[0]), dscli, trpc.NewJSONRPCClient(uris[0], networkID, destination), txID, factory); err != nil {
				return err
			}
		}

		// Ask if user would like to switch to destination chain
		sw, err := handler.Root().PromptBool("switch default chain to destination")
		if err != nil {
			return err
		}
		if !sw {
			return nil
		}
		return handler.Root().StoreDefaultChain(destination)
	},
}

var createNFTCmd = &cobra.Command{
	Use: "create-nft",
	RunE: func(*cobra.Command, []string) error {
		ctx := context.Background()
		_, _, factory, cli, scli, tcli, err := handler.DefaultActor()
		if err != nil {
			return err
		}

		// Add symbol to token
		ID, err := handler.Root().PromptString("ID", 1, 256)
		if err != nil {
			return err
		}

		// Add decimal to token
		URL, err := handler.Root().PromptString("Asset Url", 1, 256)
		if err != nil {
			return err
		}

		// Add metadata to token
		metadata, err := handler.Root().PromptString("metadata", 1, actions.MaxMetadataSize)
		if err != nil {
			return err
		}

		Owner, err := handler.Root().PromptString("recipient", 1, 256)

		// Confirm action
		cont, err := handler.Root().PromptContinue()
		if !cont || err != nil {
			return err
		}

		nft := &actions.CreateNFT{
			ID:       []byte(ID),
			Metadata: []byte(metadata),
			Owner:    []byte(Owner),
			URL:      []byte(URL),
		}

		// Generate transaction
		_, _id, err := sendAndWait(ctx, nil, nft, cli, scli, tcli, factory, true)

		storage.StoreNFT(_id.String(), nft.ID, nft.Metadata, nft.Owner, nft.URL)

		return err
	},
}

type PinataResponse struct {
	IpfsHash string `json:"IpfsHash"`
}

func deployPinata(filePath, apiKey, secretApiKey string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	// Create a new buffer to store the multipart/form-data request body
	var requestBody bytes.Buffer
	writer := multipart.NewWriter(&requestBody)

	// Create a part for the file
	part, err := writer.CreateFormFile("file", filepath.Base(filePath))
	if err != nil {
		return "", err
	}

	// Copy the file content into the part
	_, err = io.Copy(part, file)
	if err != nil {
		return "", err
	}

	writer.Close()

	// Create the HTTP POST request
	req, err := http.NewRequest("POST", "https://api.pinata.cloud/pinning/pinFileToIPFS", &requestBody)
	if err != nil {
		return "", err
	}

	// Set content type header
	req.Header.Set("Content-Type", writer.FormDataContentType())

	// Set API key headers
	req.Header.Add("pinata_api_key", apiKey)
	req.Header.Add("pinata_secret_api_key", secretApiKey)

	// Perform the HTTP request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	// Check the response status code
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("API request failed with status code %d, %s", resp.StatusCode, body)
	}

	var pinataResponse PinataResponse
	if err := json.NewDecoder(resp.Body).Decode(&pinataResponse); err != nil {
		return "", err
	}

	imageUrl := "https://ipfs.io/ipfs/" + pinataResponse.IpfsHash

	return imageUrl, nil
}

var createAndStoreNFTCmd = &cobra.Command{
	Use: "create-store-nft",
	RunE: func(*cobra.Command, []string) error {

		ctx := context.Background()
		_, _, factory, cli, scli, tcli, err := handler.DefaultActor()
		if err != nil {
			return err
		}

		// Add symbol to token
		ID, err := handler.Root().PromptString("ID", 1, 256)
		if err != nil {
			return err
		}

		// Add decimal to token
		Path, err := handler.Root().PromptString("Asset Image Path", 1, 256)
		if err != nil {
			return err
		}

		URL, err := deployPinata(
			Path,
			"fc43a725fd778580045c",
			"37c52b3571d7df2c1326c1460a1b192c209a1fb212c6b1b96eb2626bb2076efe",
		)

		if err != nil {
			return err
		}

		// Add metadata to token
		metadata, err := handler.Root().PromptString("metadata", 1, actions.MaxMetadataSize)
		if err != nil {
			return err
		}

		Owner, err := handler.Root().PromptString("recipient", 1, 256)

		// Confirm action
		cont, err := handler.Root().PromptContinue()
		if !cont || err != nil {
			return err
		}

		nft := &actions.CreateNFT{
			ID:       []byte(ID),
			Metadata: []byte(metadata),
			Owner:    []byte(Owner),
			URL:      []byte(URL),
		}

		// Generate transaction
		_, _id, err := sendAndWait(ctx, nil, nft, cli, scli, tcli, factory, true)

		storage.StoreNFT(_id.String(), nft.ID, nft.Metadata, nft.Owner, nft.URL)

		return err
	},
}

var getNFTCmd = &cobra.Command{
	Use: "get-nft",
	RunE: func(*cobra.Command, []string) error {

		ctx := context.Background()
		_, _, factory, cli, scli, tcli, err := handler.DefaultActor()
		if err != nil {
			return err
		}

		// Add symbol to token
		NftId, err := handler.Root().PromptString("NFT ID", 1, 256)
		if err != nil {
			return err
		}

		getnft := actions.GetNFT{
			ID: []byte(NftId),
		}

		_, _, err = sendAndWait(ctx, nil, &getnft, cli, scli, tcli, factory, true)

		nft_data, _ := storage.RetriveNFT(NftId)

		hutils.Outf("{{green}}NFT MetaData:{{/}} %s\n", nft_data[1])
		hutils.Outf("{{green}}NFT Owner:{{/}} %s\n", nft_data[2])
		hutils.Outf("{{green}}NFT URL:{{/}} %s\n", nft_data[3])

		return nil
	},
}

var getMyNFTCmd = &cobra.Command{
	Use: "get-my-nft",
	RunE: func(*cobra.Command, []string) error {

		_, priv, _, _, _, _, err := handler.DefaultActor()
		if err != nil {
			return err
		}

		db, err := bolt.Open("tokenvm.db", 0600, nil)

		if err != nil {
			log.Fatal(err)
		}

		_err := db.View(func(tx *bolt.Tx) error {
			bucket := tx.Bucket([]byte("nftbucket"))
			if bucket == nil {
				return nil // Bucket does not exist
			}

			c := bucket.Cursor()

			for k, v := c.First(); k != nil; k, v = c.Next() {
				// k is the key, v is the value

				nft_data := strings.Split(string(v), ",")

				if nft_data[2] == utils.Address(priv.PublicKey()) {
					hutils.Outf("{{green}}Transaction Hash:{{/}} %s\n", k)
					hutils.Outf("{{green}}NFT MetaData:{{/}} %s\n", []byte(nft_data[1]))
					hutils.Outf("{{green}}NFT Owner:{{/}} %s\n", []byte(nft_data[2]))
					hutils.Outf("{{green}}NFT URL:{{/}} %s\n", []byte(nft_data[3]))
					hutils.Outf("--------------------------------------------------\n")

				}
			}

			return nil
		})

		if _err != nil {
			log.Fatal(_err)
		}

		return nil
	},
}

var zkTransactionCmd = &cobra.Command{
	Use: "zk-transaction",
	RunE: func(*cobra.Command, []string) error {
		ctx := context.Background()
		_, _, factory, cli, scli, tcli, err := handler.DefaultActor()
		if err != nil {
			return err
		}

		// Select token to send
		assetID, err := handler.Root().PromptAsset("assetID", true)
		if err != nil {
			return err
		}

		// Select recipient
		from, err := handler.Root().PromptAddress("your wallet address")

		if err != nil {
			return err
		}

		// Select recipient
		recipient, err := handler.Root().PromptAddress("recipient")

		if err != nil {
			return err
		}

		// Confirm action
		cont, err := handler.Root().PromptContinue()
		if !cont || err != nil {
			return err
		}

		// Generate transaction
		_, _, err = sendAndWait(ctx, nil, &actions.ZkTransaction{
			From:  from,
			To:    recipient,
			Asset: assetID,
		}, cli, scli, tcli, factory, true)
		return err
	},
}
