package ethtestserver

import (
	_ "embed"

	"github.com/0xsequence/ethkit/ethartifact"
)

var (
	//go:embed artifacts/ERC20TestToken.json
	artifact_ERC20TestToken string // OpenZeppelin ERC20 token for testing purposes

	//go:embed artifacts/ERC721TestToken.json
	artifact_ERC721TestToken string // OpenZeppelin ERC721 token for testing purposes

	//go:embed artifacts/ERC1155TestToken.json
	artifact_ERC1155TestToken string // OpenZeppelin ERC1155 token for testing purposes
)

var (
	ERC20TestTokenArtifact   = ethartifact.MustParseArtifactJSON(artifact_ERC20TestToken)
	ERC721TestTokenArtifact  = ethartifact.MustParseArtifactJSON(artifact_ERC721TestToken)
	ERC1155TestTokenArtifact = ethartifact.MustParseArtifactJSON(artifact_ERC1155TestToken)
)
