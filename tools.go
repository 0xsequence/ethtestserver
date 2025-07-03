package ethtestserver

import (
	"math/big"
	"math/rand"
)

func PickRandomSigner(signers []*Signer) *Signer {
	if len(signers) == 0 {
		panic("no signers available to pick from")
	}
	index := rand.Intn(len(signers))
	return signers[index]
}

func PickRandomAmount(a, b int64) *big.Int {
	if a > b {
		a, b = b, a
	}
	if a < 0 || b < 0 {
		panic("amounts must be non-negative")
	}
	if a == b {
		return big.NewInt(a)
	}
	return big.NewInt(a + rand.Int63n(b-a+1))
}
