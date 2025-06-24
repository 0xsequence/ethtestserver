package main

import (
	"context"
	"fmt"
	"log/slog"
	"math/big"
	"time"

	"github.com/0xsequence/ethkit/ethartifact"
	"github.com/0xsequence/ethtestserver"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
)

var (
	ERC20TestTokenArtifact   = ethtestserver.ERC20TestTokenArtifact
	ERC721TestTokenArtifact  = ethtestserver.ERC721TestTokenArtifact
	ERC1155TestTokenArtifact = ethtestserver.ERC1155TestTokenArtifact
)

func main() {
	var (
		wallet0 = ethtestserver.NewSigner()
		wallet1 = ethtestserver.NewSigner()
		wallet2 = ethtestserver.NewSigner()
		wallet3 = ethtestserver.NewSigner()
		wallet4 = ethtestserver.NewSigner()
		wallet5 = ethtestserver.NewSigner()
		wallet6 = ethtestserver.NewSigner()
		wallet7 = ethtestserver.NewSigner()
		wallet8 = ethtestserver.NewSigner()
		wallet9 = ethtestserver.NewSigner()

		initialBalance = big.NewInt(1e18)
	)

	config := &ethtestserver.ETHTestServerConfig{
		AutoMining: true,
		MineRate:   time.Millisecond * 1000,
		HTTPHost:   "localhost",
		HTTPPort:   19997,
		InitialSigners: map[common.Address]*big.Int{
			wallet0.Address(): initialBalance,
			wallet1.Address(): initialBalance,
			wallet2.Address(): initialBalance,
			wallet3.Address(): initialBalance,
			wallet4.Address(): initialBalance,
			wallet5.Address(): initialBalance,
			wallet6.Address(): initialBalance,
			wallet7.Address(): initialBalance,
			wallet8.Address(): initialBalance,
			wallet9.Address(): initialBalance,
		},
		Artifacts: []ethartifact.Artifact{
			ERC20TestTokenArtifact,
			ERC721TestTokenArtifact,
			ERC1155TestTokenArtifact,
		},
	}

	monkeySigners := ethtestserver.GenSigners(500)

	// add monkey signers to the config
	for _, signer := range monkeySigners {
		config.InitialSigners[signer.Address()] = initialBalance
	}

	server, err := ethtestserver.NewETHTestServer(config)
	if err != nil {
		slog.Error("Failed to create test server", "error", err)
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = server.Run(ctx)
	if err != nil {
		slog.Error("Failed to run test server", "error", err)
		return
	}
	defer server.Stop(ctx)

	slog.Info("Test server started successfully", "endpoint", server.HTTPEndpoint())

	// run monkey transferers for native ETH
	monkeyTransferor, err := runMonkeyTransferors(ctx, server, monkeySigners[0:100])
	if err != nil {
		slog.Error("Failed to run monkey transferers", "error", err)
		return
	}
	defer monkeyTransferor.Stop(ctx)
	slog.Info("Monkey transferer started successfully", "signersCount", len(monkeySigners[0:100]))

	// run monkey transferers for ERC1155
	monkeyERC1155Transferor, err := runMonkeyERC1155Transferors(ctx, server, monkeySigners[101:200])
	if err != nil {
		slog.Error("Failed to run monkey ERC1155 transferers", "error", err)
		return
	}
	defer monkeyERC1155Transferor.Stop(ctx)
	slog.Info("Monkey ERC1155 transferer started successfully", "signersCount", len(monkeySigners[100:200]))

	time.Sleep(24 * time.Hour) // Keep the server running for 24 hours to allow for further testing
}

func runMonkeyTransferors(ctx context.Context, server *ethtestserver.ETHTestServer, monkeySigners []*ethtestserver.Signer) (*ethtestserver.MonkeyOperator, error) {
	slog.Info("Deploying ERC20 contract for monkey transfers")

	// Monkey transferer
	monkeyTransferor := ethtestserver.NewMonkeyDoer(
		func(ctx context.Context, op *ethtestserver.MonkeyOperator, gen *core.BlockGen) (*types.Transaction, error) {

			signers, err := op.PickSigners(2) // Pick 2 random signers for the transfer
			if err != nil {
				return nil, fmt.Errorf("failed to pick signers for monkey transfer: %w", err)
			}

			addr1 := signers[0].Address()
			addr2 := signers[1].Address()

			signer := types.HomesteadSigner{}
			tx, _ := types.SignTx(
				types.NewTransaction(
					gen.TxNonce(addr1),
					addr2,
					big.NewInt(1),
					params.TxGas,
					big.NewInt(params.InitialBaseFee),
					nil,
				),
				signer,
				signers[0].RawPrivateKey(),
			)

			return tx, nil
		},
	)

	// Random ETH transfers using monkey signers
	monkeyTransferOperator, err := ethtestserver.NewMonkeyOperator(&ethtestserver.MonkeyOperatorConfig{
		Signers: monkeySigners[:20],
	},
		monkeyTransferor,
		server,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create monkey transfer operator: %w", err)
	}

	err = monkeyTransferOperator.Run(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to run monkey transfer operator: %w", err)
	}

	return monkeyTransferOperator, nil
}

func runMonkeyERC1155Transferors(ctx context.Context, server *ethtestserver.ETHTestServer, monkeySigners []*ethtestserver.Signer) (*ethtestserver.MonkeyOperator, error) {
	slog.Info("Deploying ERC1155 contract for monkey transfers")

	contractDeployer := monkeySigners[0]

	erc1155Contract, err := server.DeployContract(
		ctx,
		contractDeployer,
		ERC1155TestTokenArtifact.ContractName,
		contractDeployer.Address(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to deploy ERC1155 contract: %w", err)
	}

	// prepare a pool of signers for minting
	senders := monkeySigners[1:10]
	tokenID := big.NewInt(1)

	for i := range senders {
		recipient := senders[i]

		err = server.ContractTransact(
			ctx,
			contractDeployer,
			erc1155Contract,
			"mint",
			recipient.Address(),
			tokenID,
			big.NewInt(100), // mint 100 tokens
			[]byte{},
		)
		if err != nil {
			return nil, fmt.Errorf("failed to mint ERC1155 tokens for recipient %s: %w", recipient.Address().Hex(), err)
		}
	}

	// print balances of the minted tokens for each signer
	{
		tokenBalances := make(map[common.Address]*big.Int)
		for _, signer := range senders {
			var balance *big.Int
			err := server.ContractCall(
				ctx,
				erc1155Contract,
				&balance,
				"balanceOf",
				signer.Address(), // owner
				tokenID,          // tokenId
			)
			if err != nil {
				return nil, fmt.Errorf("failed to get balance for signer %s: %w", signer.Address().Hex(), err)
			}
			tokenBalances[signer.Address()] = balance

			slog.Info("Minted ERC1155 tokens", "signer", signer.Address().Hex(), "balance", balance.String())
		}
	}

	monkeyERC1155Doer := ethtestserver.NewMonkeyDoer(
		func(ctx context.Context, op *ethtestserver.MonkeyOperator, gen *core.BlockGen) (*types.Transaction, error) {
			// Pick two random signers from the provided pool.
			signers, err := op.PickSigners(2)
			if err != nil {
				return nil, fmt.Errorf("failed to pick signers for monkey ERC1155 transfer: %w", err)
			}

			sender, recipient := signers[0], signers[1]
			tokenID := big.NewInt(1) // Use the same token ID as minted earlier

			// Pack the call data for the safeTransferFrom method.
			calldata, err := erc1155Contract.ABI.Pack(
				"safeTransferFrom",
				sender.Address(),    // from
				recipient.Address(), // to
				tokenID,             // tokenId
				big.NewInt(1),       // transfer 1 token
				[]byte{},            // data
			)
			if err != nil {
				return nil, fmt.Errorf("failed to pack safeTransferFrom call: %w", err)
			}

			// Create a transaction to call the contract.
			nonce := gen.TxNonce(sender.Address())

			tx := types.NewTransaction(
				nonce,
				common.Address(erc1155Contract.Address),
				nil,
				300_000,
				gen.BaseFee(),
				calldata,
			)

			//signer := types.LatestSigner(params.AllDevChainProtocolChanges)

			signer := types.HomesteadSigner{}
			signedTx, err := types.SignTx(tx, signer, sender.RawPrivateKey())
			if err != nil {
				return nil, fmt.Errorf("failed to sign transaction: %w", err)
			}

			return signedTx, nil
		},
	)

	monkeyTransferOperator, err := ethtestserver.NewMonkeyOperator(&ethtestserver.MonkeyOperatorConfig{
		Signers: senders,
		Ticks:   100,
	}, monkeyERC1155Doer, server)
	if err != nil {
		return nil, fmt.Errorf("failed to create monkey skyweaver transfer operator: %w", err)
	}

	slog.Info("Created Monkey ERC1155 Transfer operator", "signersCount", len(senders))

	err = monkeyTransferOperator.Run(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to run monkey skyweaver transfer operator: %w", err)
	}

	slog.Info("Monkey Skyweaver Transfer operator started")
	go func() {
		err := server.Mine()
		if err != nil {
			slog.Error("Failed to mine block after starting monkey transfer operator", "error", err)
			return
		}

		time.Sleep(10 * time.Second) // Allow some time for the operator to start
		// print balances of the minted tokens for each signer
		{
			tokenBalances := make(map[common.Address]*big.Int)
			for _, signer := range senders {
				var balance *big.Int
				err := server.ContractCall(
					ctx,
					erc1155Contract,
					&balance,
					"balanceOf",
					signer.Address(), // owner
					tokenID,          // tokenId
				)
				if err != nil {
					slog.Error("Failed to get balance for signer", "signer", signer.Address().Hex(), "error", err)
					continue
				}
				tokenBalances[signer.Address()] = balance

				slog.Info("Minted ERC1155 tokens", "signer", signer.Address().Hex(), "balance", balance.String())
			}
		}

	}()

	return monkeyTransferOperator, nil
}
