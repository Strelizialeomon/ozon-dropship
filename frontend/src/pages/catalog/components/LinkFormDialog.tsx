import { zodResolver } from '@hookform/resolvers/zod';
import { useAtom, useAtomValue } from 'jotai';
import { useEffect } from 'react';
import { useForm } from 'react-hook-form';
import { toast } from 'sonner';
import { z } from 'zod';
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
import { platformLabel } from '@/lib/labels';
import { useLinkActions } from '../actions';
import { catalogSubmittingAtom, linkModalAtom, offersAtom } from '../store';

const int = (msg: string, min: number) =>
  z.string().refine((v) => Number.isInteger(Number(v)) && Number(v) >= min, msg);

const schema = z.object({
  store_id: z.string().min(1, '请选择店铺'),
  ozon_offer_id: z.string().min(1, '请填 Ozon offer_id'),
  supplier_offer_id: z.string().min(1, '请选择货源商品'),
  priority: int('优先级要填 ≥ 1 的整数（1 = 主货源）', 1),
  target_stock: int('目标库存要填 ≥ 0 的整数', 0),
});

type LinkForm = z.infer<typeof schema>;

const EMPTY: LinkForm = { store_id: '', ozon_offer_id: '', supplier_offer_id: '', priority: '1', target_stock: '0' };

export function LinkFormDialog() {
  const [modal, setModal] = useAtom(linkModalAtom);
  const submitting = useAtomValue(catalogSubmittingAtom);
  const storeOptions = useAtomValue(storeOptionsAtom);
  const offers = useAtomValue(offersAtom);
  const { createLink, updateLink, reloadLinks } = useLinkActions();

  const form = useForm<LinkForm>({ resolver: zodResolver(schema), defaultValues: EMPTY });
  const isEdit = modal?.mode === 'edit';

  useEffect(() => {
    if (!modal) return;
    if (modal.mode === 'edit') {
      const l = modal.link;
      form.reset({
        store_id: l.store_id,
        ozon_offer_id: l.ozon_offer_id,
        supplier_offer_id: l.supplier_offer_id,
        priority: String(l.priority),
        target_stock: String(l.target_stock),
      });
    } else {
      form.reset(EMPTY);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [modal]);

  async function onSubmit(values: LinkForm): Promise<void> {
    if (isEdit && modal?.mode === 'edit') {
      // 后端 PUT 只收 priority / target_stock；改店或改商品要删了重建
      const ok = await updateLink(modal.link.id, {
        priority: Number(values.priority),
        target_stock: Number(values.target_stock),
      });
      if (ok) {
        toast.success('映射已更新');
        setModal(null);
        void reloadLinks();
      }
      return;
    }
    const ok = await createLink({
      store_id: values.store_id,
      ozon_offer_id: values.ozon_offer_id.trim(),
      supplier_offer_id: values.supplier_offer_id,
      priority: Number(values.priority),
      target_stock: Number(values.target_stock),
    });
    if (ok) {
      toast.success('映射已添加');
      setModal(null);
      void reloadLinks();
    }
  }

  return (
    <Dialog open={!!modal} onOpenChange={(open) => !open && setModal(null)}>
      {modal && (
        <DialogContent className='max-w-md'>
          <DialogHeader>
            <DialogTitle>{isEdit ? '编辑映射' : '添加映射'}</DialogTitle>
            <DialogDescription>
              {isEdit
                ? '店铺 / 商品 / 货源不可改（后端 PUT 只收优先级与目标库存）；要换请删除后重建。'
                : '按店映射：同一 Ozon offer_id 允许多个货源，按优先级排主备（1 = 主）。'}
            </DialogDescription>
          </DialogHeader>
          <form className='space-y-4' onSubmit={form.handleSubmit(onSubmit)}>
            {isEdit
              ? (
                <div className='space-y-1 rounded-md bg-muted p-3 text-sm'>
                  <div>
                    店铺：{storeOptions.find((s) => s.id === form.getValues('store_id'))?.name
                      ?? form.getValues('store_id')}
                  </div>
                  <div className='font-mono text-xs'>Ozon offer_id：{form.getValues('ozon_offer_id')}</div>
                  <div className='font-mono text-xs'>货源：{form.getValues('supplier_offer_id')}</div>
                </div>
              )
              : (
                <>
                  <div className='space-y-2'>
                    <Label>店铺</Label>
                    <Select
                      value={form.watch('store_id')}
                      onValueChange={(v) => form.setValue('store_id', v, { shouldValidate: true })}
                    >
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
                    {form.formState.errors.store_id && (
                      <p className='text-sm text-destructive'>{form.formState.errors.store_id.message}</p>
                    )}
                  </div>
                  <div className='space-y-2'>
                    <Label htmlFor='link-ozon'>Ozon offer_id</Label>
                    <Input id='link-ozon' {...form.register('ozon_offer_id')} />
                    {form.formState.errors.ozon_offer_id && (
                      <p className='text-sm text-destructive'>{form.formState.errors.ozon_offer_id.message}</p>
                    )}
                  </div>
                  <div className='space-y-2'>
                    <Label>货源商品</Label>
                    <Select
                      value={form.watch('supplier_offer_id')}
                      onValueChange={(v) => form.setValue('supplier_offer_id', v, { shouldValidate: true })}
                    >
                      <SelectTrigger className='w-full'>
                        <SelectValue placeholder='选一个货源商品' />
                      </SelectTrigger>
                      <SelectContent>
                        {offers.map((o) => (
                          <SelectItem key={o.id} value={o.id}>
                            {platformLabel(o.platform)} · {o.item_id}
                            {o.sku_id ? ` · ${o.sku_id}` : ''} · ¥{o.purchase_price}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                    {form.formState.errors.supplier_offer_id && (
                      <p className='text-sm text-destructive'>{form.formState.errors.supplier_offer_id.message}</p>
                    )}
                    {offers.length === 0 && (
                      <p className='text-xs text-muted-foreground'>还没有货源商品，先去「货源商品」页加一条。</p>
                    )}
                  </div>
                </>
              )}

            <div className='grid grid-cols-2 gap-4'>
              <div className='space-y-2'>
                <Label htmlFor='link-priority'>优先级</Label>
                <Input id='link-priority' type='number' min={1} {...form.register('priority')} />
                {form.formState.errors.priority && (
                  <p className='text-sm text-destructive'>{form.formState.errors.priority.message}</p>
                )}
              </div>
              <div className='space-y-2'>
                <Label htmlFor='link-target'>目标库存</Label>
                <Input id='link-target' type='number' min={0} {...form.register('target_stock')} />
                {form.formState.errors.target_stock && (
                  <p className='text-sm text-destructive'>{form.formState.errors.target_stock.message}</p>
                )}
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
