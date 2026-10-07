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
import { taskBackfillTargetAtom, taskFiltersAtom } from '../store';

// 国内快递号格式：字母数字 + 常见分隔符，8–32 位（后端最终校验；这里先挡明显错的，总纲 §7.3）。
const TRACKING_RE = /^[A-Za-z0-9-]{8,32}$/;

/** 回填（人工渠道 / 人工执行器）：平台单号 + 实付 + 国内快递号（国内单号不回传 Ozon）。 */
export function BackfillDialog() {
  const [target, setTarget] = useAtom(taskBackfillTargetAtom);
  const filters = useAtomValue(taskFiltersAtom);
  const { backfill } = useTaskActions();
  const { loadTasks } = useTaskListActions();
  const [platformOrderId, setPlatformOrderId] = useState('');
  const [amount, setAmount] = useState('');
  const [carrier, setCarrier] = useState('');
  const [trackingNo, setTrackingNo] = useState('');
  const [error, setError] = useState('');

  async function onSubmit(): Promise<void> {
    if (!target) return;
    if (!platformOrderId.trim()) {
      setError('请填写平台订单号');
      return;
    }
    if (!amount.trim() || Number.isNaN(Number(amount))) {
      setError('请填写实付金额（数字）');
      return;
    }
    if (!TRACKING_RE.test(trackingNo.trim())) {
      setError('国内快递号格式不对（8–32 位字母/数字/连字符）');
      return;
    }
    const ok = await backfill(target.id, {
      platform_order_id: platformOrderId.trim(),
      amount: amount.trim(),
      domestic_carrier: carrier.trim() || undefined,
      domestic_tracking_no: trackingNo.trim(),
    });
    if (ok) {
      toast.success('已回填');
      setTarget(null);
      setPlatformOrderId('');
      setAmount('');
      setCarrier('');
      setTrackingNo('');
      setError('');
      void loadTasks(filters);
    }
  }

  return (
    <Dialog open={!!target} onOpenChange={(open) => !open && setTarget(null)}>
      {target && (
        <DialogContent className='max-w-md'>
          <DialogHeader>
            <DialogTitle>回填</DialogTitle>
            <DialogDescription>
              {target.posting_number} · 平台单号 / 实付 / 国内快递号。国内快递号只在系统内跟踪到中转点，不回传 Ozon。
            </DialogDescription>
          </DialogHeader>
          <div className='space-y-4'>
            <div className='space-y-2'>
              <Label htmlFor='bf-order-id'>平台订单号</Label>
              <Input id='bf-order-id' value={platformOrderId} onChange={(e) => setPlatformOrderId(e.target.value)} />
            </div>
            <div className='space-y-2'>
              <Label htmlFor='bf-amount'>实付金额</Label>
              <Input id='bf-amount' value={amount} onChange={(e) => setAmount(e.target.value)} />
            </div>
            <div className='grid grid-cols-2 gap-4'>
              <div className='space-y-2'>
                <Label htmlFor='bf-carrier'>快递公司（可空）</Label>
                <Input id='bf-carrier' value={carrier} onChange={(e) => setCarrier(e.target.value)} />
              </div>
              <div className='space-y-2'>
                <Label htmlFor='bf-tracking'>国内快递号</Label>
                <Input id='bf-tracking' value={trackingNo} onChange={(e) => setTrackingNo(e.target.value)} />
              </div>
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
