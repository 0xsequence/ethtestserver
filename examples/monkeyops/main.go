package main

import (
	"context"
	"fmt"
	"log/slog"
	"math/big"
	"os"
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
		wallet0 = ethtestserver.NewSignerWithKey("0x4a840bea3489bdebe9d90687b93e70e7b42341f96987382dd61fba2c6a976640") // 0x3c25c2353D0193625c868C4222C85592149E7f4B
		wallet1 = ethtestserver.NewSignerWithKey("0x035378650c1b589ee7811302365d5ec734f85baf94c6ce105a594d65513811c2") // 0x0E9d2aD0F0E906f5cC1385283C6aaA800Ee1c97e
		wallet2 = ethtestserver.NewSignerWithKey("0x4a42aa45eb965529b0c78c9ac9770e04bba024cd7ff3f08b5be52d1ccb8dd772") // 0xb2e7637Fc4b1fd1fAcF1E2E9aF2221fAe4d273D3
		wallet3 = ethtestserver.NewSignerWithKey("0x91a98fa4e3134f570adfa6ab1b6442e9b1111f9b10450d08c835207e363230c0") // 0x2735e4D29B9c6F6641E25CF60c12255B12EEec8e
		wallet4 = ethtestserver.NewSignerWithKey("0x2abb2f79ffcabc47f3aa1c038dc8dff4a459c69e4f4f392e6c35a412b87c3a18") // 0xe48151D11d6459A72677BeC02E1D0895F3973fE0
		wallet5 = ethtestserver.NewSignerWithKey("0xf4cccf93983a14c1f5dc1613558ea96460490f74cc8fe93d88aa4138fe4357db") // 0x25366930038CCD45610E8554Ad795A97EE63a9D5
		wallet6 = ethtestserver.NewSignerWithKey("0xbb40f29b98f4e5d2f94f3e530de6f47ac64e3dee3e2b0d49b6b496509bc8d716") // 0xa140Dcc8E04f96Fff324df1878e871e8A859aF87
		wallet7 = ethtestserver.NewSignerWithKey("0xcb510a76512cc7250406571ac21bd5c5b54f97c48bd851e5d21367a86a752287") // 0xf1C5D542e3224b9C858C621b4fA31347DE488F38
		wallet8 = ethtestserver.NewSignerWithKey("0x26b5fa333032217f5ab69b4c8df270eedaf98968daf8d2c139fec51f3d05c02e") // 0xE75d8245f878c4929A72f9429896b52c49baC625
		wallet9 = ethtestserver.NewSignerWithKey("0xc111b3ebb5bf8d838eca2210307ff93c3c9c7a21a24c458e837e2a228a64e900") // 0x184A0e08cD157Fb6cea13c0AdD643aCa6E7eEc24

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

	// run monkey transferers for ERC20
	monkeyERC20Transferor, err := runMonkeyERC20Transferors(ctx, server, monkeySigners[201:211])
	if err != nil {
		slog.Error("Failed to run monkey ERC20 transferors", "error", err)
		return
	}
	defer monkeyERC20Transferor.Stop(ctx)
	slog.Info("Monkey ERC20 transferor started successfully", "signersCount", len(monkeySigners[201:211]))

	// run monkey transferers for ERC721
	monkeyERC721Transferor, err := runMonkeyERC721Transferors(ctx, server, monkeySigners[211:221])
	if err != nil {
		slog.Error("Failed to run monkey ERC721 transferors", "error", err)
		return
	}
	defer monkeyERC721Transferor.Stop(ctx)
	slog.Info("Monkey ERC721 transferor started successfully", "signersCount", len(monkeySigners[211:221]))

	go func() {
		for {
			select {
			case <-ctx.Done():
				slog.Info("Stopping status printing goroutine")
				return
			default:
			}
			server.PrintStatus()
			time.Sleep(10 * time.Second) // Print status every 10 seconds
		}
	}()

	time.Sleep(24 * time.Hour) // Run the test for 24 hours
	slog.Info("Test run completed, stopping server")

	err = server.Stop(ctx)
	if err != nil {
		slog.Error("Failed to stop test server", "error", err)
		os.Exit(1)
		return
	}
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
