// Typed fetch wrapper for the Shop backend. Keeps every page free of URL and
// JSON-parsing boilerplate.

export type Item = {
  id: string;
  name: string;
  price_usdt: string;
  stock: number;
};

export type Order = {
  id: string;
  buyer_wallet: string;
  tx_hash: string | null;
  amount_usdt: string;
  created_at: string;
};

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, {
    headers: { 'Content-Type': 'application/json' },
    ...init,
  });
  if (!res.ok) {
    const body = await res.text();
    throw new Error(`${res.status} ${res.statusText}: ${body}`);
  }
  if (res.status === 204) {
    return undefined as T;
  }
  return res.json() as Promise<T>;
}

export const api = {
  listItems: () => request<Item[]>('/api/items'),
  createItem: (item: Item) =>
    request<Item>('/api/items', { method: 'POST', body: JSON.stringify(item) }),
  updateItem: (id: string, item: Omit<Item, 'id'>) =>
    request<Item>(`/api/items/${id}`, { method: 'PUT', body: JSON.stringify(item) }),
  deleteItem: (id: string) =>
    request<void>(`/api/items/${id}`, { method: 'DELETE' }),
  listOrders: () => request<Order[]>('/api/orders'),
};
