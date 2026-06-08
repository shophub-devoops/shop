import { useEffect, useState } from 'react';
import { Package, Plus, Receipt, Trash2 } from 'lucide-react';
import { api, fmtUsdt, type Item, type Order } from '../lib/api';

const empty: Item = { id: '', name: '', price_usdt: '0', stock: 0 };

function StatusBadge({ status }: { status: string }) {
  const tone =
    status === 'confirmed'
      ? 'bg-emerald-500/15 text-emerald-300'
      : status === 'failed'
        ? 'bg-red-500/15 text-red-300'
        : 'bg-white/5 text-muted';
  return <span className={`rounded-md px-2 py-0.5 text-xs font-medium ${tone}`}>{status}</span>;
}

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

  const field =
    'rounded-lg border border-line bg-surface px-3 py-2 text-sm outline-none transition-colors placeholder:text-faint focus:border-accent-bright';

  return (
    <div className="animate-fade-up space-y-10">
      <div>
        <h1 className="font-serif text-[28px] font-medium">Admin dashboard</h1>
        <p className="mt-1 text-sm text-muted">Manage your catalogue and review incoming orders.</p>
      </div>

      {error && <p className="text-sm text-red-400">{error}</p>}

      {/* Items */}
      <section className="rounded-xl border border-line bg-card p-5">
        <h2 className="mb-4 flex items-center gap-2 font-serif text-lg font-medium">
          <Package size={17} className="text-accent-bright" /> Items
        </h2>

        <form onSubmit={createItem} className="mb-5 grid gap-2 sm:grid-cols-[1fr_1fr_1fr_90px_auto]">
          <input placeholder="id" value={draft.id} onChange={(e) => setDraft({ ...draft, id: e.target.value })} className={field} required />
          <input placeholder="name" value={draft.name} onChange={(e) => setDraft({ ...draft, name: e.target.value })} className={field} required />
          <input placeholder="price USDT" value={draft.price_usdt} onChange={(e) => setDraft({ ...draft, price_usdt: e.target.value })} className={field} required />
          <input type="number" placeholder="stock" value={draft.stock} onChange={(e) => setDraft({ ...draft, stock: Number(e.target.value) })} className={field} required />
          <button
            type="submit"
            className="inline-flex items-center justify-center gap-1.5 rounded-lg bg-accent px-4 text-sm font-medium text-white transition-[filter] hover:brightness-110"
          >
            <Plus size={15} /> Add
          </button>
        </form>

        {items.length === 0 ? (
          <p className="py-6 text-center text-sm text-muted">No items yet — add your first product above.</p>
        ) : (
          <div className="overflow-hidden rounded-lg border border-line">
            <table className="w-full text-sm">
              <thead className="bg-surface text-left text-xs uppercase tracking-wide text-faint">
                <tr>
                  <th className="px-4 py-2.5">ID</th>
                  <th className="px-4 py-2.5">Name</th>
                  <th className="px-4 py-2.5">Price</th>
                  <th className="px-4 py-2.5">Stock</th>
                  <th className="px-4 py-2.5"></th>
                </tr>
              </thead>
              <tbody>
                {items.map((it) => (
                  <tr key={it.id} className="border-t border-line/60 hover:bg-white/[0.02]">
                    <td className="px-4 py-2.5 font-mono text-xs text-muted">{it.id}</td>
                    <td className="px-4 py-2.5 font-medium">{it.name}</td>
                    <td className="px-4 py-2.5">{fmtUsdt(it.price_usdt)} USDT</td>
                    <td className="px-4 py-2.5">{it.stock}</td>
                    <td className="px-4 py-2.5 text-right">
                      <button
                        onClick={() => deleteItem(it.id)}
                        className="text-faint transition-colors hover:text-red-400"
                        title="Delete item"
                      >
                        <Trash2 size={15} />
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>

      {/* Orders */}
      <section className="rounded-xl border border-line bg-card p-5">
        <h2 className="mb-4 flex items-center gap-2 font-serif text-lg font-medium">
          <Receipt size={17} className="text-accent-bright" /> Orders
        </h2>

        {orders.length === 0 ? (
          <p className="py-6 text-center text-sm text-muted">No orders yet.</p>
        ) : (
          <div className="overflow-x-auto rounded-lg border border-line">
            <table className="w-full text-sm">
              <thead className="bg-surface text-left text-xs uppercase tracking-wide text-faint">
                <tr>
                  <th className="px-4 py-2.5">Buyer</th>
                  <th className="px-4 py-2.5">Amount</th>
                  <th className="px-4 py-2.5">Status</th>
                  <th className="px-4 py-2.5">Tx</th>
                  <th className="px-4 py-2.5">Created</th>
                </tr>
              </thead>
              <tbody>
                {orders.map((o) => (
                  <tr key={o.id} className="border-t border-line/60 hover:bg-white/[0.02]">
                    <td className="px-4 py-2.5 font-mono text-xs text-muted">
                      {o.buyer_wallet.slice(0, 6)}…{o.buyer_wallet.slice(-4)}
                    </td>
                    <td className="px-4 py-2.5">{fmtUsdt(o.amount_usdt)} USDT</td>
                    <td className="px-4 py-2.5"><StatusBadge status={o.status} /></td>
                    <td className="px-4 py-2.5 font-mono text-xs text-muted">
                      {o.tx_hash ? `${o.tx_hash.slice(0, 8)}…` : '—'}
                    </td>
                    <td className="px-4 py-2.5 text-muted">{new Date(o.created_at).toLocaleString()}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>
    </div>
  );
}
