package monkey

import (
	"context"
	"log/slog"

	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
)

type MonkeyNOPDoer struct{}

func (m *MonkeyNOPDoer) Do(ctx context.Context, op *MonkeyOperator, gen *core.BlockGen) (*types.Transaction, error) {
	// No operation, just a placeholder
	slog.Info("MonkeyNOPDoer: doing nothing")
	return nil, nil
}

type MonkeyDoerFunc struct {
	doer func(ctx context.Context, op *MonkeyOperator, gen *core.BlockGen) (*types.Transaction, error)
}

func (f *MonkeyDoerFunc) Do(ctx context.Context, op *MonkeyOperator, gen *core.BlockGen) (*types.Transaction, error) {
	return f.doer(ctx, op, gen)
}

func NewMonkeyDoer(doer func(ctx context.Context, op *MonkeyOperator, gen *core.BlockGen) (*types.Transaction, error)) MonkeyDoer {
	return &MonkeyDoerFunc{doer: doer}
}
