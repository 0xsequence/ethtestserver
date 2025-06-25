package main

import (
	"context"
	"fmt"
	"log/slog"
	"math/big"
	"time"

	"github.com/0xsequence/ethtestserver"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
)

var txSigner = types.HomesteadSigner{}

func runMonkeyTransferors(ctx context.Context, server *ethtestserver.ETHTestServer, senders []*ethtestserver.Signer, recipients []*ethtestserver.Signer) (*ethtestserver.MonkeyOperator, error) {
	// Monkey transferor
	monkeyTransferor := ethtestserver.NewMonkeyDoer(
		func(ctx context.Context, op *ethtestserver.MonkeyOperator, gen *core.BlockGen) (*types.Transaction, error) {

			sender := ethtestserver.PickRandomSigner(senders)
			recipient := ethtestserver.PickRandomSigner(recipients)

			amount := ethtestserver.PickRandomAmount(1, 100)

			slog.Debug("ETH transfer",
				"sender", sender.Address().Hex(),
				"recipient", recipient.Address().Hex(),
				"amount", amount.String(),
			)

			nonce := gen.TxNonce(sender.Address())

			tx, _ := types.SignTx(
				types.NewTransaction(
					nonce,
					recipient.Address(),
					amount,
					params.TxGas,
					big.NewInt(params.InitialBaseFee),
					nil,
				),
				txSigner,
				sender.RawPrivateKey(),
			)

			return tx, nil
		},
	)

	// Random ETH transfers using monkey signers
	monkeyTransferOperator, err := ethtestserver.NewMonkeyOperator(
		nil,
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

func runMonkeyERC20Transferors(ctx context.Context, server *ethtestserver.ETHTestServer, monkeySigners []*ethtestserver.Signer) (*ethtestserver.MonkeyOperator, error) {
	slog.Info("Deploying ERC20 contract for monkey transfers")

	// Deploy ERC20 contract using the first signer from the given slice.
	contractDeployer := monkeySigners[0]

	erc20Contract, err := server.DeployContract(
		ctx,
		contractDeployer,
		ERC20TestTokenArtifact.ContractName,
		contractDeployer.Address(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to deploy ERC20 contract: %w", err)
	}

	// Use a small pool of signers to mint ERC20 tokens.
	senders := monkeySigners[1:10]
	mintAmount := big.NewInt(1000) // mint 1000 tokens

	for _, sender := range senders {
		err = server.ContractTransact(
			ctx,
			contractDeployer,
			erc20Contract,
			"mint",
			sender.Address(),
			mintAmount,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to mint ERC20 tokens for %s: %w", sender.Address().Hex(), err)
		}
	}

	// Optionally, print ERC20 balances.
	for _, sender := range senders {
		var balance *big.Int
		err := server.ContractCall(
			ctx,
			erc20Contract,
			&balance,
			"balanceOf",
			sender.Address(),
		)
		if err != nil {
			slog.Error("Failed to get ERC20 balance", "signer", sender.Address().Hex(), "error", err)
			continue
		}
		slog.Info("Minted ERC20 tokens", "signer", sender.Address().Hex(), "balance", balance.String())
	}

	monkeyERC20Doer := ethtestserver.NewMonkeyDoer(
		func(ctx context.Context, op *ethtestserver.MonkeyOperator, gen *core.BlockGen) (*types.Transaction, error) {
			// Pick two random signers from the pool.
			signers, err := op.PickSigners(2)
			if err != nil {
				return nil, fmt.Errorf("failed to pick signers for monkey ERC20 transfer: %w", err)
			}

			sender, recipient := signers[0], signers[1]
			transferAmount := big.NewInt(1) // Transfer 1 token

			calldata, err := erc20Contract.ABI.Pack("transfer", recipient.Address(), transferAmount)
			if err != nil {
				return nil, fmt.Errorf("failed to pack ERC20 transfer call: %w", err)
			}

			nonce := gen.TxNonce(sender.Address())
			gasLimit := uint64(100_000)

			tx := types.NewTransaction(
				nonce,
				common.Address(erc20Contract.Address),
				nil,
				gasLimit,
				gen.BaseFee(),
				calldata,
			)

			signer := types.HomesteadSigner{}
			signedTx, err := types.SignTx(tx, signer, sender.RawPrivateKey())
			if err != nil {
				return nil, fmt.Errorf("failed to sign ERC20 transfer transaction: %w", err)
			}

			return signedTx, nil
		},
	)

	monkeyTransferOperator, err := ethtestserver.NewMonkeyOperator(&ethtestserver.MonkeyOperatorConfig{
		Signers: senders,
		Ticks:   100,
	}, monkeyERC20Doer, server)
	if err != nil {
		return nil, fmt.Errorf("failed to create monkey ERC20 transfer operator: %w", err)
	}

	slog.Info("Created Monkey ERC20 Transfer operator", "signersCount", len(senders))

	err = monkeyTransferOperator.Run(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to run monkey ERC20 transfer operator: %w", err)
	}

	slog.Info("Monkey ERC20 Transfer operator started")
	return monkeyTransferOperator, nil
}

func runMonkeyERC721Transferors(ctx context.Context, server *ethtestserver.ETHTestServer, monkeySigners []*ethtestserver.Signer) (*ethtestserver.MonkeyOperator, error) {
	slog.Info("Deploying ERC721 contract for monkey transfers")

	// Deploy ERC721 contract using the first signer from the given slice.
	contractDeployer := monkeySigners[0]

	erc721Contract, err := server.DeployContract(
		ctx,
		contractDeployer,
		ERC721TestTokenArtifact.ContractName,
		contractDeployer.Address(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to deploy ERC721 contract: %w", err)
	}

	// Use a small pool of signers for minting.
	senders := monkeySigners[1:10]
	// Create a mapping to track which tokenID was minted for which signer.
	tokenMapping := make(map[common.Address]*big.Int)

	// For each signer in the pool mint a unique NFT (tokenID = i+1)
	for i, sender := range senders {
		tokenID := big.NewInt(int64(i + 1))
		err = server.ContractTransact(
			ctx,
			contractDeployer,
			erc721Contract,
			"mint",
			sender.Address(),
			tokenID,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to mint ERC721 token for %s: %w", sender.Address().Hex(), err)
		}
		tokenMapping[sender.Address()] = tokenID
		slog.Info("Minted ERC721 token", "signer", sender.Address().Hex(), "tokenID", tokenID.String())
	}

	monkeyERC721Doer := ethtestserver.NewMonkeyDoer(
		func(ctx context.Context, op *ethtestserver.MonkeyOperator, gen *core.BlockGen) (*types.Transaction, error) {
			// Pick two random signers from the pool.
			signers, err := op.PickSigners(2)
			if err != nil {
				return nil, fmt.Errorf("failed to pick signers for monkey ERC721 transfer: %w", err)
			}

			sender, recipient := signers[0], signers[1]
			tokenID, ok := tokenMapping[sender.Address()]
			if !ok {
				return nil, fmt.Errorf("sender %s does not own a minted ERC721 token", sender.Address().Hex())
			}

			// Pack call data for safeTransferFrom (ERC721 standard with 3 parameters).
			calldata, err := erc721Contract.ABI.Pack("safeTransferFrom", sender.Address(), recipient.Address(), tokenID)
			if err != nil {
				return nil, fmt.Errorf("failed to pack ERC721 safeTransferFrom call: %w", err)
			}

			nonce := gen.TxNonce(sender.Address())
			gasLimit := uint64(150_000)

			tx := types.NewTransaction(
				nonce,
				common.Address(erc721Contract.Address),
				nil,
				gasLimit,
				gen.BaseFee(),
				calldata,
			)

			signer := types.HomesteadSigner{}
			signedTx, err := types.SignTx(tx, signer, sender.RawPrivateKey())
			if err != nil {
				return nil, fmt.Errorf("failed to sign ERC721 transfer transaction: %w", err)
			}

			return signedTx, nil
		},
	)

	monkeyTransferOperator, err := ethtestserver.NewMonkeyOperator(&ethtestserver.MonkeyOperatorConfig{
		Signers: senders,
		Ticks:   100,
	}, monkeyERC721Doer, server)
	if err != nil {
		return nil, fmt.Errorf("failed to create monkey ERC721 transfer operator: %w", err)
	}

	slog.Info("Created Monkey ERC721 Transfer operator", "signersCount", len(senders))

	err = monkeyTransferOperator.Run(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to run monkey ERC721 transfer operator: %w", err)
	}

	slog.Info("Monkey ERC721 Transfer operator started")
	return monkeyTransferOperator, nil
}
