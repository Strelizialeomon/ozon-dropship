import { useAtom, useAtomValue } from 'jotai';
import type { ReactNode } from 'react';
import { StatusBadge } from '@/components/StatusBadge';
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { Skeleton } from '@/components/ui/skeleton';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { formatDateTime, formatMoney } from '@/lib/format';
import { orderStatus, tplIntegration } from '@/lib/labels';
import { orderDetailAtom, orderDetailLoadingAtom } from '../store';

function Field({ label, value }: { label: string; value: ReactNode }) {
  return (
    <div className='space-y-0.5'>
      <div className='text-xs text-muted-foreground'>{label}</div>
      <div className='text-sm'>{value}</div>
    </div>
  );
}

export function OrderDetailDialog() {
  const [detail, setDetail] = useAtom(orderDetailAtom);
  const loading = useAtomValue(orderDetailLoadingAtom);
  const order = detail?.order;

  return (
    <Dialog open={!!order || loading} onOpenChange={(open) => !open && setDetail(null)}>
      {(order || loading) && (
        <DialogContent className='max-w-2xl'>
          {loading || !order
            ? (
              <div className='space-y-3'>
                <Skeleton className='h-6 w-64' />
                <Skeleton className='h-24 w-full' />
              </div>
            )
            : (
              <>
                <DialogHeader>
                  <DialogTitle className='flex items-center gap-2'>
                    <span className='font-mono'>{order.posting_number}</span>
                    <StatusBadge spec={orderStatus(order.status)} />
                  </DialogTitle>
                  <DialogDescription>
                    Ozon 单号 {order.order_number}
                    {order.parent_posting_number ? ` · 母包裹 ${order.parent_posting_number}` : ''}
                  </DialogDescription>
                </DialogHeader>

                <div className='grid grid-cols-3 gap-4'>
                  <Field
                    label='Ozon 状态'
                    value={`${order.ozon_status}${order.ozon_substatus ? ` / ${order.ozon_substatus}` : ''}`}
                  />
                  <Field label='发货截止' value={formatDateTime(order.ship_deadline)} />
                  <Field label='物流方式' value={tplIntegration(order.tpl_integration_type)} />
                  <Field label='中转点' value={order.relay_point_id ?? '未指定'} />
                  <Field label='金额合计' value={formatMoney(order.total_amount, order.currency)} />
                  <Field label='入库时间' value={formatDateTime(order.created_at)} />
                </div>

                <div className='overflow-hidden rounded-md border'>
                  <Table>
                    <TableHeader>
                      <TableRow>
                        <TableHead>Ozon 商品</TableHead>
                        <TableHead>数量</TableHead>
                        <TableHead>单价</TableHead>
                        <TableHead>映射</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {detail.items.length === 0 && (
                        <TableRow>
                          <TableCell colSpan={4} className='text-center text-muted-foreground'>
                            没有明细
                          </TableCell>
                        </TableRow>
                      )}
                      {detail.items.map((it) => (
                        <TableRow key={it.id}>
                          <TableCell className='font-mono text-xs'>{it.ozon_offer_id}</TableCell>
                          <TableCell>{it.qty}</TableCell>
                          <TableCell>{formatMoney(it.price, it.currency)}</TableCell>
                          <TableCell>{it.offer_link_id ? '已映射' : '未映射'}</TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>
                </div>
              </>
            )}
        </DialogContent>
      )}
    </Dialog>
  );
}
