package main

import (
	"context"
	"fmt"
	"math/big"
	"math/rand"

	"github.com/0xsequence/ethtestserver"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	"log/slog"
)

// runMonkeyTransferors uses the given sender and recipient pools for ETH transfers.
func runMonkeyTransferors(ctx context.Context, server *ethtestserver.ETHTestServer, senders []*ethtestserver.Signer, recipients []*ethtestserver.Signer) (*ethtestserver.MonkeyOperator, error) {
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
				gen.Signer(),
				sender.RawPrivateKey(),
			)
			if err != nil {
				return nil, fmt.Errorf("failed to sign ETH transfer: %w", err)
			}

			return tx, nil
		},
	)

	monkeyTransferOperator, err := ethtestserver.NewMonkeyOperator(
		&ethtestserver.MonkeyOperatorConfig{},
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

// runMonkeyERC1155Transferors deploys an ERC1155 contract, mints tokens to
// each sender, and sets up a monkey operator to transfer the tokens between
// senders and recipients.
func runMonkeyERC1155Transferors(ctx context.Context, server *ethtestserver.ETHTestServer, senders []*ethtestserver.Signer, recipients []*ethtestserver.Signer, tokenMin int64, tokenMax int64, mintAmount int64) (*ethtestserver.MonkeyOperator, error) {

	var erc1155Contract *ethtestserver.ETHContractCaller
	stateKeyPrefix := "monkeyERC1155TransferorsState:"
	stateContractCallerKey := stateKeyPrefix + "contractCaller"

	ok, err := server.RetrieveValue(stateContractCallerKey, &erc1155Contract)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve ERC1155 contract caller: %w", err)
	}
	if ok {
		slog.Info("Using existing ERC1155 contract for monkey transfers", "address", erc1155Contract.Address.Hex())
	} else {
		slog.Info("Deploying ERC1155 contract for monkey transfers")
		contractDeployer := senders[0]
		erc1155Contract, err = server.DeployContract(
			ctx,
			contractDeployer,
			ERC1155TestTokenArtifact.ContractName,
			contractDeployer.Address(),
		)
		if err != nil {
			return nil, fmt.Errorf("failed to deploy ERC1155 contract: %w", err)
		}
		if err := server.StoreValue(stateContractCallerKey, erc1155Contract); err != nil {
			return nil, fmt.Errorf("failed to store ERC1155 contract caller: %w", err)
		}
		slog.Info("Deployed ERC1155 contract", "address", erc1155Contract.Address.Hex())
	}

	stateTokensMintedKey := stateKeyPrefix + "tokensMinted"
	ok, err = server.RetrieveValue(stateTokensMintedKey, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve tokens minted state: %w", err)
	}

	if ok {
		slog.Info("Tokens already minted for monkey transfers, skipping minting")
	} else {
		slog.Info("Minting ERC1155 tokens for monkey transfers")
		for i := tokenMin; i <= tokenMax; i++ {
			tokenID := big.NewInt(i)
			for _, s := range senders {
				slog.Info("Minting ERC1155 tokens", "recipient", s.Address().Hex())
				err = server.ContractTransact(
					ctx,
					senders[0],
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
		if err := server.StoreValue(stateTokensMintedKey, true); err != nil {
			return nil, fmt.Errorf("failed to store tokens minted state: %w", err)
		}
	}

	monkeyERC1155Doer := ethtestserver.NewMonkeyDoer(
		func(ctx context.Context, op *ethtestserver.MonkeyOperator, gen *core.BlockGen) (*types.Transaction, error) {
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
			signedTx, err := types.SignTx(tx, gen.Signer(), sender.RawPrivateKey())
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

// runMonkeyERC20Transferors deploys an ERC20 contract, mints tokens to each
// sender, and sets up a monkey operator to transfer the tokens
func runMonkeyERC20Transferors(ctx context.Context, server *ethtestserver.ETHTestServer, senders []*ethtestserver.Signer, recipients []*ethtestserver.Signer, mintAmount int) (*ethtestserver.MonkeyOperator, error) {
	stateKeyPrefix := "monkeyERC20TransferorsState:"
	var erc20Contract *ethtestserver.ETHContractCaller
	stateContractCallerKey := stateKeyPrefix + "contractCaller"

	// Try to retrieve the previously deployed ERC20 contract.
	ok, err := server.RetrieveValue(stateContractCallerKey, &erc20Contract)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve ERC20 contract caller: %w", err)
	}
	if ok {
		slog.Info("Using existing ERC20 contract for monkey transfers", "address", erc20Contract.Address.Hex())
	} else {
		slog.Info("Deploying ERC20 contract for monkey transfers")
		contractDeployer := senders[0]
		// For ERC20 the recipient/initial owner information might be the same.
		contractRecipient := senders[0]
		contractInitialOwner := senders[0]
		erc20Contract, err = server.DeployContract(
			ctx,
			contractDeployer,
			ERC20TestTokenArtifact.ContractName,
			contractRecipient.Address(),
			contractInitialOwner.Address(),
		)
		if err != nil {
			return nil, fmt.Errorf("failed to deploy ERC20 contract: %w", err)
		}
		if err := server.StoreValue(stateContractCallerKey, erc20Contract); err != nil {
			return nil, fmt.Errorf("failed to store ERC20 contract caller: %w", err)
		}
		slog.Info("Deployed ERC20 contract", "address", erc20Contract.Address.Hex())
	}

	// Check if tokens have been minted already.
	stateTokensMintedKey := stateKeyPrefix + "tokensMinted"
	var tokensMinted bool
	ok, err = server.RetrieveValue(stateTokensMintedKey, &tokensMinted)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve tokens minted state for ERC20: %w", err)
	}
	if ok && tokensMinted {
		slog.Info("Tokens already minted for ERC20 monkey transfers, skipping minting")
	} else {
		slog.Info("Minting ERC20 tokens for monkey transfers")
		// Mint tokens for each sender.
		for _, s := range senders {
			err = server.ContractTransact(
				ctx,
				senders[0], // using the initial owner as sender for minting
				erc20Contract,
				"mint",
				s.Address(),
				big.NewInt(int64(mintAmount)),
			)
			if err != nil {
				return nil, fmt.Errorf("failed to mint ERC20 tokens for %s: %w", s.Address().Hex(), err)
			}
		}
		if err := server.StoreValue(stateTokensMintedKey, true); err != nil {
			return nil, fmt.Errorf("failed to store tokens minted state for ERC20: %w", err)
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

			signedTx, err := types.SignTx(tx, gen.Signer(), sender.RawPrivateKey())
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

// runMonkeyERC20Transferors deploys an ERC20 contract, mints tokens to each
// sender, and sets up a monkey operator to transfer the tokens.
func runMonkeyERC721Transferors(ctx context.Context, server *ethtestserver.ETHTestServer, senders []*ethtestserver.Signer, recipients []*ethtestserver.Signer, mintedTokens int) (*ethtestserver.MonkeyOperator, error) {
	stateKeyPrefix := "monkeyERC721TransferorsState:"
	var erc721Contract *ethtestserver.ETHContractCaller
	stateContractCallerKey := stateKeyPrefix + "contractCaller"

	// Retrieve the deployed ERC721 contract from state if available.
	ok, err := server.RetrieveValue(stateContractCallerKey, &erc721Contract)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve ERC721 contract caller: %w", err)
	}
	if ok {
		slog.Info("Using existing ERC721 contract for monkey transfers", "address", erc721Contract.Address.Hex())
	} else {
		slog.Info("Deploying ERC721 contract for monkey transfers")
		contractDeployer := ethtestserver.PickRandomSigner(senders)
		erc721Contract, err = server.DeployContract(
			ctx,
			contractDeployer,
			ERC721TestTokenArtifact.ContractName,
			contractDeployer.Address(),
		)
		if err != nil {
			return nil, fmt.Errorf("failed to deploy ERC721 contract: %w", err)
		}
		if err := server.StoreValue(stateContractCallerKey, erc721Contract); err != nil {
			return nil, fmt.Errorf("failed to store ERC721 contract caller: %w", err)
		}
		slog.Info("Deployed ERC721 contract", "address", erc721Contract.Address.Hex())
	}

	// Retrieve the token mapping state if available.
	// tokenMapping holds for each address a slice of minted token IDs.
	var tokenMapping map[common.Address][]uint64
	stateTokenMappingKey := stateKeyPrefix + "tokenMapping"
	ok, err = server.RetrieveValue(stateTokenMappingKey, &tokenMapping)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve ERC721 token mapping: %w", err)
	}

	if ok {
		slog.Info("Tokens already minted for ERC721 monkey transfers, using stored token mapping")
	} else {
		slog.Info("Minting ERC721 tokens for monkey transfers")
		tokenMapping = make(map[common.Address][]uint64)
		var nextTokenID uint64 = 1

		for i := 0; i < mintedTokens; i++ {
			for _, s := range senders {
				err = server.ContractTransact(
					ctx,
					senders[0], // using a deployer or designated minter
					erc721Contract,
					"safeMint",
					s.Address(),
					big.NewInt(int64(nextTokenID)),
				)
				if err != nil {
					return nil, fmt.Errorf("failed to mint ERC721 token for %s: %w", s.Address().Hex(), err)
				}

				slog.Info("Minted ERC721 token",
					"contract", erc721Contract.Address.Hex(),
					"recipient", s.Address().Hex(),
					"tokenID", nextTokenID,
				)

				tokenMapping[s.Address()] = append(tokenMapping[s.Address()], nextTokenID)
				nextTokenID++
			}
		}

		if err := server.StoreValue(stateTokenMappingKey, tokenMapping); err != nil {
			return nil, fmt.Errorf("failed to store ERC721 token mapping: %w", err)
		}
	}

	monkeyERC721Doer := ethtestserver.NewMonkeyDoer(
		func(ctx context.Context, op *ethtestserver.MonkeyOperator, gen *core.BlockGen) (*types.Transaction, error) {
			sender := ethtestserver.PickRandomSigner(senders)
			senderAddr := sender.Address()

			// If the sender has no tokens, skip.
			if len(tokenMapping[senderAddr]) == 0 {
				slog.Debug("No tokens available for transfer", "sender", senderAddr.Hex())
				return nil, nil
			}

			recipient := ethtestserver.PickRandomSigner(recipients)
			recipientAddr := recipient.Address()

			// Pick a random token ID from the sender's tokens.
			index := rand.Intn(len(tokenMapping[senderAddr]))
			tokenID := tokenMapping[senderAddr][index]

			calldata, err := erc721Contract.ABI.Pack(
				"safeTransferFrom",
				sender.Address(),
				recipient.Address(),
				big.NewInt(int64(tokenID)),
			)
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

			signedTx, err := types.SignTx(tx, gen.Signer(), sender.RawPrivateKey())
			if err != nil {
				return nil, fmt.Errorf("failed to sign ERC721 transfer transaction: %w", err)
			}

			slog.Info("ERC721 transfer",
				"sender", sender.Address().Hex(),
				"recipient", recipient.Address().Hex(),
				"tokenID", tokenID,
			)

			// Remove the token from the sender's mapping.
			tokenMapping[senderAddr] = append(tokenMapping[senderAddr][:index], tokenMapping[senderAddr][index+1:]...)
			// Add the token to the recipient's mapping.
			tokenMapping[recipientAddr] = append(tokenMapping[recipientAddr], tokenID)
			// (Optionally, update the persistent state of tokenMapping here.)

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
