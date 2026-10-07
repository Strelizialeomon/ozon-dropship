import { useAtom, useAtomValue } from 'jotai';
import { useState } from 'react';
import { toast } from 'sonner';
import { Button } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { useTaskActions, useTaskListActions } from '../actions';
import { taskFiltersAtom, taskMarkPaidTargetAtom } from '../store';

/** 「记已付款」：S1 无免密支付——人工在 1688 付款后回填实付，任务 ordered → paid（总纲 §5.1）。 */
export function MarkPaidDialog() {
  const [target, setTarget] = useAtom(taskMarkPaidTargetAtom);
  const filters = useAtomValue(taskFiltersAtom);
  const { markPaid } = useTaskActions();
  const { loadTasks } = useTaskListActions();
  const [amount, setAmount] = useState('');
  const [platformOrderId, setPlatformOrderId] = useState('');
  const [error, setError] = useState('');

  async function onSubmit(): Promise<void> {
    if (!target) return;
    if (!amount.trim() || Number.isNaN(Number(amount))) {
      setError('请填写实付金额（数字）');
      return;
    }
    const ok = await markPaid(target.id, {
      amount: amount.trim(),
      platform_order_id: platformOrderId.trim() || undefined,
    });
    if (ok) {
      toast.success('已记付款');
      setTarget(null);
      setAmount('');
      setPlatformOrderId('');
      setError('');
      void loadTasks(filters);
    }
  }

  return (
    <Dialog open={!!target} onOpenChange={(open) => !open && setTarget(null)}>
      {target && (
        <DialogContent className='max-w-sm'>
          <DialogHeader>
            <DialogTitle>记已付款</DialogTitle>
            <DialogDescription>
              {target.posting_number} · 人工确认 1688 已付款后回填实付金额（可经 GetOrder 核对）。
            </DialogDescription>
          </DialogHeader>
          <div className='space-y-4'>
            <div className='space-y-2'>
              <Label htmlFor='paid-amount'>实付金额</Label>
              <Input
                id='paid-amount'
                placeholder='如 128.50'
                value={amount}
                onChange={(e) => setAmount(e.target.value)}
              />
            </div>
            <div className='space-y-2'>
              <Label htmlFor='paid-order-id'>1688 订单号（可空）</Label>
              <Input id='paid-order-id' value={platformOrderId} onChange={(e) => setPlatformOrderId(e.target.value)} />
            </div>
            {error && <p className='text-sm text-destructive'>{error}</p>}
          </div>
          <DialogFooter>
            <Button variant='outline' onClick={() => setTarget(null)}>
              取消
            </Button>
            <Button onClick={() => void onSubmit()}>保存</Button>
          </DialogFooter>
        </DialogContent>
      )}
    </Dialog>
  );
}
