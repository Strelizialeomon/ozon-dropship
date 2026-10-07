import { useAtom, useAtomValue } from 'jotai';
import { useEffect, useState } from 'react';
import { toast } from 'sonner';
import { storeOptionsAtom } from '@/atoms/storeOptions';
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
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { useLinkActions } from '../actions';
import { linkFiltersAtom, linkModalAtom } from '../store';

interface LinkForm {
  store_id: string;
  ozon_offer_id: string;
  supplier_offer_id: string;
  priority: string;
  target_stock: string;
}

const EMPTY: LinkForm = { store_id: '', ozon_offer_id: '', supplier_offer_id: '', priority: '1', target_stock: '0' };

export function LinkFormDialog() {
  const [modal, setModal] = useAtom(linkModalAtom);
  const filters = useAtomValue(linkFiltersAtom);
  const storeOptions = useAtomValue(storeOptionsAtom);
  const { createLink, updateLink, loadLinks } = useLinkActions();
  const [form, setForm] = useState<LinkForm>(EMPTY);
  const [error, setError] = useState('');
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    if (!modal) return;
    setError('');
    if (modal.mode === 'edit') {
      const l = modal.link;
      setForm({
        store_id: l.store_id,
        ozon_offer_id: l.ozon_offer_id,
        supplier_offer_id: l.supplier_offer_id,
        priority: String(l.priority),
        target_stock: String(l.target_stock),
      });
    } else {
      setForm(EMPTY);
    }
  }, [modal]);

  function patch(p: Partial<LinkForm>): void {
    setForm((prev) => ({ ...prev, ...p }));
  }

  async function onSubmit(): Promise<void> {
    if (!form.store_id) return setError('请选择店铺');
    if (!form.ozon_offer_id.trim()) return setError('请填写 Ozon offer_id');
    if (!form.supplier_offer_id.trim()) return setError('请填写货源商品 ID');
    if (!Number.isInteger(Number(form.priority)) || Number(form.priority) < 1) {
      return setError('优先级要填正整数（1 = 主货源）');
    }

    setSubmitting(true);
    const body = {
      store_id: form.store_id,
      ozon_offer_id: form.ozon_offer_id.trim(),
      supplier_offer_id: form.supplier_offer_id.trim(),
      priority: Number(form.priority),
      target_stock: Number(form.target_stock) || 0,
    };
    const ok = modal?.mode === 'edit' ? await updateLink(modal.link.id, body) : await createLink(body);
    setSubmitting(false);
    if (ok) {
      toast.success(modal?.mode === 'edit' ? '映射已更新' : '映射已添加');
      setModal(null);
      void loadLinks(filters);
    }
  }

  return (
    <Dialog open={!!modal} onOpenChange={(open) => !open && setModal(null)}>
      {modal && (
        <DialogContent className='max-w-md'>
          <DialogHeader>
            <DialogTitle>{modal.mode === 'edit' ? '编辑映射' : '添加映射'}</DialogTitle>
            <DialogDescription>
              按店映射：同一 Ozon offer_id 允许多个货源，按优先级排主备（1 = 主）。
            </DialogDescription>
          </DialogHeader>
          <div className='space-y-4'>
            <div className='space-y-2'>
              <Label>店铺</Label>
              <Select value={form.store_id} onValueChange={(v) => patch({ store_id: v })}>
                <SelectTrigger className='w-full'>
                  <SelectValue placeholder='选择店铺' />
                </SelectTrigger>
                <SelectContent>
                  {storeOptions.map((s) => (
                    <SelectItem key={s.id} value={s.id}>
                      {s.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className='space-y-2'>
              <Label htmlFor='link-ozon'>Ozon offer_id</Label>
              <Input
                id='link-ozon'
                value={form.ozon_offer_id}
                onChange={(e) => patch({ ozon_offer_id: e.target.value })}
              />
            </div>
            <div className='space-y-2'>
              <Label htmlFor='link-supplier'>货源商品 ID（supplier_offer_id）</Label>
              <Input
                id='link-supplier'
                placeholder='从「货源商品」页复制 ID'
                value={form.supplier_offer_id}
                onChange={(e) => patch({ supplier_offer_id: e.target.value })}
              />
            </div>
            <div className='grid grid-cols-2 gap-4'>
              <div className='space-y-2'>
                <Label htmlFor='link-priority'>优先级</Label>
                <Input
                  id='link-priority'
                  type='number'
                  min={1}
                  value={form.priority}
                  onChange={(e) => patch({ priority: e.target.value })}
                />
              </div>
              <div className='space-y-2'>
                <Label htmlFor='link-target'>目标库存</Label>
                <Input
                  id='link-target'
                  type='number'
                  value={form.target_stock}
                  onChange={(e) => patch({ target_stock: e.target.value })}
                />
              </div>
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
