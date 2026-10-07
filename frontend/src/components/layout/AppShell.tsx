import { useAtomValue } from 'jotai';
import {
  Activity,
  AlertTriangle,
  ClipboardList,
  ExternalLink,
  KeyRound,
  Link2,
  Package,
  ShoppingCart,
} from 'lucide-react';
import { NavLink, Outlet, useNavigate } from 'react-router-dom';
import { isAdminAtom, meAtom } from '@/atoms/app';
import { useAppActions } from '@/atoms/appActions';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import { cn } from '@/lib/utils';

const NAV_ITEMS = [
  { to: '/orders', label: '订单工作台', icon: ClipboardList },
  { to: '/purchase-tasks', label: '采购任务台', icon: ShoppingCart },
  { to: '/exceptions', label: '异常池', icon: AlertTriangle },
  { to: '/catalog', label: '映射与报价', icon: Link2 },
  { to: '/shipments', label: '中转点与打包', icon: Package },
  { to: '/stores', label: '店铺与凭据', icon: KeyRound },
  { to: '/system', label: '系统状态', icon: Activity },
];

export function AppShell() {
  const me = useAtomValue(meAtom);
  const isAdmin = useAtomValue(isAdminAtom);
  const { logout } = useAppActions();
  const navigate = useNavigate();

  return (
    <div className='flex min-h-screen bg-muted/30'>
      <aside className='flex w-52 shrink-0 flex-col border-r bg-background'>
        <div className='flex h-12 items-center border-b px-4 text-sm font-semibold'>履约中台</div>
        <nav className='flex-1 space-y-1 p-2'>
          {NAV_ITEMS.map((item) => (
            <NavLink
              key={item.to}
              to={item.to}
              className={({ isActive }) =>
                cn(
                  'flex items-center gap-2 rounded-md px-3 py-2 text-sm text-muted-foreground hover:bg-accent hover:text-accent-foreground',
                  isActive && 'bg-accent font-medium text-accent-foreground',
                )}
            >
              <item.icon className='size-4' />
              {item.label}
            </NavLink>
          ))}
          {isAdmin && (
            // asynq 队列监控是后端自带的独立页面（/api/admin/queues，仅 admin），新开窗口。
            <a
              href='/api/admin/queues'
              target='_blank'
              rel='noreferrer'
              className='flex items-center gap-2 rounded-md px-3 py-2 text-sm text-muted-foreground hover:bg-accent hover:text-accent-foreground'
            >
              <Activity className='size-4' />
              队列监控
              <ExternalLink className='ml-auto size-3.5' />
            </a>
          )}
        </nav>
      </aside>

      <div className='flex min-w-0 flex-1 flex-col'>
        <header className='flex h-12 items-center justify-end border-b bg-background px-4'>
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant='ghost' size='sm' className='gap-2'>
                <span>{me?.name ?? '—'}</span>
                <Badge variant='secondary'>{me?.role === 'admin' ? '管理员' : '操作员'}</Badge>
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align='end'>
              <DropdownMenuLabel>{me?.name}</DropdownMenuLabel>
              <DropdownMenuSeparator />
              <DropdownMenuItem
                onClick={() => {
                  void logout().then(() => navigate('/login', { replace: true }));
                }}
              >
                退出登录
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </header>
        <main className='min-w-0 flex-1 p-6'>
          <Outlet />
        </main>
      </div>
    </div>
  );
}
