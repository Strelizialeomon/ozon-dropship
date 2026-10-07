import { zodResolver } from '@hookform/resolvers/zod';
import { useAtom, useAtomValue } from 'jotai';
import { useEffect } from 'react';
import { useForm } from 'react-hook-form';
import { toast } from 'sonner';
import { z } from 'zod';
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
import { taskActionSubmittingAtom, taskBackfillTargetAtom } from '../store';

// 正则与后端逐字一致（backend/internal/purchase/manual.go:19-22）：
// 国内快递号 6–32 位字母数字或连字符；平台单号给了就校（6–64 位字母数字）。
const TRACKING_RE = /^[A-Za-z0-9-]{6,32}$/;
const PLATFORM_ORDER_RE = /^[A-Za-z0-9]{6,64}$/;

const schema = z.object({
  domestic_tracking_no: z.string().regex(TRACKING_RE, '国内快递号格式不对（6–32 位字母/数字/连字符）'),
  platform_order_id: z.string().refine(
    (v) => v === '' || PLATFORM_ORDER_RE.test(v),
    '平台单号格式不对（6–64 位字母数字）',
  ),
  amount: z.string().refine(
    (v) => v === '' || (!Number.isNaN(Number(v)) && Number(v) > 0),
    '实付金额要填大于 0 的数字',
  ),
  domestic_carrier: z.string(),
});

type BackfillForm = z.infer<typeof schema>;

const EMPTY: BackfillForm = { domestic_tracking_no: '', platform_order_id: '', amount: '', domestic_carrier: '' };

/** 回填：国内快递号必填；平台单号/实付在采购单里还没有时必须补（后端口径，前端提示保持一致）。 */
export function BackfillDialog() {
  const [target, setTarget] = useAtom(taskBackfillTargetAtom);
  const submitting = useAtomValue(taskActionSubmittingAtom);
  const { fillBack } = useTaskActions();
  const { reloadTasks } = useTaskListActions();

  const form = useForm<BackfillForm>({ resolver: zodResolver(schema), defaultValues: EMPTY });

  // 每次打开都重置（评审路二 #1：取消不把上一条的单号/金额带进来）
  useEffect(() => {
    if (target) form.reset(EMPTY);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [target]);

  async function onSubmit(values: BackfillForm): Promise<void> {
    if (!target) return;
    const ok = await fillBack(target.id, {
      domestic_tracking_no: values.domestic_tracking_no.trim(),
      platform_order_id: values.platform_order_id.trim() || undefined,
      amount: values.amount.trim() || undefined,
      domestic_carrier: values.domestic_carrier.trim() || undefined,
    });
    if (ok) {
      toast.success('已回填');
      setTarget(null);
      void reloadTasks();
    }
  }

  return (
    <Dialog open={!!target} onOpenChange={(open) => !open && setTarget(null)}>
      {target && (
        <DialogContent className='max-w-md'>
          <DialogHeader>
            <DialogTitle>回填</DialogTitle>
            <DialogDescription>
              {target.payload?.posting_number ?? target.id}{' '}
              · 国内快递号只在系统内跟踪到中转点，不回传 Ozon。 若这笔采购还没有平台单号 /
              实付，请一并补上（后端会拒）。
            </DialogDescription>
          </DialogHeader>
          <form className='space-y-4' onSubmit={form.handleSubmit(onSubmit)}>
            <div className='space-y-2'>
              <Label htmlFor='bf-tracking'>国内快递号</Label>
              <Input id='bf-tracking' autoFocus {...form.register('domestic_tracking_no')} />
              {form.formState.errors.domestic_tracking_no && (
                <p className='text-sm text-destructive'>{form.formState.errors.domestic_tracking_no.message}</p>
              )}
            </div>
            <div className='grid grid-cols-2 gap-4'>
              <div className='space-y-2'>
                <Label htmlFor='bf-order-id'>平台单号（可空）</Label>
                <Input id='bf-order-id' {...form.register('platform_order_id')} />
                {form.formState.errors.platform_order_id && (
                  <p className='text-sm text-destructive'>{form.formState.errors.platform_order_id.message}</p>
                )}
              </div>
              <div className='space-y-2'>
                <Label htmlFor='bf-amount'>实付金额（可空）</Label>
                <Input id='bf-amount' {...form.register('amount')} />
                {form.formState.errors.amount && (
                  <p className='text-sm text-destructive'>{form.formState.errors.amount.message}</p>
                )}
              </div>
            </div>
            <div className='space-y-2'>
              <Label htmlFor='bf-carrier'>快递公司（可空）</Label>
              <Input id='bf-carrier' {...form.register('domestic_carrier')} />
            </div>
            <DialogFooter>
              <Button type='button' variant='outline' onClick={() => setTarget(null)} disabled={submitting}>
                取消
              </Button>
              <Button type='submit' disabled={submitting}>
                {submitting ? '保存中…' : '保存'}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      )}
    </Dialog>
  );
}
