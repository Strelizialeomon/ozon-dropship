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
import { tplIntegration } from '@/lib/labels';
import { useShipmentActions } from '../actions';
import { shipmentSubmittingAtom, shipmentTrackingTargetAtom } from '../store';

const schema = z.object({
  tracking_no: z.string().min(6, '请填写承运商单号（至少 6 位）'),
  carrier: z.string(),
});

type TrackingForm = z.infer<typeof schema>;

const EMPTY: TrackingForm = { tracking_no: '', carrier: '' };

/** 传单号：只对 tracking_action = set 的行显示（判定权在后端 order.TrackingActionFor）。 */
export function TrackingDialog() {
  const [target, setTarget] = useAtom(shipmentTrackingTargetAtom);
  const submitting = useAtomValue(shipmentSubmittingAtom);
  const { setTracking, reloadShipments } = useShipmentActions();

  const form = useForm<TrackingForm>({ resolver: zodResolver(schema), defaultValues: EMPTY });

  // 每次打开都重置（评审路二 #1：取消不把上一条的单号带进来）
  useEffect(() => {
    if (target) form.reset(EMPTY);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [target]);

  async function onSubmit(values: TrackingForm): Promise<void> {
    if (!target) return;
    const ok = await setTracking(target.order_id, values.tracking_no.trim());
    if (ok) {
      toast.success('已回传单号');
      setTarget(null);
      void reloadShipments();
    }
  }

  return (
    <Dialog open={!!target} onOpenChange={(open) => !open && setTarget(null)}>
      {target && (
        <DialogContent className='max-w-sm'>
          <DialogHeader>
            <DialogTitle>传承运商单号</DialogTitle>
            <DialogDescription>
              {target.posting_number}{' '}
              · 物流方式「{tplIntegration(target.tpl_integration_type)}」由卖家登记，需要回传单号。
            </DialogDescription>
          </DialogHeader>
          <form className='space-y-4' onSubmit={form.handleSubmit(onSubmit)}>
            <div className='space-y-2'>
              <Label htmlFor='tracking-no'>承运商单号</Label>
              <Input id='tracking-no' autoFocus {...form.register('tracking_no')} />
              {form.formState.errors.tracking_no && (
                <p className='text-sm text-destructive'>{form.formState.errors.tracking_no.message}</p>
              )}
            </div>
            <div className='space-y-2'>
              <Label htmlFor='tracking-carrier'>承运商（可空）</Label>
              <Input id='tracking-carrier' {...form.register('carrier')} />
            </div>
            <DialogFooter>
              <Button type='button' variant='outline' onClick={() => setTarget(null)} disabled={submitting}>
                取消
              </Button>
              <Button type='submit' disabled={submitting}>
                {submitting ? '回传中…' : '回传'}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      )}
    </Dialog>
  );
}
