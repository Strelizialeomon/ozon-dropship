import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom';
import { Toaster } from '@/components/ui/sonner';
import { AppShell } from '@/components/layout/AppShell';
import CatalogPage from '@/pages/catalog';
import ExceptionsPage from '@/pages/exceptions';
import LoginPage from '@/pages/login';
import OrdersPage from '@/pages/orders';
import PurchaseTasksPage from '@/pages/purchase-tasks';
import ShipmentsPage from '@/pages/shipments';
import StoresPage from '@/pages/stores';
import SystemPage from '@/pages/system';
import { RequireAuth } from './RequireAuth';

export default function Router() {
  return (
    <BrowserRouter>
      <Routes>
        {/* 登录页在守卫之外——守卫要往它身上跳 */}
        <Route path='/login' element={<LoginPage />} />

        {/* 其余全部要登录：守卫 → 应用壳（侧边栏 + 顶栏） */}
        <Route element={<RequireAuth />}>
          <Route element={<AppShell />}>
            <Route path='/' element={<Navigate to='/orders' replace />} />
            <Route path='/orders' element={<OrdersPage />} />
            <Route path='/purchase-tasks' element={<PurchaseTasksPage />} />
            <Route path='/exceptions' element={<ExceptionsPage />} />
            <Route path='/catalog' element={<CatalogPage />} />
            <Route path='/shipments' element={<ShipmentsPage />} />
            <Route path='/stores' element={<StoresPage />} />
            <Route path='/system' element={<SystemPage />} />
            <Route path='*' element={<div className='p-8 text-muted-foreground'>页面不存在</div>} />
          </Route>
        </Route>
      </Routes>
      <Toaster position='top-center' richColors />
    </BrowserRouter>
  );
}
