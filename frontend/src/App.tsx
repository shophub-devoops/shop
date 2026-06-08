import { Link, Route, Routes } from 'react-router-dom';
import { Store } from 'lucide-react';
import Storefront from './pages/Storefront';
import AdminLogin from './pages/AdminLogin';
import AdminDashboard from './pages/AdminDashboard';

export default function App() {
  return (
    <div className="min-h-screen bg-bg text-fg">
      <div className="pointer-events-none fixed inset-x-0 top-0 h-[360px] bg-[radial-gradient(50%_60%_at_50%_0%,rgba(133,59,206,0.16),transparent_70%)]" />
      <div className="dot-grid pointer-events-none fixed inset-0 opacity-[0.18]" />

      <header className="sticky top-0 z-40 border-b border-white/5 bg-bg/70 backdrop-blur">
        <nav className="mx-auto flex h-16 max-w-5xl items-center justify-between px-6">
          <Link to="/" className="flex items-center gap-2 text-sm font-medium">
            <div className="grid h-7 w-7 place-items-center rounded-md bg-gradient-to-br from-[#7E28BC] to-[#531AFF]">
              <Store size={15} className="text-white" />
            </div>
            <span className="font-semibold">Shop</span>
            <span className="text-faint">/</span>
            <span className="text-muted">storefront</span>
          </Link>
          <Link
            to="/admin/login"
            className="text-sm font-medium text-muted transition-colors hover:text-fg"
          >
            Admin
          </Link>
        </nav>
      </header>

      <main className="relative mx-auto max-w-5xl px-6 py-10">
        <Routes>
          <Route path="/" element={<Storefront />} />
          <Route path="/admin/login" element={<AdminLogin />} />
          <Route path="/admin" element={<AdminDashboard />} />
        </Routes>
      </main>
    </div>
  );
}
