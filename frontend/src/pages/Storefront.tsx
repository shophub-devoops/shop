import { useEffect, useMemo, useState } from 'react';
import { parseUnits } from 'ethers';
import { Search, ShoppingCart, Trash2, Wallet } from 'lucide-react';
import { api, fmtUsdt, type Item } from '../lib/api';
import { connectWallet, switchAccount, onAccountsChanged, payUSDT } from '../lib/web3';

// PaymentFlow shows the order status, and while the payment is in flight it
// streams little coins from "You" to "Shop" — a small visual cue that funds are
// moving on-chain (pure CSS, see index.css .coin).
function PaymentFlow({ status }: { status: string }) {
  const inFlight = status.startsWith('Waiting') || status.startsWith('Payment sent');
  return (
    <div className="rounded-xl border border-line bg-card px-4 py-3 text-sm">
      <div className="flex items-center gap-3">
        <span className="font-medium text-fg">You</span>
        <div className="relative h-5 flex-1">
          <div className="absolute inset-x-0 top-1/2 -translate-y-1/2 border-t border-dashed border-line" />
          {inFlight &&
            Array.from({ length: 7 }).map((_, i) => (
              <span key={i} className="coin" style={{ animationDelay: `${i * 0.3}s` }} />
            ))}
        </div>
        <span className="font-medium text-fg">Shop</span>
      </div>
      <p className="mt-2 text-muted">{status}</p>
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

  // Follow account switches made from within MetaMask so the connected chip and
  // the paying account stay in sync with the wallet.
  useEffect(() => onAccountsChanged(setAccount), []);

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

  // changeAccount opens MetaMask's picker so the buyer can pay from a different
  // account (e.g. one that isn't the shop's own payout wallet).
  async function changeAccount() {
    try {
      setAccount(await switchAccount());
      setStatus(null);
    } catch (e) {
      setStatus((e as Error).message);
    }
  }

  // pollOrders polls every order created at checkout until the backend's
  // on-chain verifier confirms or fails it (or we give up after a few minutes).
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

  // checkout reserves stock first (one pending order per cart line), then pays
  // the whole cart total in a single USDT transfer, then attaches the tx hash
  // to the orders so the backend verifier can confirm them on-chain. Paying
  // after the reservation means the buyer can never pay for items that ran out
  // between cart and checkout; abandoned reservations expire server-side.
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
      setStatus('Reserving your items…');
      const ids: string[] = [];
      // Sum the backend-computed order amounts (price × qty, server-side) into
      // exact base units. Paying this — not the display-rounded cart total —
      // guarantees the on-chain transfer matches what the verifier expects to
      // the unit, so payments with sub-cent prices can't fail verification.
      let totalBaseUnits = 0n;
      for (const line of cartLines) {
        const id = crypto.randomUUID();
        const created = await api.createOrder({
          id,
          buyer_wallet: acct,
          item_id: line.item.id,
          item_quantity: line.qty,
        });
        totalBaseUnits += parseUnits(created.amount_usdt, info.token_decimals);
        ids.push(id);
      }

      setStatus('Confirm the payment in MetaMask…');
      const txHash = await payUSDT(info, totalBaseUnits);

      setStatus('Payment sent — recording transaction…');
      await Promise.all(ids.map((id) => api.attachTx(id, txHash)));

      setStatus('Waiting for on-chain confirmation…');
      await pollOrders(ids);
    } catch (e) {
      setStatus('Error: ' + (e as Error).message);
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="animate-fade-up space-y-6">
      <div className="flex items-end justify-between">
        <div>
          <h1 className="font-serif text-[28px] font-medium">Storefront</h1>
          <p className="mt-1 text-sm text-muted">Browse, add to cart, and pay with USDT on Sepolia.</p>
        </div>
        {account ? (
          <div className="inline-flex items-center gap-2">
            <span className="inline-flex items-center gap-1.5 rounded-lg border border-line bg-card px-3 py-1.5 text-xs text-muted">
              <Wallet size={13} className="text-accent-bright" />
              {account.slice(0, 6)}…{account.slice(-4)}
            </span>
            <button
              onClick={changeAccount}
              className="rounded-lg border border-line bg-card px-3 py-1.5 text-xs text-muted transition-colors hover:border-white/20 hover:text-fg"
              title="Pay from a different wallet account"
            >
              Change
            </button>
          </div>
        ) : (
          <button
            onClick={connect}
            className="inline-flex items-center gap-2 rounded-lg bg-accent px-4 py-2 text-sm font-medium text-white transition-[filter] hover:brightness-110"
          >
            <Wallet size={15} /> Connect wallet
          </button>
        )}
      </div>

      <div className="relative">
        <Search size={16} className="absolute left-3.5 top-1/2 -translate-y-1/2 text-faint" />
        <input
          type="search"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder="Search items…"
          className="w-full rounded-lg border border-line bg-surface py-2.5 pl-10 pr-3.5 text-sm outline-none transition-colors placeholder:text-faint focus:border-accent-bright"
        />
      </div>

      {error && <p className="text-sm text-red-400">{error}</p>}
      {status && <PaymentFlow status={status} />}

      <div className="grid gap-5 md:grid-cols-3">
        <ul className="grid gap-3 sm:grid-cols-2 md:col-span-2">
          {filtered.map((item) => {
            const inCart = cart[item.id] ?? 0;
            const soldOut = item.stock === 0;
            return (
              <li
                key={item.id}
                className="group rounded-xl border border-line bg-card p-4 transition-colors hover:border-white/20"
              >
                <div className="font-semibold">{item.name}</div>
                <div className="mt-0.5 text-sm text-accent-bright">{fmtUsdt(item.price_usdt)} USDT</div>
                <div className="mt-1 text-xs text-faint">In stock: {item.stock}</div>
                <button
                  onClick={() => addToCart(item)}
                  disabled={soldOut || inCart >= item.stock}
                  className="mt-3 inline-flex w-full items-center justify-center gap-1.5 rounded-lg bg-white/5 py-2 text-sm font-medium text-fg transition-colors hover:bg-accent hover:text-white disabled:cursor-not-allowed disabled:bg-white/5 disabled:text-faint"
                >
                  <ShoppingCart size={14} /> {soldOut ? 'Sold out' : 'Add to cart'}
                </button>
              </li>
            );
          })}
          {filtered.length === 0 && (
            <li className="rounded-xl border border-dashed border-line py-12 text-center text-sm text-muted sm:col-span-2">
              No items match your search.
            </li>
          )}
        </ul>

        <aside className="h-fit rounded-xl border border-line bg-card p-5">
          <h2 className="mb-3 flex items-center gap-2 font-serif text-lg font-medium">
            <ShoppingCart size={16} className="text-accent-bright" /> Cart
          </h2>
          {cartLines.length === 0 ? (
            <p className="text-sm text-muted">Your cart is empty.</p>
          ) : (
            <>
              <ul className="space-y-2.5">
                {cartLines.map((line) => (
                  <li key={line.item.id} className="flex items-center justify-between gap-2 text-sm">
                    <span className="flex-1 truncate">{line.item.name}</span>
                    <input
                      type="number"
                      min={0}
                      max={line.item.stock}
                      value={line.qty}
                      onChange={(e) => setQty(line.item.id, Number(e.target.value))}
                      className="w-14 rounded-md border border-line bg-surface px-2 py-1 text-sm outline-none focus:border-accent-bright"
                    />
                    <button
                      onClick={() => setQty(line.item.id, 0)}
                      className="text-faint transition-colors hover:text-red-400"
                      title="Remove"
                    >
                      <Trash2 size={14} />
                    </button>
                  </li>
                ))}
              </ul>
              <div className="mt-4 flex items-center justify-between border-t border-line pt-3 text-sm font-medium">
                <span className="text-muted">Total</span>
                <span>{total.toFixed(2)} USDT</span>
              </div>
              <button
                onClick={checkout}
                disabled={busy}
                className="btn-gradient mt-4 h-11 w-full rounded-lg text-[15px] font-medium disabled:opacity-60"
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
