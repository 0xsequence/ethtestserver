package operator

import (
	"context"

	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
)

type BlockGenerator interface {
	GenerateBlocks(int, func(int, *core.BlockGen) error) ([]*types.Block, []types.Receipts, error)
}

type TransactionGenerator interface {
	Initialize(ctx context.Context) error

	GenerateTransaction(ctx context.Context, gen *core.BlockGen) (*types.Transaction, error)
}
