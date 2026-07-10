// MetaMask / Sepolia helpers for the USDT checkout. Kept tiny: connect, ensure
// the Sepolia network, and send a single ERC-20 transfer.
import { BrowserProvider, Contract } from 'ethers';
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

// switchAccount reopens MetaMask's account picker so the buyer can pay from a
// different account than the one currently permitted to the site. eth_accounts
// alone returns the already-connected account without a prompt; requesting the
// eth_accounts permission forces the selection dialog, after which the chosen
// account becomes accounts[0].
export async function switchAccount(): Promise<string> {
  await provider().request({
    method: 'wallet_requestPermissions',
    params: [{ eth_accounts: {} }],
  });
  const accounts: string[] = await provider().request({ method: 'eth_requestAccounts' });
  await ensureSepolia();
  return accounts[0];
}

// onAccountsChanged subscribes to MetaMask account switches so the UI can follow
// the wallet. Returns an unsubscribe function.
export function onAccountsChanged(cb: (account: string | null) => void): () => void {
  const eth = (window as { ethereum?: any }).ethereum;
  if (!eth?.on) return () => {};
  const handler = (accounts: string[]) => cb(accounts[0] ?? null);
  eth.on('accountsChanged', handler);
  return () => eth.removeListener?.('accountsChanged', handler);
}

// payUSDT sends `amountBaseUnits` of the shop's token to the shop wallet and
// returns the transaction hash once MetaMask broadcasts it. The caller passes
// the exact integer base-unit total (summed from the backend-computed order
// amounts) so the on-chain transfer matches what the payment verifier requires
// to the unit — never a display-rounded value.
export async function payUSDT(info: ShopInfo, amountBaseUnits: bigint): Promise<string> {
  await ensureSepolia();
  const signer = await new BrowserProvider(provider()).getSigner();
  const from = await signer.getAddress();
  // Paying from the shop's own payout address is a transfer to self: MetaMask
  // flags it as suspicious and the purchase can't complete. Catch it here with a
  // clear message instead of letting the user hit the wallet's red alert.
  if (from.toLowerCase() === info.wallet_address.toLowerCase()) {
    throw new Error(
      'This account is the shop’s own payout wallet — use “Change” to pay from a different account.',
    );
  }
  const token = new Contract(info.token_contract, ERC20_ABI, signer);
  const tx = await token.transfer(info.wallet_address, amountBaseUnits);
  return tx.hash as string;
}
