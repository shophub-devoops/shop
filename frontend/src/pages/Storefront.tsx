import { useEffect, useState } from 'react';
import { api, type Item } from '../lib/api';

type CartLine = { item: Item; quantity: number };

export default function Storefront() {
  const [items, setItems] = useState<Item[]>([]);
  const [cart, setCart] = useState<CartLine[]>([]);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    api.listItems().then(setItems).catch((e) => setError(String(e)));
  }, []);

  function addToCart(item: Item) {
    setCart((c) => {
      const existing = c.find((l) => l.item.id === item.id);
      if (existing) {
        return c.map((l) =>
          l.item.id === item.id ? { ...l, quantity: l.quantity + 1 } : l,
        );
      }
      return [...c, { item, quantity: 1 }];
    });
  }

  const total = cart
    .reduce((sum, line) => sum + Number(line.item.price_usdt) * line.quantity, 0)
    .toFixed(2);

  return (
    <div className="grid gap-6 md:grid-cols-[2fr_1fr]">
      <section>
        <h1 className="mb-4 text-xl font-semibold">Items</h1>
        {error && <p className="text-red-600">{error}</p>}
        <ul className="grid gap-3 sm:grid-cols-2">
          {items.map((item) => (
            <li key={item.id} className="rounded-lg border border-slate-200 bg-white p-4">
              <div className="font-medium">{item.name}</div>
              <div className="text-sm text-slate-500">{item.price_usdt} USDT</div>
              <div className="text-xs text-slate-400">In stock: {item.stock}</div>
              <button
                onClick={() => addToCart(item)}
                disabled={item.stock === 0}
                className="mt-2 rounded bg-slate-900 px-3 py-1 text-sm text-white disabled:bg-slate-300"
              >
                Add to cart
              </button>
            </li>
          ))}
        </ul>
      </section>
      <aside className="rounded-lg border border-slate-200 bg-white p-4">
        <h2 className="mb-2 font-semibold">Cart</h2>
        {cart.length === 0 && <p className="text-sm text-slate-500">Empty.</p>}
        <ul className="space-y-1 text-sm">
          {cart.map((line) => (
            <li key={line.item.id} className="flex justify-between">
              <span>
                {line.item.name} × {line.quantity}
              </span>
              <span>{(Number(line.item.price_usdt) * line.quantity).toFixed(2)}</span>
            </li>
          ))}
        </ul>
        {cart.length > 0 && (
          <>
            <div className="mt-3 flex justify-between border-t border-slate-200 pt-2 font-medium">
              <span>Total</span>
              <span>{total} USDT</span>
            </div>
            <button
              disabled
              className="mt-3 w-full rounded bg-slate-300 px-3 py-2 text-sm text-white"
              title="Web3 checkout arrives in D12"
            >
              Pay with USDT (coming soon)
            </button>
          </>
        )}
      </aside>
    </div>
  );
}
