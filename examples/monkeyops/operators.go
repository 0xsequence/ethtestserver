package main

import (
	"context"
	"fmt"
	"log/slog"
	"math/big"

	"github.com/0xsequence/ethtestserver"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
)

var txSigner = types.HomesteadSigner{}

// runMonkeyTransferors uses the given sender and recipient pools for ETH transfers.
func runMonkeyTransferors(ctx context.Context, server *ethtestserver.ETHTestServer, senders []*ethtestserver.Signer, recipients []*ethtestserver.Signer) (*ethtestserver.MonkeyOperator, error) {
	// Create a monkey doer for ETH transfers.
	monkeyTransferor := ethtestserver.NewMonkeyDoer(
		func(ctx context.Context, op *ethtestserver.MonkeyOperator, gen *core.BlockGen) (*types.Transaction, error) {
			sender := ethtestserver.PickRandomSigner(senders)
			recipient := ethtestserver.PickRandomSigner(recipients)
			amount := ethtestserver.PickRandomAmount(1, 100)

			nonce := gen.TxNonce(sender.Address())

			slog.Info("ETH transfer",
				"sender", sender.Address().Hex(),
				"recipient", recipient.Address().Hex(),
				"amount", amount.String(),
				"nonce", nonce,
			)

			tx, err := types.SignTx(
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
			if err != nil {
				return nil, fmt.Errorf("failed to sign ETH transfer: %w", err)
			}

			return tx, nil
		},
	)

	monkeyTransferOperator, err := ethtestserver.NewMonkeyOperator(
		nil,
		monkeyTransferor,
		server,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create monkey transfer operator: %w", err)
	}

	if err = monkeyTransferOperator.Run(ctx); err != nil {
		return nil, fmt.Errorf("failed to run monkey transfer operator: %w", err)
	}

	return monkeyTransferOperator, nil
}

// runMonkeyERC1155Transferors deploys an ERC1155 contract, mints tokens for
// each sender, and sets up a monkey operator to transfer these tokens between
// senders.
func runMonkeyERC1155Transferors(ctx context.Context, server *ethtestserver.ETHTestServer, senders []*ethtestserver.Signer, recipients []*ethtestserver.Signer, tokenMin int64, tokenMax int64, mintAmount int64) (*ethtestserver.MonkeyOperator, error) {
	slog.Info("Deploying ERC1155 contract for monkey transfers")

	contractDeployer := ethtestserver.PickRandomSigner(senders)

	erc1155Contract, err := server.DeployContract(
		ctx,
		contractDeployer,
		ERC1155TestTokenArtifact.ContractName,
		contractDeployer.Address(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to deploy ERC1155 contract: %w", err)
	}

	// Mint tokens for each sender
	for i := tokenMin; i <= tokenMax; i++ {
		tokenID := big.NewInt(i)
		for _, s := range senders {
			slog.Info("Minting ERC1155 tokens", "address", s.Address())
			err = server.ContractTransact(
				ctx,
				contractDeployer,
				erc1155Contract,
				"mint",
				s.Address(),
				tokenID,
				big.NewInt(mintAmount),
				[]byte{},
			)
			if err != nil {
				return nil, fmt.Errorf("failed to mint ERC1155 tokens for %s: %w", s.Address().Hex(), err)
			}
			slog.Info("Minted ERC1155 tokens",
				"contract", erc1155Contract.Address.Hex(),
				"recipient", s.Address().Hex(),
				"tokenID", tokenID.String(),
				"amount", mintAmount,
			)
		}
	}

	monkeyERC1155Doer := ethtestserver.NewMonkeyDoer(
		func(ctx context.Context, op *ethtestserver.MonkeyOperator, gen *core.BlockGen) (*types.Transaction, error) {
			// Pick sender from the senders pool and recipient from the recipients pool.
			sender := ethtestserver.PickRandomSigner(senders)
			recipient := ethtestserver.PickRandomSigner(recipients)

			tokenID := ethtestserver.PickRandomAmount(tokenMin, tokenMax)
			amount := ethtestserver.PickRandomAmount(1, mintAmount)

			calldata, err := erc1155Contract.ABI.Pack(
				"safeTransferFrom",
				sender.Address(),    // from
				recipient.Address(), // to
				tokenID,
				amount,
				[]byte{},
			)
			if err != nil {
				return nil, fmt.Errorf("failed to pack safeTransferFrom call: %w", err)
			}

			nonce := gen.TxNonce(sender.Address())
			tx := types.NewTransaction(
				nonce,
				common.Address(erc1155Contract.Address),
				nil,
				300_000,
				gen.BaseFee(),
				calldata,
			)
			signedTx, err := types.SignTx(tx, txSigner, sender.RawPrivateKey())
			if err != nil {
				return nil, fmt.Errorf("failed to sign ERC1155 transfer transaction: %w", err)
			}

			return signedTx, nil
		},
	)

	monkeyTransferOperator, err := ethtestserver.NewMonkeyOperator(
		&ethtestserver.MonkeyOperatorConfig{
			Signers: senders,
		},
		monkeyERC1155Doer,
		server,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create monkey ERC1155 transfer operator: %w", err)
	}

	slog.Info("Created Monkey ERC1155 Transfer operator", "signersCount", len(senders))
	if err = monkeyTransferOperator.Run(ctx); err != nil {
		return nil, fmt.Errorf("failed to run monkey ERC1155 transfer operator: %w", err)
	}
	slog.Info("Monkey ERC1155 Transfer operator started")

	return monkeyTransferOperator, nil
}

// runMonkeyERC20Transferors deploys an ERC20 contract, mints tokens for each
// sender, and sets up a monkey operator to transfer tokens between senders and
// recipients.
func runMonkeyERC20Transferors(ctx context.Context, server *ethtestserver.ETHTestServer, senders []*ethtestserver.Signer, recipients []*ethtestserver.Signer) (*ethtestserver.MonkeyOperator, error) {
	slog.Info("Deploying ERC20 contract for monkey transfers")
	contractDeployer := senders[0]

	contractRecipient := senders[0]
	contractInitialOwner := senders[0]

	erc20Contract, err := server.DeployContract(
		ctx,
		contractDeployer,
		ERC20TestTokenArtifact.ContractName,
		contractRecipient.Address(),
		contractInitialOwner.Address(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to deploy ERC20 contract: %w", err)
	}

	mintAmount := big.NewInt(1000)
	// Mint ERC20 tokens to each sender.
	for _, s := range senders {
		err = server.ContractTransact(
			ctx,
			contractInitialOwner,
			erc20Contract,
			"mint",
			s.Address(),
			mintAmount,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to mint ERC20 tokens for %s: %w", s.Address().Hex(), err)
		}
	}

	monkeyERC20Doer := ethtestserver.NewMonkeyDoer(
		func(ctx context.Context, op *ethtestserver.MonkeyOperator, gen *core.BlockGen) (*types.Transaction, error) {
			sender := ethtestserver.PickRandomSigner(senders)
			recipient := ethtestserver.PickRandomSigner(recipients)
			transferAmount := big.NewInt(1) // Transfer 1 token

			slog.Debug("ERC20 transfer",
				"sender", sender.Address().Hex(),
				"recipient", recipient.Address().Hex(),
				"amount", transferAmount.String(),
			)

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

			signedTx, err := types.SignTx(tx, txSigner, sender.RawPrivateKey())
			if err != nil {
				return nil, fmt.Errorf("failed to sign ERC20 transfer transaction: %w", err)
			}

			return signedTx, nil
		},
	)

	monkeyTransferOperator, err := ethtestserver.NewMonkeyOperator(
		&ethtestserver.MonkeyOperatorConfig{
			Signers: senders,
		},
		monkeyERC20Doer,
		server,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create monkey ERC20 transfer operator: %w", err)
	}

	slog.Info("Created Monkey ERC20 Transfer operator", "signersCount", len(senders))
	if err = monkeyTransferOperator.Run(ctx); err != nil {
		return nil, fmt.Errorf("failed to run monkey ERC20 transfer operator: %w", err)
	}

	slog.Info("Monkey ERC20 Transfer operator started")
	return monkeyTransferOperator, nil
}

// runMonkeyERC721Transferors deploys an ERC721 contract, mints tokens for each
// sender, and sets up a monkey operator to transfer these tokens between
// senders and recipients.
func runMonkeyERC721Transferors(ctx context.Context, server *ethtestserver.ETHTestServer, senders []*ethtestserver.Signer, recipients []*ethtestserver.Signer) (*ethtestserver.MonkeyOperator, error) {
	slog.Info("Deploying ERC721 contract for monkey transfers")
	contractDeployer := senders[0]

	erc721Contract, err := server.DeployContract(
		ctx,
		contractDeployer,
		ERC721TestTokenArtifact.ContractName,
		contractDeployer.Address(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to deploy ERC721 contract: %w", err)
	}

	// Map each sender's address to its minted tokenID.
	tokenMapping := make(map[common.Address]*big.Int)
	for i, s := range senders {
		tokenID := big.NewInt(int64(i + 1))
		err = server.ContractTransact(
			ctx,
			contractDeployer,
			erc721Contract,
			"mint",
			s.Address(),
			tokenID,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to mint ERC721 token for %s: %w", s.Address().Hex(), err)
		}
		tokenMapping[s.Address()] = tokenID
		slog.Info("Minted ERC721 token", "signer", s.Address().Hex(), "tokenID", tokenID.String())
	}

	monkeyERC721Doer := ethtestserver.NewMonkeyDoer(
		func(ctx context.Context, op *ethtestserver.MonkeyOperator, gen *core.BlockGen) (*types.Transaction, error) {
			sender := ethtestserver.PickRandomSigner(senders)
			recipient := ethtestserver.PickRandomSigner(recipients)

			tokenID, ok := tokenMapping[sender.Address()]
			if !ok {
				return nil, fmt.Errorf("sender %s does not own a minted ERC721 token", sender.Address().Hex())
			}

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

			signedTx, err := types.SignTx(tx, txSigner, sender.RawPrivateKey())
			if err != nil {
				return nil, fmt.Errorf("failed to sign ERC721 transfer transaction: %w", err)
			}

			return signedTx, nil
		},
	)

	monkeyTransferOperator, err := ethtestserver.NewMonkeyOperator(
		&ethtestserver.MonkeyOperatorConfig{
			Signers: senders,
		},
		monkeyERC721Doer,
		server,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create monkey ERC721 transfer operator: %w", err)
	}

	slog.Info("Created Monkey ERC721 Transfer operator", "signersCount", len(senders))
	if err = monkeyTransferOperator.Run(ctx); err != nil {
		return nil, fmt.Errorf("failed to run monkey ERC721 transfer operator: %w", err)
	}

	slog.Info("Monkey ERC721 Transfer operator started")
	return monkeyTransferOperator, nil
}
