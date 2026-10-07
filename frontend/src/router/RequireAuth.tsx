import { useEffect, useState } from 'react';
import { Navigate, Outlet, useLocation } from 'react-router-dom';
import { setUnauthorizedListener } from '@/api/client';
import { useAppActions } from '@/atoms/appActions';

// 登录守卫：没会话就送回登录页。做法是 <Routes> 内的布局路由（登录页是它的同级兄弟）——
// 页面之间来回切时守卫不卸载，401 监听也就只注册一次。功能上等于「守卫罩住除登录页外的全部路由」
// （spec §6 的「包在 <Routes> 外层」就是这个意思，落到声明式路由里是布局路由这条写法）。

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
