import { useEffect, useMemo, useState } from 'react';
import { api, type Item } from '../lib/api';
import { connectWallet, payUSDT } from '../lib/web3';

// PaymentFlow shows the order status, and while the payment is in flight it
// streams little coins from "You" to "Shop" — a small visual cue that funds are
// moving on-chain (pure CSS, see index.css .coin).
function PaymentFlow({ status }: { status: string }) {
  const inFlight = status.startsWith('Waiting') || status.startsWith('Payment sent');
  return (
    <div className="rounded border border-slate-200 bg-slate-50 px-4 py-3 text-sm">
      <div className="flex items-center gap-3">
        <span className="font-medium text-slate-700">You</span>
        <div className="relative h-5 flex-1">
          <div className="absolute inset-x-0 top-1/2 -translate-y-1/2 border-t border-dashed border-slate-300" />
          {inFlight &&
            Array.from({ length: 7 }).map((_, i) => (
              <span key={i} className="coin" style={{ animationDelay: `${i * 0.3}s` }} />
            ))}
        </div>
        <span className="font-medium text-slate-700">Shop</span>
      </div>
      <p className="mt-2 text-slate-600">{status}</p>
    </div>
  );
}

export default function Storefront() {
  const [items, setItems] = useState<Item[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [account, setAccount] = useState<string | null>(null);
  const [search, setSearch] = useState('');
  // cart maps item id -> quantity.
  const [cart, setCart] = useState<Record<string, number>>({});
  const [busy, setBusy] = useState(false);
  const [status, setStatus] = useState<string | null>(null);

  useEffect(() => {
    api.listItems().then(setItems).catch((e) => setError(String(e)));
  }, []);

  const byId = useMemo(() => Object.fromEntries(items.map((it) => [it.id, it])), [items]);

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase();
    return q ? items.filter((it) => it.name.toLowerCase().includes(q)) : items;
  }, [items, search]);

  const cartLines = useMemo(
    () =>
      Object.entries(cart)
        .map(([id, qty]) => ({ item: byId[id], qty }))
        .filter((l) => l.item),
    [cart, byId],
  );

  const total = useMemo(
    () => cartLines.reduce((sum, l) => sum + Number(l.item.price_usdt) * l.qty, 0),
    [cartLines],
  );

  function addToCart(it: Item) {
    setCart((c) => {
      const next = (c[it.id] ?? 0) + 1;
      return next > it.stock ? c : { ...c, [it.id]: next };
    });
  }

  function setQty(id: string, qty: number) {
    setCart((c) => {
      if (qty <= 0) {
        const rest = { ...c };
        delete rest[id];
        return rest;
      }
      const max = byId[id]?.stock ?? qty;
      return { ...c, [id]: Math.min(qty, max) };
    });
  }

  async function connect() {
    try {
      setAccount(await connectWallet());
      setStatus(null);
    } catch (e) {
      setStatus((e as Error).message);
    }
  }

  // pollOrders polls every order created at checkout until the backend's
  // on-chain verifier confirms or fails them (or we give up after a few minutes).
  async function pollOrders(ids: string[]) {
    for (let i = 0; i < 40; i++) {
      const orders = await Promise.all(ids.map((id) => api.getOrder(id)));
      if (orders.every((o) => o.status === 'confirmed')) {
        setStatus('✅ Payment confirmed — order complete!');
        setCart({});
        api.listItems().then(setItems).catch(() => {});
        return;
      }
      if (orders.some((o) => o.status === 'failed')) {
        setStatus('❌ Payment could not be verified on-chain.');
        return;
      }
      await new Promise((r) => setTimeout(r, 5000));
    }
    setStatus('Still pending — the order will confirm once the transaction is mined.');
  }

  // checkout pays the whole cart total in a single USDT transfer, then records
  // one order per cart line sharing that transaction hash.
  async function checkout() {
    if (cartLines.length === 0) return;
    setBusy(true);
    try {
      let acct = account;
      if (!acct) {
        acct = await connectWallet();
        setAccount(acct);
      }

      const info = await api.shopInfo();
      setStatus('Confirm the payment in MetaMask…');
      const txHash = await payUSDT(info, total.toFixed(2));

      setStatus('Payment sent — recording order…');
      const ids: string[] = [];
      for (const line of cartLines) {
        const id = crypto.randomUUID();
        ids.push(id);
        await api.createOrder({
          id,
          buyer_wallet: acct,
          tx_hash: txHash,
          amount_usdt: (Number(line.item.price_usdt) * line.qty).toFixed(2),
          item_id: line.item.id,
          item_quantity: line.qty,
        });
      }

      setStatus('Waiting for on-chain confirmation…');
      await pollOrders(ids);
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

      <input
        type="search"
        value={search}
        onChange={(e) => setSearch(e.target.value)}
        placeholder="Search items…"
        className="w-full rounded border border-slate-300 px-3 py-2 text-sm"
      />

      {error && <p className="text-red-600">{error}</p>}
      {status && <PaymentFlow status={status} />}

      <div className="grid gap-4 md:grid-cols-3">
        <ul className="grid gap-3 sm:grid-cols-2 md:col-span-2">
          {filtered.map((item) => (
            <li key={item.id} className="rounded-lg border border-slate-200 bg-white p-4">
              <div className="font-medium">{item.name}</div>
              <div className="text-sm text-slate-500">{item.price_usdt} USDT</div>
              <div className="text-xs text-slate-400">In stock: {item.stock}</div>
              <button
                onClick={() => addToCart(item)}
                disabled={item.stock === 0 || (cart[item.id] ?? 0) >= item.stock}
                className="mt-2 rounded bg-slate-900 px-3 py-1 text-sm text-white disabled:bg-slate-300"
              >
                Add to cart
              </button>
            </li>
          ))}
          {filtered.length === 0 && <li className="text-sm text-slate-500">No items match your search.</li>}
        </ul>

        <aside className="rounded-lg border border-slate-200 bg-white p-4">
          <h2 className="mb-2 font-semibold">Cart</h2>
          {cartLines.length === 0 ? (
            <p className="text-sm text-slate-500">Your cart is empty.</p>
          ) : (
            <>
              <ul className="space-y-2">
                {cartLines.map((line) => (
                  <li key={line.item.id} className="flex items-center justify-between gap-2 text-sm">
                    <span className="flex-1 truncate">{line.item.name}</span>
                    <input
                      type="number"
                      min={0}
                      max={line.item.stock}
                      value={line.qty}
                      onChange={(e) => setQty(line.item.id, Number(e.target.value))}
                      className="w-14 rounded border border-slate-300 px-2 py-1"
                    />
                  </li>
                ))}
              </ul>
              <div className="mt-3 flex items-center justify-between border-t border-slate-200 pt-2 text-sm font-medium">
                <span>Total</span>
                <span>{total.toFixed(2)} USDT</span>
              </div>
              <button
                onClick={checkout}
                disabled={busy}
                className="mt-3 w-full rounded bg-slate-900 px-3 py-2 text-sm text-white disabled:bg-slate-300"
              >
                {busy ? 'Processing…' : 'Pay with USDT'}
              </button>
            </>
          )}
        </aside>
      </div>
    </div>
  );
}
