import { useState } from 'react';
import { useNavigate } from 'react-router-dom';

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

  return (
    <div className="mx-auto max-w-sm rounded-lg border border-slate-200 bg-white p-6">
      <h1 className="mb-4 text-lg font-semibold">Admin login</h1>
      <form onSubmit={submit} className="space-y-3">
        <label className="block">
          <span className="text-sm text-slate-600">Email</span>
          <input
            type="email"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            className="mt-1 w-full rounded border border-slate-300 px-2 py-1 text-sm"
            required
          />
        </label>
        <label className="block">
          <span className="text-sm text-slate-600">Password</span>
          <input
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            className="mt-1 w-full rounded border border-slate-300 px-2 py-1 text-sm"
            required
          />
        </label>
        <button type="submit" className="w-full rounded bg-slate-900 px-3 py-2 text-sm text-white">
          Sign in
        </button>
      </form>
      <p className="mt-3 text-xs text-slate-400">
        Stub login. Replace with backend JWT in D13.
      </p>
    </div>
  );
}
