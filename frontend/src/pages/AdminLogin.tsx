import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { ShieldCheck } from 'lucide-react';

// Placeholder admin login. Real JWT auth lands with D13; for now any non-empty
// credentials grant access so the admin dashboard is reachable in the demo.
export default function AdminLogin() {
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const nav = useNavigate();

  function submit(e: React.FormEvent) {
    e.preventDefault();
    if (email && password) {
      nav('/admin');
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
              <label className="mb-1.5 block text-[13px] font-medium text-fg/80">Email</label>
              <input
                type="email"
                required
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                placeholder="admin@shop.local"
                className={field}
              />
            </div>
            <div>
              <label className="mb-1.5 block text-[13px] font-medium text-fg/80">Password</label>
              <input
                type="password"
                required
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                placeholder="••••••••"
                className={field}
              />
            </div>
            <button
              type="submit"
              className="btn-gradient h-11 w-full rounded-lg text-[15px] font-medium"
            >
              Sign in
            </button>
          </form>

          <p className="mt-6 text-center text-xs text-faint">
            Demo gate — any credentials work (admin auth is owner-side only).
          </p>
        </div>
      </div>
    </div>
  );
}
