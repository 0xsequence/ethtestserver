//go:build !pebble
// +build !pebble

package ethtestserver

import (
	"fmt"

	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/ethdb/memorydb"
	"github.com/ethereum/go-ethereum/node"
)

func (s *ETHTestServer) makeKeyValueStore(stack *node.Node, options *node.DatabaseOptions) (ethdb.KeyValueStore, error) {
	if s.config.DBMode == dbModeDisk {
		return nil, fmt.Errorf("disk mode is not enabled: please build with '-tags pebble'")
	}
	return memorydb.New(), nil
}

func (s *ETHTestServer) openDatabase(stack *node.Node, readOnly bool) (ethdb.Database, error) {
	options := node.DatabaseOptions{
		ReadOnly:          readOnly,
		Cache:             512,
		Handles:           512,
		AncientsDirectory: stack.ResolveAncient("chaindata", ""),
	}

	kvdb, err := s.makeKeyValueStore(stack, &options)
	if err != nil {
		return nil, fmt.Errorf("failed to create key-value store: %w", err)
	}

	opts := rawdb.OpenOptions{
		ReadOnly:         readOnly,
		Ancient:          options.AncientsDirectory,
		Era:              options.EraDirectory,
		MetricsNamespace: options.MetricsNamespace,
	}

	db, err := rawdb.Open(kvdb, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to open raw database: %w", err)
	}

	return db, nil
}
