import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { ShieldCheck } from 'lucide-react';
import { api } from '../lib/api';

// Admin login: exchanges the shop's admin password (shown to the owner in
// ShopHub) for a bearer token via POST /api/auth/login.
export default function AdminLogin() {
  const [password, setPassword] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const nav = useNavigate();

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      await api.login(password);
      nav('/admin');
    } catch {
      setError('Invalid password.');
    } finally {
      setBusy(false);
    }
  }

  const field =
    'w-full rounded-lg border border-line bg-surface px-3.5 py-2.5 text-sm outline-none transition-colors placeholder:text-faint focus:border-accent-bright';

  return (
    <div className="grid min-h-[70vh] place-items-center">
      <div className="w-full max-w-[400px] animate-fade-up">
        <div className="mb-8 flex items-center justify-center gap-2">
          <div className="grid h-8 w-8 place-items-center rounded-md bg-gradient-to-br from-[#7E28BC] to-[#531AFF]">
            <ShieldCheck size={17} className="text-white" />
          </div>
          <span className="text-lg font-semibold tracking-tight">Shop Admin</span>
        </div>

        <div className="rounded-2xl border border-white/10 bg-card/80 p-8 backdrop-blur">
          <h1 className="text-center font-serif text-[26px] font-medium">Welcome back</h1>
          <p className="mt-2 text-center text-sm text-muted">Sign in to manage items and orders</p>

          <form onSubmit={submit} className="mt-7 space-y-4">
            <div>
              <label className="mb-1.5 block text-[13px] font-medium text-fg/80">Admin password</label>
              <input
                type="password"
                required
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                placeholder="••••••••"
                className={field}
              />
            </div>
            {error && <p className="text-sm text-red-400">{error}</p>}
            <button
              type="submit"
              disabled={busy}
              className="btn-gradient h-11 w-full rounded-lg text-[15px] font-medium disabled:opacity-60"
            >
              {busy ? 'Signing in…' : 'Sign in'}
            </button>
          </form>

          <p className="mt-6 text-center text-xs text-faint">
            The admin password is shown to the shop owner in the ShopHub dashboard.
          </p>
        </div>
      </div>
    </div>
  );
}
