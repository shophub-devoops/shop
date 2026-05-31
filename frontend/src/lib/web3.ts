// MetaMask / Sepolia helpers for the USDT checkout. Kept tiny: connect, ensure
// the Sepolia network, and send a single ERC-20 transfer.
import { BrowserProvider, Contract, parseUnits } from 'ethers';
import type { ShopInfo } from './api';

const SEPOLIA_CHAIN_ID = '0xaa36a7'; // 11155111
const ERC20_ABI = ['function transfer(address to, uint256 amount) returns (bool)'];

function provider(): any {
  const eth = (window as { ethereum?: any }).ethereum;
  if (!eth) {
    throw new Error('MetaMask not found — install the browser extension.');
  }
  return eth;
}

async function ensureSepolia(): Promise<void> {
  try {
    await provider().request({
      method: 'wallet_switchEthereumChain',
      params: [{ chainId: SEPOLIA_CHAIN_ID }],
    });
  } catch {
    throw new Error('Switch MetaMask to the Sepolia test network and retry.');
  }
}

// connectWallet prompts MetaMask, ensures Sepolia, and returns the account.
export async function connectWallet(): Promise<string> {
  const accounts: string[] = await provider().request({ method: 'eth_requestAccounts' });
  await ensureSepolia();
  return accounts[0];
}

// payUSDT sends `amount` of the shop's token to the shop wallet and returns the
// transaction hash once MetaMask broadcasts it.
export async function payUSDT(info: ShopInfo, amount: string): Promise<string> {
  await ensureSepolia();
  const signer = await new BrowserProvider(provider()).getSigner();
  const token = new Contract(info.token_contract, ERC20_ABI, signer);
  const tx = await token.transfer(info.wallet_address, parseUnits(amount, info.token_decimals));
  return tx.hash as string;
}
