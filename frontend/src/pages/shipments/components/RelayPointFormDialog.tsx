import { zodResolver } from '@hookform/resolvers/zod';
import { useAtom, useAtomValue } from 'jotai';
import { useEffect } from 'react';
import { useForm } from 'react-hook-form';
import { toast } from 'sonner';
import { z } from 'zod';
import type { RelayKind, RelayStatus } from '@/api/shipments';
import { Button } from '@/components/ui/button';
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Textarea } from '@/components/ui/textarea';
import { useRelayPointActions } from '../actions';
import { relayModalAtom, shipmentSubmittingAtom } from '../store';

const schema = z.object({
  name: z.string().min(1, '请填名称'),
  kind: z.enum(['forwarder', 'own_warehouse']),
  address: z.string().min(1, '请填地址（采购单收货地址用它）'),
  contact: z.string(),
  status: z.enum(['active', 'disabled']),
});

type RelayForm = z.infer<typeof schema>;

const EMPTY: RelayForm = { name: '', kind: 'forwarder', address: '', contact: '', status: 'active' };

export function RelayPointFormDialog() {
  const [modal, setModal] = useAtom(relayModalAtom);
  const submitting = useAtomValue(shipmentSubmittingAtom);
  const { createRelayPoint, updateRelayPoint } = useRelayPointActions();

  const form = useForm<RelayForm>({ resolver: zodResolver(schema), defaultValues: EMPTY });
  const kind = form.watch('kind');
  const status = form.watch('status');

  useEffect(() => {
    if (!modal) return;
    if (modal.mode === 'edit') {
      const p = modal.point;
      form.reset({ name: p.name, kind: p.kind, address: p.address, contact: p.contact, status: p.status });
    } else {
      form.reset(EMPTY);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [modal]);

  async function onSubmit(values: RelayForm): Promise<void> {
    const ok = modal?.mode === 'edit'
      ? await updateRelayPoint(modal.point.id, values)
      : await createRelayPoint(values);
    if (ok) {
      toast.success('已保存');
      setModal(null);
    }
  }

  return (
    <Dialog open={!!modal} onOpenChange={(open) => !open && setModal(null)}>
      {modal && (
        <DialogContent className='max-w-md'>
          <DialogHeader>
            <DialogTitle>{modal.mode === 'edit' ? '编辑中转点' : '新建中转点'}</DialogTitle>
          </DialogHeader>
          <form className='space-y-4' onSubmit={form.handleSubmit(onSubmit)}>
            <div className='grid grid-cols-2 gap-4'>
              <div className='space-y-2'>
                <Label htmlFor='relay-name'>名称</Label>
                <Input id='relay-name' {...form.register('name')} />
                {form.formState.errors.name && (
                  <p className='text-sm text-destructive'>{form.formState.errors.name.message}</p>
                )}
              </div>
              <div className='space-y-2'>
                <Label>类型</Label>
                <Select value={kind} onValueChange={(v) => form.setValue('kind', v as RelayKind)}>
                  <SelectTrigger className='w-full'>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value='forwarder'>货代 / 物流商代打包仓</SelectItem>
                    <SelectItem value='own_warehouse'>自有仓</SelectItem>
                  </SelectContent>
                </Select>
              </div>
            </div>
            <div className='space-y-2'>
              <Label htmlFor='relay-address'>收货地址（收货人 + 电话 + 地址）</Label>
              <Textarea
                id='relay-address'
                rows={3}
                placeholder='采购下单时直接复制这里，作为收货地址'
                {...form.register('address')}
              />
              {form.formState.errors.address && (
                <p className='text-sm text-destructive'>{form.formState.errors.address.message}</p>
              )}
            </div>
            <div className='grid grid-cols-2 gap-4'>
              <div className='space-y-2'>
                <Label htmlFor='relay-contact'>联系人（可空）</Label>
                <Input id='relay-contact' {...form.register('contact')} />
              </div>
              <div className='space-y-2'>
                <Label>状态</Label>
                <Select value={status} onValueChange={(v) => form.setValue('status', v as RelayStatus)}>
                  <SelectTrigger className='w-full'>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value='active'>启用</SelectItem>
                    <SelectItem value='disabled'>停用</SelectItem>
                  </SelectContent>
                </Select>
              </div>
            </div>
            <DialogFooter>
              <Button type='button' variant='outline' onClick={() => setModal(null)} disabled={submitting}>
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
