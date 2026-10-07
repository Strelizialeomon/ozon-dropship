import { useEffect, useState } from 'react';
import { Navigate, Outlet, useLocation } from 'react-router-dom';
import { setUnauthorizedListener } from '@/api/client';
import { useAppActions } from '@/atoms/appActions';

// 登录守卫：没会话就送回登录页（子 spec §6 自定细节：守卫包在 <Routes> 外层，
// 做成布局路由，页面间切换不卸载、401 监听只注册一次）。

export type Gate = 'checking' | 'allowed' | 'anonymous';

export function RequireAuth() {
  const location = useLocation();
  const [gate, setGate] = useState<Gate>('checking');
  const { loadMe } = useAppActions();

  // 进站第一件事问一次「我是谁」：200 放行，401 走 anonymous。
  // ⚠️ 依赖只放 []：loadMe 每次渲染都是新函数，放进去会变成每帧重拉（xhs-analysis 踩过）。
  useEffect(() => {
    let alive = true;
    void loadMe().then((me) => {
      if (alive) setGate(me ? 'allowed' : 'anonymous');
    });
    return () => {
      alive = false;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // 会话中途失效（401）：拦截器喊一声，守卫当场把人送回登录页。
  useEffect(() => {
    setUnauthorizedListener(() => setGate('anonymous'));
    return () => setUnauthorizedListener(null);
  }, []);

  if (gate === 'checking') {
    return <div className='flex min-h-screen items-center justify-center text-muted-foreground'>加载中…</div>;
  }
  if (gate === 'anonymous') {
    // 带上原来想去的地址：登录完回那儿，而不是一律甩回首页。
    return <Navigate to='/login' replace state={{ from: location.pathname + location.search }} />;
  }
  return <Outlet />;
}
