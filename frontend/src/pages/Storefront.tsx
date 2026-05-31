import { useEffect, useState } from 'react';
import { api, type Item } from '../lib/api';
import { connectWallet, payUSDT } from '../lib/web3';

export default function Storefront() {
  const [items, setItems] = useState<Item[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [account, setAccount] = useState<string | null>(null);
  const [qty, setQty] = useState<Record<string, number>>({});
  const [busy, setBusy] = useState(false);
  const [status, setStatus] = useState<string | null>(null);

  useEffect(() => {
    api.listItems().then(setItems).catch((e) => setError(String(e)));
  }, []);

  async function connect() {
    try {
      setAccount(await connectWallet());
      setStatus(null);
    } catch (e) {
      setStatus((e as Error).message);
    }
  }

  // pollOrder polls the order until the backend's on-chain verifier confirms or
  // fails it (or we give up after a few minutes).
  async function pollOrder(id: string) {
    for (let i = 0; i < 40; i++) {
      const order = await api.getOrder(id);
      if (order.status === 'confirmed') {
        setStatus('✅ Payment confirmed — order complete!');
        api.listItems().then(setItems).catch(() => {});
        return;
      }
      if (order.status === 'failed') {
        setStatus('❌ Payment could not be verified on-chain.');
        return;
      }
      await new Promise((r) => setTimeout(r, 5000));
    }
    setStatus('Still pending — the order will confirm once the transaction is mined.');
  }

  async function buy(item: Item) {
    setBusy(true);
    try {
      let acct = account;
      if (!acct) {
        acct = await connectWallet();
        setAccount(acct);
      }
      const quantity = qty[item.id] ?? 1;
      const amount = (Number(item.price_usdt) * quantity).toFixed(2);

      const info = await api.shopInfo();
      setStatus('Confirm the payment in MetaMask…');
      const txHash = await payUSDT(info, amount);

      setStatus('Payment sent — recording order…');
      const id = crypto.randomUUID();
      await api.createOrder({
        id,
        buyer_wallet: acct,
        tx_hash: txHash,
        amount_usdt: amount,
        item_id: item.id,
        item_quantity: quantity,
      });

      setStatus('Waiting for on-chain confirmation…');
      await pollOrder(id);
    } catch (e) {
      setStatus('Error: ' + (e as Error).message);
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h1 className="text-xl font-semibold">Items</h1>
        {account ? (
          <span className="text-xs text-slate-500">
            Wallet: {account.slice(0, 6)}…{account.slice(-4)}
          </span>
        ) : (
          <button onClick={connect} className="rounded bg-slate-900 px-3 py-1 text-sm text-white">
            Connect wallet
          </button>
        )}
      </div>

      {error && <p className="text-red-600">{error}</p>}
      {status && (
        <p className="rounded border border-slate-200 bg-slate-50 px-3 py-2 text-sm">{status}</p>
      )}

      <ul className="grid gap-3 sm:grid-cols-2">
        {items.map((item) => (
          <li key={item.id} className="rounded-lg border border-slate-200 bg-white p-4">
            <div className="font-medium">{item.name}</div>
            <div className="text-sm text-slate-500">{item.price_usdt} USDT</div>
            <div className="text-xs text-slate-400">In stock: {item.stock}</div>
            <div className="mt-2 flex items-center gap-2">
              <input
                type="number"
                min={1}
                max={item.stock}
                value={qty[item.id] ?? 1}
                onChange={(e) =>
                  setQty((q) => ({ ...q, [item.id]: Math.max(1, Number(e.target.value)) }))
                }
                className="w-16 rounded border border-slate-300 px-2 py-1 text-sm"
              />
              <button
                onClick={() => buy(item)}
                disabled={item.stock === 0 || busy}
                className="rounded bg-slate-900 px-3 py-1 text-sm text-white disabled:bg-slate-300"
              >
                Buy with USDT
              </button>
            </div>
          </li>
        ))}
      </ul>
    </div>
  );
}
