import { useAtomValue } from 'jotai';
import { ExternalLink, RefreshCw } from 'lucide-react';
import { useEffect } from 'react';
import { isAdminAtom } from '@/atoms/app';
import { PageHeader } from '@/components/PageHeader';
import { StatusBadge } from '@/components/StatusBadge';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { formatDateTime } from '@/lib/format';
import { shopStatus } from '@/lib/labels';
import { useSystemActions } from './actions';
import { systemLoadingAtom, systemStatusAtom } from './store';

/** 各店最近一次同步：超过这个分钟数没同步就标黄（轮询间隔默认 5 分钟，总纲 §5.9）。 */
const STALE_MINUTES = 30;

function stale(lastSyncAt: string | null): boolean {
  if (!lastSyncAt) return true;
  return Date.now() - new Date(lastSyncAt).getTime() > STALE_MINUTES * 60_000;
}

export default function SystemPage() {
  const status = useAtomValue(systemStatusAtom);
  const loading = useAtomValue(systemLoadingAtom);
  const isAdmin = useAtomValue(isAdminAtom);
  const { loadStatus } = useSystemActions();

  useEffect(() => {
    void loadStatus();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const queues = Object.entries(status?.queues ?? {});

  return (
    <div>
      <PageHeader
        title='系统状态'
        description='各店最近一次同步、队列积压与失败任务'
        actions={
          <Button variant='outline' size='sm' disabled={loading} onClick={() => void loadStatus()}>
            <RefreshCw className='size-4' />
            刷新
          </Button>
        }
      />

      <div className='grid gap-4 lg:grid-cols-2'>
        <Card>
          <CardHeader>
            <CardTitle className='text-sm'>店铺同步</CardTitle>
          </CardHeader>
          <CardContent>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>店铺</TableHead>
                  <TableHead>状态</TableHead>
                  <TableHead>最近同步成功</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {(status?.stores ?? []).length === 0 && (
                  <TableRow>
                    <TableCell colSpan={3} className='text-center text-muted-foreground'>
                      {loading ? '加载中…' : '暂无店铺'}
                    </TableCell>
                  </TableRow>
                )}
                {(status?.stores ?? []).map((s) => (
                  <TableRow key={s.id}>
                    <TableCell>{s.name}</TableCell>
                    <TableCell>
                      <StatusBadge spec={shopStatus(s.status)} />
                    </TableCell>
                    <TableCell className={stale(s.last_sync_at) ? 'text-amber-700' : undefined}>
                      {formatDateTime(s.last_sync_at)}
                      {stale(s.last_sync_at) && s.last_sync_at && <span className='ml-1 text-xs'>(超 30 分钟)</span>}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className='flex flex-row items-center justify-between'>
            <CardTitle className='text-sm'>任务队列</CardTitle>
            {isAdmin && (
              <Button variant='outline' size='sm' asChild>
                <a href='/api/admin/queues' target='_blank' rel='noreferrer'>
                  队列监控
                  <ExternalLink className='size-3.5' />
                </a>
              </Button>
            )}
          </CardHeader>
          <CardContent>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>队列</TableHead>
                  <TableHead>待处理</TableHead>
                  <TableHead>执行中</TableHead>
                  <TableHead>重试中</TableHead>
                  <TableHead>失败(归档)</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {queues.length === 0 && (
                  <TableRow>
                    <TableCell colSpan={5} className='text-center text-muted-foreground'>
                      {loading ? '加载中…' : '暂无队列'}
                    </TableCell>
                  </TableRow>
                )}
                {queues.map(([name, q]) => (
                  <TableRow key={name}>
                    <TableCell className='font-mono text-xs'>{name}</TableCell>
                    <TableCell>{q.pending}</TableCell>
                    <TableCell>{q.active}</TableCell>
                    <TableCell>{q.retry}</TableCell>
                    <TableCell className={q.archived > 0 ? 'font-medium text-destructive' : undefined}>
                      {q.archived}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </CardContent>
        </Card>

        <Card className='lg:col-span-2'>
          <CardHeader>
            <CardTitle className='text-sm'>失败任务（重试用尽）</CardTitle>
          </CardHeader>
          <CardContent>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>任务</TableHead>
                  <TableHead>队列</TableHead>
                  <TableHead>重试</TableHead>
                  <TableHead>最后一次失败</TableHead>
                  <TableHead>错误</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {(status?.failed_tasks ?? []).length === 0 && (
                  <TableRow>
                    <TableCell colSpan={5} className='text-center text-muted-foreground'>
                      没有失败任务
                    </TableCell>
                  </TableRow>
                )}
                {(status?.failed_tasks ?? []).map((t) => (
                  <TableRow key={t.id}>
                    <TableCell className='font-mono text-xs'>{t.type}</TableCell>
                    <TableCell>{t.queue}</TableCell>
                    <TableCell>
                      {t.retried}/{t.max_retry}
                    </TableCell>
                    <TableCell>{formatDateTime(t.last_failed_at)}</TableCell>
                    <TableCell className='max-w-md truncate text-destructive' title={t.last_error}>
                      {t.last_error}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
