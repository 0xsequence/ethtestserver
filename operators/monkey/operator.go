package monkey

import (
	"context"

	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
)

type MonkeyDoer interface {
	Do(ctx context.Context, op *MonkeyOperator, gen *core.BlockGen) (*types.Transaction, error)
}

type MonkeyBlockGenerator interface {
	GenerateBlocks(int, func(int, *core.BlockGen) error) ([]*types.Block, []types.Receipts, error)
}
