import { useEffect, useState } from 'react';
import { api, type Item, type Order } from '../lib/api';

const empty: Item = { id: '', name: '', price_usdt: '0', stock: 0 };

export default function AdminDashboard() {
  const [items, setItems] = useState<Item[]>([]);
  const [orders, setOrders] = useState<Order[]>([]);
  const [draft, setDraft] = useState<Item>(empty);
  const [error, setError] = useState<string | null>(null);

  function refresh() {
    api.listItems().then(setItems).catch((e) => setError(String(e)));
    api.listOrders().then(setOrders).catch((e) => setError(String(e)));
  }

  useEffect(refresh, []);

  async function createItem(e: React.FormEvent) {
    e.preventDefault();
    try {
      await api.createItem(draft);
      setDraft(empty);
      refresh();
    } catch (e) {
      setError(String(e));
    }
  }

  async function deleteItem(id: string) {
    try {
      await api.deleteItem(id);
      refresh();
    } catch (e) {
      setError(String(e));
    }
  }

  return (
    <div className="space-y-8">
      {error && <p className="text-red-600">{error}</p>}

      <section>
        <h1 className="mb-3 text-xl font-semibold">Items</h1>
        <form onSubmit={createItem} className="mb-4 grid gap-2 sm:grid-cols-5">
          <input
            placeholder="id"
            value={draft.id}
            onChange={(e) => setDraft({ ...draft, id: e.target.value })}
            className="rounded border border-slate-300 px-2 py-1 text-sm"
            required
          />
          <input
            placeholder="name"
            value={draft.name}
            onChange={(e) => setDraft({ ...draft, name: e.target.value })}
            className="rounded border border-slate-300 px-2 py-1 text-sm"
            required
          />
          <input
            placeholder="price USDT"
            value={draft.price_usdt}
            onChange={(e) => setDraft({ ...draft, price_usdt: e.target.value })}
            className="rounded border border-slate-300 px-2 py-1 text-sm"
            required
          />
          <input
            type="number"
            placeholder="stock"
            value={draft.stock}
            onChange={(e) => setDraft({ ...draft, stock: Number(e.target.value) })}
            className="rounded border border-slate-300 px-2 py-1 text-sm"
            required
          />
          <button type="submit" className="rounded bg-slate-900 px-3 py-1 text-sm text-white">
            Add
          </button>
        </form>
        <table className="w-full text-sm">
          <thead className="text-left text-slate-500">
            <tr>
              <th className="py-1">ID</th>
              <th>Name</th>
              <th>Price</th>
              <th>Stock</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {items.map((it) => (
              <tr key={it.id} className="border-t border-slate-200">
                <td className="py-1">{it.id}</td>
                <td>{it.name}</td>
                <td>{it.price_usdt}</td>
                <td>{it.stock}</td>
                <td>
                  <button
                    onClick={() => deleteItem(it.id)}
                    className="text-xs text-red-600 hover:underline"
                  >
                    Delete
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </section>

      <section>
        <h2 className="mb-3 text-xl font-semibold">Orders</h2>
        <table className="w-full text-sm">
          <thead className="text-left text-slate-500">
            <tr>
              <th className="py-1">ID</th>
              <th>Buyer</th>
              <th>Amount</th>
              <th>Tx</th>
              <th>Created</th>
            </tr>
          </thead>
          <tbody>
            {orders.map((o) => (
              <tr key={o.id} className="border-t border-slate-200">
                <td className="py-1">{o.id}</td>
                <td className="font-mono text-xs">{o.buyer_wallet}</td>
                <td>{o.amount_usdt} USDT</td>
                <td className="font-mono text-xs">{o.tx_hash ?? '—'}</td>
                <td>{new Date(o.created_at).toLocaleString()}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </section>
    </div>
  );
}
