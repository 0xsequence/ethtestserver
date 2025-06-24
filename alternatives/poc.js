import { ethers } from "ethers";
import fs from "fs";

const provider = new ethers.JsonRpcProvider("http://127.0.0.1:8545");

const load = name => JSON.parse(fs.readFileSync(`../artifacts/${name}.json`));

async function main() {
  //const signer    = provider.getSigner(0);

  const signer = new ethers.Wallet(
    "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80",
    provider
  )

  const recipient = await provider.getSigner(2);
  const owner = await provider.getSigner(1);

  const ERC20   = await new ethers.ContractFactory(load("ERC20TestToken").abi,  load("ERC20TestToken").bytecode,  signer).deploy(recipient.address, owner.address);
  const ERC721  = await new ethers.ContractFactory(load("ERC721TestToken").abi, load("ERC721TestToken").bytecode, signer).deploy(owner.address);
  const ERC1155 = await new ethers.ContractFactory(load("ERC1155TestToken").abi, load("ERC1155TestToken").bytecode, signer).deploy(owner.address);

  console.table({
    ERC20:  ERC20.target,
    ERC721: ERC721.target,
    ERC1155: ERC1155.target
  });

  await provider.send("anvil_mine", [1]);

  // Mint ERC1155
  await ERC1155.connect(owner).mint(recipient.address, 1, 1, "0x"); // Mint 1155 to a random address
  await ERC1155.connect(recipient).safeTransferFrom(recipient.address, "0x976EA74026E726554dB657fA54763abd0C3a0aa9", 1, 1, "0x");

  //ERC20.transfer("0xa0Ee7A142d267C1f36714E4a8F75612F20a79720", 1); // ERC20 transfer to a random address
}

main();
