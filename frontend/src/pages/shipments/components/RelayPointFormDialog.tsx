import { useAtom } from 'jotai';
import { useEffect, useState } from 'react';
import { toast } from 'sonner';
import type { RelayKind } from '@/api/shipments';
import { Button } from '@/components/ui/button';
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Textarea } from '@/components/ui/textarea';
import { useRelayPointActions } from '../actions';
import { relayModalAtom } from '../store';

interface RelayForm {
  name: string;
  kind: RelayKind;
  address: string;
  contact: string;
  status: string;
}

const EMPTY: RelayForm = { name: '', kind: 'forwarder', address: '', contact: '', status: 'active' };

export function RelayPointFormDialog() {
  const [modal, setModal] = useAtom(relayModalAtom);
  const { createRelayPoint, updateRelayPoint } = useRelayPointActions();
  const [form, setForm] = useState<RelayForm>(EMPTY);
  const [error, setError] = useState('');
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    if (!modal) return;
    setError('');
    if (modal.mode === 'edit') {
      const p = modal.point;
      setForm({ name: p.name, kind: p.kind, address: p.address, contact: p.contact, status: p.status });
    } else {
      setForm(EMPTY);
    }
  }, [modal]);

  function patch(p: Partial<RelayForm>): void {
    setForm((prev) => ({ ...prev, ...p }));
  }

  async function onSubmit(): Promise<void> {
    if (!form.name.trim()) return setError('请填写名称');
    if (!form.address.trim()) return setError('请填写地址（采购单收货地址用它）');
    setSubmitting(true);
    const ok = modal?.mode === 'edit'
      ? await updateRelayPoint(modal.point.id, form)
      : await createRelayPoint(form);
    setSubmitting(false);
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
          <div className='space-y-4'>
            <div className='grid grid-cols-2 gap-4'>
              <div className='space-y-2'>
                <Label htmlFor='relay-name'>名称</Label>
                <Input id='relay-name' value={form.name} onChange={(e) => patch({ name: e.target.value })} />
              </div>
              <div className='space-y-2'>
                <Label>类型</Label>
                <Select value={form.kind} onValueChange={(v) => patch({ kind: v as RelayKind })}>
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
                value={form.address}
                onChange={(e) => patch({ address: e.target.value })}
                placeholder='采购下单时直接复制这里，作为收货地址'
              />
            </div>
            <div className='space-y-2'>
              <Label htmlFor='relay-contact'>联系人（可空）</Label>
              <Input id='relay-contact' value={form.contact} onChange={(e) => patch({ contact: e.target.value })} />
            </div>
          </div>
          {error && <p className='text-sm text-destructive'>{error}</p>}
          <DialogFooter>
            <Button variant='outline' onClick={() => setModal(null)} disabled={submitting}>
              取消
            </Button>
            <Button onClick={() => void onSubmit()} disabled={submitting}>
              {submitting ? '保存中…' : '保存'}
            </Button>
          </DialogFooter>
        </DialogContent>
      )}
    </Dialog>
  );
}
