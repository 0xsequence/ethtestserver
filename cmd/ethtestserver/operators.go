package main

import (
	"context"
	"fmt"
	"log/slog"
	"math/big"
	"math/rand"
	"sync"

	"github.com/0xsequence/ethtestserver"
	"github.com/0xsequence/ethtestserver/operator/monkey"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
)

const gasLimit = 3_000_000

var monkeyOperatorConfig monkey.MonkeyOperatorConfig

type nativeTransactionGenerator struct {
	senders    []*ethtestserver.Signer
	recipients []*ethtestserver.Signer
}

func NewNativeTransactionGenerator(senders, recipients []*ethtestserver.Signer) *nativeTransactionGenerator {
	return &nativeTransactionGenerator{
		senders:    senders,
		recipients: recipients,
	}
}

func (n *nativeTransactionGenerator) Initialize(ctx context.Context) error {
	// Nothing special to initialize for native transfers.
	return nil
}

func (n *nativeTransactionGenerator) GenerateTransaction(ctx context.Context, gen *core.BlockGen) (*types.Transaction, error) {
	sender := ethtestserver.PickRandomSigner(n.senders)
	recipient := ethtestserver.PickRandomSigner(n.recipients)
	amount := ethtestserver.PickRandomAmount(1, 100)

	nonce := gen.TxNonce(sender.Address())

	slog.Debug("ETH transfer",
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
			gasLimit,
			gen.BaseFee(),
			nil,
		),
		gen.Signer(),
		sender.RawPrivateKey(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to sign ETH transfer: %w", err)
	}

	return tx, nil
}

type ERC1155TransactionGenerator struct {
	senders       []*ethtestserver.Signer
	recipients    []*ethtestserver.Signer
	server        *ethtestserver.ETHTestServer
	tokenMin      int64
	tokenMax      int64
	mintAmount    int64
	erc1155Caller *ethtestserver.ETHContractCaller
}

func NewERC1155TransactionGenerator(server *ethtestserver.ETHTestServer, senders, recipients []*ethtestserver.Signer, tokenMin, tokenMax, mintAmount int64) *ERC1155TransactionGenerator {
	return &ERC1155TransactionGenerator{
		senders:    senders,
		recipients: recipients,
		server:     server,
		tokenMin:   tokenMin,
		tokenMax:   tokenMax,
		mintAmount: mintAmount,
	}
}

func (g *ERC1155TransactionGenerator) Initialize(ctx context.Context) error {
	stateContractCallerKey := "monkeyERC1155TransferorsState:contractCaller"
	var contract *ethtestserver.ETHContractCaller
	ok, err := g.server.RetrieveValue(stateContractCallerKey, &contract)
	if err != nil {
		return fmt.Errorf("failed to retrieve ERC1155 contract caller: %w", err)
	}
	if ok {
		slog.Debug("Using existing ERC1155 contract for monkey transfers", "address", contract.Address.Hex())
		g.erc1155Caller = contract
	} else {
		slog.Debug("Deploying ERC1155 contract for monkey transfers")
		deployer := g.senders[0]
		contract, err = g.server.DeployContract(
			deployer,
			ERC1155TestTokenArtifact.ContractName,
			deployer.Address(),
		)
		if err != nil {
			return fmt.Errorf("failed to deploy ERC1155 contract: %w", err)
		}
		if err := g.server.StoreValue(stateContractCallerKey, contract); err != nil {
			return fmt.Errorf("failed to store ERC1155 contract caller: %w", err)
		}
		g.erc1155Caller = contract
		slog.Debug("Deployed ERC1155 contract", "address", contract.Address.Hex())
	}

	// Check whether tokens have been minted.
	stateTokensMintedKey := "monkeyERC1155TransferorsState:tokensMinted"
	var tokensMinted bool
	ok, err = g.server.RetrieveValue(stateTokensMintedKey, &tokensMinted)
	if err != nil {
		return fmt.Errorf("failed to retrieve tokens minted state: %w", err)
	}
	if ok && tokensMinted {
		slog.Debug("Tokens already minted for monkey transfers, skipping minting")
	} else {
		slog.Debug("Minting ERC1155 tokens for monkey transfers")
		for i := g.tokenMin; i <= g.tokenMax; i++ {
			tokenID := big.NewInt(i)
			for _, s := range g.senders {
				slog.Debug("Minting ERC1155 tokens", "recipient", s.Address().Hex())
				err = g.server.ContractTransact(
					g.senders[0],
					g.erc1155Caller,
					"mint",
					s.Address(),
					tokenID,
					big.NewInt(g.mintAmount),
					[]byte{},
				)
				if err != nil {
					return fmt.Errorf("failed to mint ERC1155 tokens for %s: %w", s.Address().Hex(), err)
				}
				slog.Debug("Minted ERC1155 tokens",
					"contract", g.erc1155Caller.Address.Hex(),
					"recipient", s.Address().Hex(),
					"tokenID", tokenID.String(),
					"amount", g.mintAmount,
				)
			}
		}
		if err := g.server.StoreValue(stateTokensMintedKey, true); err != nil {
			return fmt.Errorf("failed to store tokens minted state: %w", err)
		}
	}

	return nil
}

func (g *ERC1155TransactionGenerator) GenerateTransaction(ctx context.Context, gen *core.BlockGen) (*types.Transaction, error) {
	sender := ethtestserver.PickRandomSigner(g.senders)
	recipient := ethtestserver.PickRandomSigner(g.recipients)
	// tokenID is chosen randomly from the [tokenMin, tokenMax] range.
	tokenID := ethtestserver.PickRandomAmount(g.tokenMin, g.tokenMax)
	amount := ethtestserver.PickRandomAmount(1, g.mintAmount)

	calldata, err := g.erc1155Caller.ABI.Pack("safeTransferFrom",
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
		common.Address(g.erc1155Caller.Address),
		nil,
		gasLimit,
		gen.BaseFee(),
		calldata,
	)
	signedTx, err := types.SignTx(tx, gen.Signer(), sender.RawPrivateKey())
	if err != nil {
		return nil, fmt.Errorf("failed to sign ERC1155 transfer transaction: %w", err)
	}

	return signedTx, nil
}

type ERC20TransactionGenerator struct {
	senders     []*ethtestserver.Signer
	recipients  []*ethtestserver.Signer
	server      *ethtestserver.ETHTestServer
	mintAmount  int
	erc20Caller *ethtestserver.ETHContractCaller
}

func NewERC20TransactionGenerator(server *ethtestserver.ETHTestServer, senders, recipients []*ethtestserver.Signer, mintAmount int) *ERC20TransactionGenerator {
	return &ERC20TransactionGenerator{
		senders:    senders,
		recipients: recipients,
		server:     server,
		mintAmount: mintAmount,
	}
}

func (g *ERC20TransactionGenerator) Initialize(ctx context.Context) error {
	stateContractCallerKey := "monkeyERC20TransferorsState:contractCaller"
	var contract *ethtestserver.ETHContractCaller
	ok, err := g.server.RetrieveValue(stateContractCallerKey, &contract)
	if err != nil {
		return fmt.Errorf("failed to retrieve ERC20 contract caller: %w", err)
	}
	if ok {
		slog.Debug("Using existing ERC20 contract for monkey transfers", "address", contract.Address.Hex())
		g.erc20Caller = contract
	} else {
		slog.Debug("Deploying ERC20 contract for monkey transfers")
		deployer := g.senders[0]
		contractRecipient := g.senders[0]
		contractInitialOwner := g.senders[0]
		contract, err = g.server.DeployContract(
			deployer,
			ERC20TestTokenArtifact.ContractName,
			contractRecipient.Address(),
			contractInitialOwner.Address(),
		)
		if err != nil {
			return fmt.Errorf("failed to deploy ERC20 contract: %w", err)
		}
		if err := g.server.StoreValue(stateContractCallerKey, contract); err != nil {
			return fmt.Errorf("failed to store ERC20 contract caller: %w", err)
		}
		g.erc20Caller = contract
		slog.Debug("Deployed ERC20 contract", "address", contract.Address.Hex())
	}

	// Check tokens minted state.
	stateTokensMintedKey := "monkeyERC20TransferorsState:tokensMinted"
	var tokensMinted bool
	ok, err = g.server.RetrieveValue(stateTokensMintedKey, &tokensMinted)
	if err != nil {
		return fmt.Errorf("failed to retrieve tokens minted state for ERC20: %w", err)
	}
	if ok && tokensMinted {
		slog.Debug("Tokens already minted for ERC20 monkey transfers, skipping minting")
	} else {
		slog.Debug("Minting ERC20 tokens for monkey transfers")
		for _, s := range g.senders {
			err = g.server.ContractTransact(
				g.senders[0],
				g.erc20Caller,
				"mint",
				s.Address(),
				big.NewInt(int64(g.mintAmount)),
			)
			if err != nil {
				return fmt.Errorf("failed to mint ERC20 tokens for %s: %w", s.Address().Hex(), err)
			}
		}
		if err := g.server.StoreValue(stateTokensMintedKey, true); err != nil {
			return fmt.Errorf("failed to store tokens minted state for ERC20: %w", err)
		}
	}

	return nil
}

func (g *ERC20TransactionGenerator) GenerateTransaction(ctx context.Context, gen *core.BlockGen) (*types.Transaction, error) {
	sender := ethtestserver.PickRandomSigner(g.senders)
	recipient := ethtestserver.PickRandomSigner(g.recipients)
	transferAmount := big.NewInt(1) // Transfer 1 token

	slog.Debug("ERC20 transfer",
		"sender", sender.Address().Hex(),
		"recipient", recipient.Address().Hex(),
		"amount", transferAmount.String(),
	)

	calldata, err := g.erc20Caller.ABI.Pack("transfer", recipient.Address(), transferAmount)
	if err != nil {
		return nil, fmt.Errorf("failed to pack ERC20 transfer call: %w", err)
	}

	nonce := gen.TxNonce(sender.Address())
	tx := types.NewTransaction(
		nonce,
		common.Address(g.erc20Caller.Address),
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
}

type ERC721TransactionGenerator struct {
	senders      []*ethtestserver.Signer
	recipients   []*ethtestserver.Signer
	server       *ethtestserver.ETHTestServer
	mintedTokens int
	erc721Caller *ethtestserver.ETHContractCaller
	tokenMapping map[common.Address][]uint64
	mappingLock  sync.Mutex
}

func NewERC721TransactionGenerator(server *ethtestserver.ETHTestServer, senders, recipients []*ethtestserver.Signer, mintedTokens int) *ERC721TransactionGenerator {
	return &ERC721TransactionGenerator{
		senders:      senders,
		recipients:   recipients,
		server:       server,
		mintedTokens: mintedTokens,
	}
}

func (g *ERC721TransactionGenerator) Initialize(ctx context.Context) error {
	stateContractCallerKey := "monkeyERC721TransferorsState:contractCaller"
	var contract *ethtestserver.ETHContractCaller
	ok, err := g.server.RetrieveValue(stateContractCallerKey, &contract)
	if err != nil {
		return fmt.Errorf("failed to retrieve ERC721 contract caller: %w", err)
	}
	if ok {
		slog.Debug("Using existing ERC721 contract for monkey transfers", "address", contract.Address.Hex())
		g.erc721Caller = contract
	} else {
		slog.Debug("Deploying ERC721 contract for monkey transfers")
		deployer := ethtestserver.PickRandomSigner(g.senders)
		contract, err = g.server.DeployContract(
			deployer,
			ERC721TestTokenArtifact.ContractName,
			deployer.Address(),
		)
		if err != nil {
			return fmt.Errorf("failed to deploy ERC721 contract: %w", err)
		}
		if err := g.server.StoreValue(stateContractCallerKey, contract); err != nil {
			return fmt.Errorf("failed to store ERC721 contract caller: %w", err)
		}
		g.erc721Caller = contract
		slog.Debug("Deployed ERC721 contract", "address", contract.Address.Hex())
	}

	stateTokenMappingKey := "monkeyERC721TransferorsState:tokenMapping"
	var tokenMapping map[common.Address][]uint64
	ok, err = g.server.RetrieveValue(stateTokenMappingKey, &tokenMapping)
	if err != nil {
		return fmt.Errorf("failed to retrieve ERC721 token mapping: %w", err)
	}
	if ok {
		slog.Debug("Tokens already minted for ERC721 monkey transfers, using stored token mapping")
		g.tokenMapping = tokenMapping
	} else {
		slog.Debug("Minting ERC721 tokens for monkey transfers")
		tokenMapping = make(map[common.Address][]uint64)
		var nextTokenID uint64 = 1
		// Mint tokens for each sender.
		for i := 0; i < g.mintedTokens; i++ {
			for _, s := range g.senders {
				err = g.server.ContractTransact(
					g.senders[0],
					g.erc721Caller,
					"safeMint",
					s.Address(),
					big.NewInt(int64(nextTokenID)),
				)
				if err != nil {
					return fmt.Errorf("failed to mint ERC721 token for %s: %w", s.Address().Hex(), err)
				}
				slog.Debug("Minted ERC721 token",
					"contract", g.erc721Caller.Address.Hex(),
					"recipient", s.Address().Hex(),
					"tokenID", nextTokenID,
				)
				tokenMapping[s.Address()] = append(tokenMapping[s.Address()], nextTokenID)
				nextTokenID++
			}
		}
		if err := g.server.StoreValue(stateTokenMappingKey, tokenMapping); err != nil {
			return fmt.Errorf("failed to store ERC721 token mapping: %w", err)
		}
		g.tokenMapping = tokenMapping
	}

	return nil
}

func (g *ERC721TransactionGenerator) GenerateTransaction(ctx context.Context, gen *core.BlockGen) (*types.Transaction, error) {
	sender := ethtestserver.PickRandomSigner(g.senders)
	senderAddr := sender.Address()

	g.mappingLock.Lock()
	defer g.mappingLock.Unlock()
	if len(g.tokenMapping[senderAddr]) == 0 {
		slog.Debug("No tokens available for transfer", "sender", senderAddr.Hex())
		return nil, nil
	}

	recipient := ethtestserver.PickRandomSigner(g.recipients)
	recipientAddr := recipient.Address()
	index := rand.Intn(len(g.tokenMapping[senderAddr]))
	tokenID := g.tokenMapping[senderAddr][index]

	calldata, err := g.erc721Caller.ABI.Pack(
		"safeTransferFrom",
		sender.Address(),
		recipient.Address(),
		big.NewInt(int64(tokenID)),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to pack ERC721 safeTransferFrom call: %w", err)
	}

	nonce := gen.TxNonce(sender.Address())
	tx := types.NewTransaction(
		nonce,
		common.Address(g.erc721Caller.Address),
		nil,
		gasLimit,
		gen.BaseFee(),
		calldata,
	)

	signedTx, err := types.SignTx(tx, gen.Signer(), sender.RawPrivateKey())
	if err != nil {
		return nil, fmt.Errorf("failed to sign ERC721 transfer transaction: %w", err)
	}

	slog.Debug("ERC721 transfer",
		"sender", sender.Address().Hex(),
		"recipient", recipient.Address().Hex(),
		"tokenID", tokenID,
	)

	// Update token mapping: remove from sender, add to recipient.
	g.tokenMapping[senderAddr] = append(g.tokenMapping[senderAddr][:index], g.tokenMapping[senderAddr][index+1:]...)
	g.tokenMapping[recipientAddr] = append(g.tokenMapping[recipientAddr], tokenID)

	return signedTx, nil
}

func runMonkeyTransferors(ctx context.Context, server *ethtestserver.ETHTestServer, senders, recipients []*ethtestserver.Signer) (*monkey.MonkeyOperator, error) {
	txGen := NewNativeTransactionGenerator(senders, recipients)
	if err := txGen.Initialize(ctx); err != nil {
		return nil, fmt.Errorf("failed to initialize native transaction generator: %w", err)
	}

	monkeyOperator, err := monkey.NewMonkeyOperatorWithConfig(
		&monkeyOperatorConfig,
		server,
		txGen,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create monkey transfer operator: %w", err)
	}

	if err = monkeyOperator.Run(ctx); err != nil {
		return nil, fmt.Errorf("failed to run monkey transfer operator: %w", err)
	}

	return monkeyOperator, nil
}

func runMonkeyERC1155Transferors(ctx context.Context, server *ethtestserver.ETHTestServer, senders, recipients []*ethtestserver.Signer, tokenMin, tokenMax, mintAmount int64) (*monkey.MonkeyOperator, error) {
	txGen := NewERC1155TransactionGenerator(server, senders, recipients, tokenMin, tokenMax, mintAmount)
	if err := txGen.Initialize(ctx); err != nil {
		return nil, fmt.Errorf("failed to initialize ERC1155 transaction generator: %w", err)
	}

	monkeyOperator, err := monkey.NewMonkeyOperatorWithConfig(
		&monkeyOperatorConfig,
		server,
		txGen,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create monkey ERC1155 transfer operator: %w", err)
	}

	if err = monkeyOperator.Run(ctx); err != nil {
		return nil, fmt.Errorf("failed to run monkey ERC1155 transfer operator: %w", err)
	}

	return monkeyOperator, nil
}

func runMonkeyERC20Transferors(ctx context.Context, server *ethtestserver.ETHTestServer, senders, recipients []*ethtestserver.Signer, mintAmount int) (*monkey.MonkeyOperator, error) {
	txGen := NewERC20TransactionGenerator(server, senders, recipients, mintAmount)
	if err := txGen.Initialize(ctx); err != nil {
		return nil, fmt.Errorf("failed to initialize ERC20 transaction generator: %w", err)
	}

	monkeyOperator, err := monkey.NewMonkeyOperatorWithConfig(
		&monkeyOperatorConfig,
		server,
		txGen,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create monkey ERC20 transfer operator: %w", err)
	}

	if err = monkeyOperator.Run(ctx); err != nil {
		return nil, fmt.Errorf("failed to run monkey ERC20 transfer operator: %w", err)
	}

	return monkeyOperator, nil
}

func runMonkeyERC721Transferors(ctx context.Context, server *ethtestserver.ETHTestServer, senders, recipients []*ethtestserver.Signer, mintedTokens int) (*monkey.MonkeyOperator, error) {
	txGen := NewERC721TransactionGenerator(server, senders, recipients, mintedTokens)
	if err := txGen.Initialize(ctx); err != nil {
		return nil, fmt.Errorf("failed to initialize ERC721 transaction generator: %w", err)
	}

	monkeyOperator, err := monkey.NewMonkeyOperatorWithConfig(
		&monkeyOperatorConfig,
		server,
		txGen,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create monkey ERC721 transfer operator: %w", err)
	}

	if err = monkeyOperator.Run(ctx); err != nil {
		return nil, fmt.Errorf("failed to run monkey ERC721 transfer operator: %w", err)
	}

	return monkeyOperator, nil
}
