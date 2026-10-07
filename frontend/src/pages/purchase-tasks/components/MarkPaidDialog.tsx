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
import { taskActionSubmittingAtom, taskMarkPaidTargetAtom } from '../store';

// 金额口径与后端一致：> 0（backend/internal/purchase/manual.go:190-192）
const schema = z.object({
  amount: z
    .string()
    .min(1, '请填写实付金额')
    .refine((v) => !Number.isNaN(Number(v)) && Number(v) > 0, '实付金额必须是大于 0 的数字'),
  paid_at: z.string(),
});

type MarkPaidForm = z.infer<typeof schema>;

const EMPTY: MarkPaidForm = { amount: '', paid_at: '' };

/** 「记已付款」：S1 无免密支付——人工在 1688 付款后回填实付，任务 ordered → paid（总纲 §5.1）。 */
export function MarkPaidDialog() {
  const [target, setTarget] = useAtom(taskMarkPaidTargetAtom);
  const submitting = useAtomValue(taskActionSubmittingAtom);
  const { markPaid } = useTaskActions();
  const { reloadTasks } = useTaskListActions();

  const form = useForm<MarkPaidForm>({ resolver: zodResolver(schema), defaultValues: EMPTY });

  // 每次打开都重置：取消过不把上一条的金额带进下一条（评审路二 #1）
  useEffect(() => {
    if (target) form.reset(EMPTY);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [target]);

  async function onSubmit(values: MarkPaidForm): Promise<void> {
    if (!target) return;
    const ok = await markPaid(target.id, {
      amount: values.amount.trim(),
      paid_at: values.paid_at ? new Date(values.paid_at).toISOString() : undefined,
    });
    if (ok) {
      toast.success('已记付款');
      setTarget(null);
      void reloadTasks();
    }
  }

  return (
    <Dialog open={!!target} onOpenChange={(open) => !open && setTarget(null)}>
      {target && (
        <DialogContent className='max-w-sm'>
          <DialogHeader>
            <DialogTitle>记已付款</DialogTitle>
            <DialogDescription>
              {target.payload?.posting_number ?? target.id}{' '}
              · 人工确认 1688 已付款后回填实付金额（后端会以 GetOrder 核对为辅）。
            </DialogDescription>
          </DialogHeader>
          <form className='space-y-4' onSubmit={form.handleSubmit(onSubmit)}>
            <div className='space-y-2'>
              <Label htmlFor='paid-amount'>实付金额</Label>
              <Input id='paid-amount' placeholder='如 128.50' autoFocus {...form.register('amount')} />
              {form.formState.errors.amount && (
                <p className='text-sm text-destructive'>{form.formState.errors.amount.message}</p>
              )}
            </div>
            <div className='space-y-2'>
              <Label htmlFor='paid-at'>付款时间（可空，默认现在）</Label>
              <Input id='paid-at' type='datetime-local' {...form.register('paid_at')} />
            </div>
            <DialogFooter>
              <Button type='button' variant='outline' onClick={() => setTarget(null)} disabled={submitting}>
                取消
              </Button>
              <Button type='submit' disabled={submitting || form.formState.isSubmitting}>
                {submitting ? '保存中…' : '保存'}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      )}
    </Dialog>
  );
}
