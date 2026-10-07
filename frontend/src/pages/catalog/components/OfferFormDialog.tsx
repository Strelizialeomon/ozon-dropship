import { zodResolver } from '@hookform/resolvers/zod';
import { useAtom, useAtomValue } from 'jotai';
import { useEffect } from 'react';
import { useForm } from 'react-hook-form';
import { toast } from 'sonner';
import { z } from 'zod';
import type { OfferStatus, OrderChannel, Platform } from '@/api/catalog';
import { Button } from '@/components/ui/button';
import { Checkbox } from '@/components/ui/checkbox';
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
import { useOfferActions } from '../actions';
import { catalogSubmittingAtom, offerModalAtom } from '../store';

const num = (msg: string, min: number, max?: number) =>
  z.string().refine((v) => {
    if (v === '') return false;
    const n = Number(v);
    if (Number.isNaN(n) || n < min) return false;
    return max === undefined || n <= max;
  }, msg);

const schema = z.object({
  platform: z.enum(['1688', 'pdd', 'taobao']),
  item_id: z.string().min(1, '请填商品 ID'),
  sku_id: z.string(),
  url: z.string(),
  purchase_price: num('采购价要填 ≥ 0 的数字', 0),
  domestic_freight: num('境内运费要填 ≥ 0 的数字', 0),
  currency: z.string().length(3, '币种是 3 位码（如 CNY）'),
  stock: z.string().refine((v) => Number.isInteger(Number(v)) && Number(v) >= 0, '库存要填 ≥ 0 的整数'),
  price_alert_threshold: z.string().refine(
    (v) => v === '' || (Number(v) >= 0 && Number(v) <= 1),
    '阈值填 0–1 的小数（如 0.1）',
  ),
  order_channel: z.enum(['self_use', 'cross_border', 'manual']),
  followed: z.boolean(),
  status: z.enum(['active', 'out_of_stock', 'invalid']),
});

type OfferForm = z.infer<typeof schema>;

const EMPTY: OfferForm = {
  platform: '1688',
  item_id: '',
  sku_id: '',
  url: '',
  purchase_price: '',
  domestic_freight: '0',
  currency: 'CNY',
  stock: '0',
  price_alert_threshold: '',
  order_channel: 'self_use',
  followed: false,
  status: 'active',
};

export function OfferFormDialog() {
  const [modal, setModal] = useAtom(offerModalAtom);
  const submitting = useAtomValue(catalogSubmittingAtom);
  const { createOffer, updateOffer, reloadOffers } = useOfferActions();

  const form = useForm<OfferForm>({ resolver: zodResolver(schema), defaultValues: EMPTY });
  const platform = form.watch('platform');
  const channel = form.watch('order_channel');
  const followed = form.watch('followed');
  const status = form.watch('status');

  useEffect(() => {
    if (!modal) return;
    if (modal.mode === 'edit') {
      const o = modal.offer;
      form.reset({
        platform: o.platform,
        item_id: o.item_id,
        sku_id: o.sku_id,
        url: o.url,
        purchase_price: o.purchase_price,
        domestic_freight: o.domestic_freight,
        currency: o.currency || 'CNY',
        stock: String(o.stock),
        price_alert_threshold: o.price_alert_threshold ?? '',
        order_channel: o.order_channel,
        followed: o.followed,
        status: o.status,
      });
    } else {
      form.reset(EMPTY);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [modal]);

  // 非 1688 货源没有下单 API，只能是人工通道（后端同款校验）
  useEffect(() => {
    if (platform !== '1688' && channel !== 'manual') {
      form.setValue('order_channel', 'manual');
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [platform]);

  async function onSubmit(values: OfferForm): Promise<void> {
    const body = {
      platform: values.platform,
      item_id: values.item_id.trim(),
      sku_id: values.sku_id.trim(),
      url: values.url.trim(),
      purchase_price: values.purchase_price.trim(),
      domestic_freight: values.domestic_freight.trim() || '0',
      currency: values.currency.trim().toUpperCase(),
      stock: Number(values.stock),
      price_alert_threshold: values.price_alert_threshold.trim() || null,
      order_channel: values.order_channel as OrderChannel,
      followed: values.followed,
      status: values.status as OfferStatus,
    };
    const ok = modal?.mode === 'edit' ? await updateOffer(modal.offer.id, body) : await createOffer(body);
    if (ok) {
      toast.success(modal?.mode === 'edit' ? '货源已更新' : '货源已添加');
      setModal(null);
      void reloadOffers();
    }
  }

  return (
    <Dialog open={!!modal} onOpenChange={(open) => !open && setModal(null)}>
      {modal && (
        <DialogContent className='max-w-lg'>
          <DialogHeader>
            <DialogTitle>{modal.mode === 'edit' ? '编辑货源商品' : '添加货源商品'}</DialogTitle>
            <DialogDescription>价格是下单时快照的来源；比价阈值按比率填（0.1 = 涨 10% 告警）。</DialogDescription>
          </DialogHeader>
          <form className='space-y-4' onSubmit={form.handleSubmit(onSubmit)}>
            <div className='grid grid-cols-2 gap-4'>
              <div className='space-y-2'>
                <Label>平台</Label>
                <Select value={platform} onValueChange={(v) => form.setValue('platform', v as Platform)}>
                  <SelectTrigger className='w-full'>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value='1688'>1688</SelectItem>
                    <SelectItem value='pdd'>拼多多</SelectItem>
                    <SelectItem value='taobao'>淘宝</SelectItem>
                  </SelectContent>
                </Select>
              </div>
              <div className='space-y-2'>
                <Label>下单通道</Label>
                <Select
                  value={channel}
                  onValueChange={(v) => form.setValue('order_channel', v as OfferForm['order_channel'])}
                  disabled={platform !== '1688'}
                >
                  <SelectTrigger className='w-full'>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value='self_use'>1688 自用版（默认）</SelectItem>
                    <SelectItem value='cross_border' disabled>
                      1688 跨境版（S2 起接入）
                    </SelectItem>
                    <SelectItem value='manual'>人工渠道</SelectItem>
                  </SelectContent>
                </Select>
              </div>
              <div className='space-y-2'>
                <Label htmlFor='offer-item'>商品 ID</Label>
                <Input id='offer-item' {...form.register('item_id')} />
                {form.formState.errors.item_id && (
                  <p className='text-sm text-destructive'>{form.formState.errors.item_id.message}</p>
                )}
              </div>
              <div className='space-y-2'>
                <Label htmlFor='offer-sku'>规格 ID（sku_id，可空）</Label>
                <Input id='offer-sku' {...form.register('sku_id')} />
              </div>
              <div className='col-span-2 space-y-2'>
                <Label htmlFor='offer-url'>商品链接</Label>
                <Input id='offer-url' {...form.register('url')} />
              </div>
              <div className='space-y-2'>
                <Label htmlFor='offer-price'>采购价</Label>
                <Input id='offer-price' {...form.register('purchase_price')} />
                {form.formState.errors.purchase_price && (
                  <p className='text-sm text-destructive'>{form.formState.errors.purchase_price.message}</p>
                )}
              </div>
              <div className='space-y-2'>
                <Label htmlFor='offer-freight'>境内运费</Label>
                <Input id='offer-freight' {...form.register('domestic_freight')} />
                {form.formState.errors.domestic_freight && (
                  <p className='text-sm text-destructive'>{form.formState.errors.domestic_freight.message}</p>
                )}
              </div>
              <div className='space-y-2'>
                <Label htmlFor='offer-currency'>币种</Label>
                <Input id='offer-currency' placeholder='CNY' {...form.register('currency')} />
                {form.formState.errors.currency && (
                  <p className='text-sm text-destructive'>{form.formState.errors.currency.message}</p>
                )}
              </div>
              <div className='space-y-2'>
                <Label htmlFor='offer-stock'>库存</Label>
                <Input id='offer-stock' type='number' min={0} {...form.register('stock')} />
                {form.formState.errors.stock && (
                  <p className='text-sm text-destructive'>{form.formState.errors.stock.message}</p>
                )}
              </div>
              <div className='space-y-2'>
                <Label htmlFor='offer-threshold'>比价告警阈值（0–1，可空 = 默认 0.1）</Label>
                <Input id='offer-threshold' {...form.register('price_alert_threshold')} />
                {form.formState.errors.price_alert_threshold && (
                  <p className='text-sm text-destructive'>{form.formState.errors.price_alert_threshold.message}</p>
                )}
              </div>
              <div className='space-y-2'>
                <Label>状态</Label>
                <Select value={status} onValueChange={(v) => form.setValue('status', v as OfferForm['status'])}>
                  <SelectTrigger className='w-full'>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value='active'>在售</SelectItem>
                    <SelectItem value='out_of_stock'>断货</SelectItem>
                    <SelectItem value='invalid'>失效</SelectItem>
                  </SelectContent>
                </Select>
              </div>
            </div>
            <label className='flex items-center gap-2 text-sm'>
              <Checkbox checked={followed} onCheckedChange={(v) => form.setValue('followed', v === true)} />
              已在 1688 关注该商品（库存 / 失效推送的前提）
            </label>
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
