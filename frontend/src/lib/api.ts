// Typed fetch wrapper for the Shop backend. Keeps every page free of URL and
// JSON-parsing boilerplate.

// fmtUsdt trims the database's fixed-point precision ("165.000000000000000000")
// to a clean display value ("165"), leaving real decimals intact ("9.99").
export const fmtUsdt = (s: string) => {
  const n = Number(s);
  return Number.isFinite(n) ? n.toString() : s;
};

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
  status: string;
  item_id?: string;
  item_quantity?: number;
  created_at: string;
};

// ShopInfo carries the on-chain payment parameters the storefront needs to
// build the USDT transfer (served by GET /api/shop-info).
export type ShopInfo = {
  wallet_address: string;
  token_contract: string;
  token_decimals: number;
  chain_id: number;
};

export type NewOrder = {
  id: string;
  buyer_wallet: string;
  tx_hash: string;
  amount_usdt: string;
  item_id: string;
  item_quantity: number;
};

// ApiError keeps the HTTP status so callers can react to 401 (expired or
// missing admin token) by sending the user back to the login page.
export class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

const TOKEN_KEY = 'shop_admin_token';

export const adminToken = {
  get: () => localStorage.getItem(TOKEN_KEY),
  set: (t: string) => localStorage.setItem(TOKEN_KEY, t),
  clear: () => localStorage.removeItem(TOKEN_KEY),
};

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const headers: Record<string, string> = { 'Content-Type': 'application/json' };
  const token = adminToken.get();
  if (token) {
    headers.Authorization = `Bearer ${token}`;
  }
  const res = await fetch(path, { headers, ...init });
  if (!res.ok) {
    const body = await res.text();
    throw new ApiError(res.status, `${res.status} ${res.statusText}: ${body}`);
  }
  if (res.status === 204) {
    return undefined as T;
  }
  return res.json() as Promise<T>;
}

export const api = {
  // login exchanges the shop's admin password for a bearer token used on
  // item writes and the order list.
  login: async (password: string) => {
    const { token } = await request<{ token: string }>('/api/auth/login', {
      method: 'POST',
      body: JSON.stringify({ password }),
    });
    adminToken.set(token);
  },
  listItems: () => request<Item[]>('/api/items'),
  createItem: (item: Item) =>
    request<Item>('/api/items', { method: 'POST', body: JSON.stringify(item) }),
  updateItem: (id: string, item: Omit<Item, 'id'>) =>
    request<Item>(`/api/items/${id}`, { method: 'PUT', body: JSON.stringify(item) }),
  deleteItem: (id: string) =>
    request<void>(`/api/items/${id}`, { method: 'DELETE' }),
  listOrders: () => request<Order[]>('/api/orders'),
  getOrder: (id: string) => request<Order>(`/api/orders/${id}`),
  createOrder: (order: NewOrder) =>
    request<Order>('/api/orders', { method: 'POST', body: JSON.stringify(order) }),
  shopInfo: () => request<ShopInfo>('/api/shop-info'),
};
