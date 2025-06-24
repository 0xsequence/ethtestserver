package ethtestserver

import (
	"crypto/ecdsa"
	"fmt"
	"sync/atomic"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// Signer is a helper type that encapsulates a signing key
type Signer struct {
	key *ecdsa.PrivateKey

	nonce atomic.Uint64 // Nonce for the next transaction
}

func NewSigner() *Signer {
	key, err := crypto.GenerateKey()
	if err != nil {
		// this is a test utility, it either works or it panics
		panic(fmt.Sprintf("failed to generate private key: %v", err))
	}

	return newSignerWithPrivateKey(key)
}

func NewSignerWithKey(key string) *Signer {
	privateKey, err := crypto.HexToECDSA(key)
	if err != nil {
		// this is a test utility, it either works or it panics
		panic(fmt.Sprintf("failed to create private key from hex: %v", err))
	}

	return newSignerWithPrivateKey(privateKey)
}

func newSignerWithPrivateKey(key *ecdsa.PrivateKey) *Signer {
	return &Signer{
		key: key,
	}
}

func (h *Signer) Key() string {
	if h.key == nil {
		panic("Signer: Key is nil")
	}

	privateKeyBytes := crypto.FromECDSA(h.key)
	return fmt.Sprintf("%02x", privateKeyBytes)
}

func (h *Signer) privateKey() *ecdsa.PrivateKey {
	if h.key == nil {
		panic("Signer: Key is nil")
	}

	return h.key
}

func (h *Signer) Address() common.Address {
	if h.key == nil {
		panic("Signer: Key is nil")
	}

	return crypto.PubkeyToAddress(h.key.PublicKey)
}

// GenSigners generates a slice of n Signer instances.
func GenSigners(n int) []*Signer {
	if n < 0 {
		return nil
	}
	// Create a slice with capacity n (could be empty if n==0)
	signers := make([]*Signer, n)
	for i := 0; i < n; i++ {
		signers[i] = NewSigner()
	}
	return signers
}
