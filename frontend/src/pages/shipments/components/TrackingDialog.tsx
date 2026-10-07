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
import { tplIntegration } from '@/lib/labels';
import { useShipmentActions } from '../actions';
import { shipmentFiltersAtom, shipmentTrackingTargetAtom } from '../store';

/** 传单号：仅 tpl_integration_type ∈ {3pl_tracking, non_integrated} 的 posting 需要（总纲 §7.4）。 */
export function TrackingDialog() {
  const [target, setTarget] = useAtom(shipmentTrackingTargetAtom);
  const filters = useAtomValue(shipmentFiltersAtom);
  const { setTracking, loadShipments } = useShipmentActions();
  const [trackingNo, setTrackingNo] = useState('');
  const [error, setError] = useState('');

  async function onSubmit(): Promise<void> {
    if (!target) return;
    if (trackingNo.trim().length < 6) {
      setError('请填写承运商单号');
      return;
    }
    const ok = await setTracking(target.order_id, trackingNo.trim());
    if (ok) {
      toast.success('已回传单号');
      setTarget(null);
      setTrackingNo('');
      setError('');
      void loadShipments(filters);
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
          <div className='space-y-2'>
            <Label htmlFor='tracking-no'>承运商单号</Label>
            <Input id='tracking-no' value={trackingNo} onChange={(e) => setTrackingNo(e.target.value)} />
            {error && <p className='text-sm text-destructive'>{error}</p>}
          </div>
          <DialogFooter>
            <Button variant='outline' onClick={() => setTarget(null)}>
              取消
            </Button>
            <Button onClick={() => void onSubmit()}>回传</Button>
          </DialogFooter>
        </DialogContent>
      )}
    </Dialog>
  );
}
