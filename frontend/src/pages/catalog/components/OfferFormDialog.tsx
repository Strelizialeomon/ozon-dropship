import { useAtom, useAtomValue } from 'jotai';
import { useEffect, useState } from 'react';
import { toast } from 'sonner';
import type { OrderChannel, Platform } from '@/api/catalog';
import { Button } from '@/components/ui/button';
import { Checkbox } from '@/components/ui/checkbox';
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { useOfferActions } from '../actions';
import { offerFiltersAtom, offerModalAtom } from '../store';

interface OfferForm {
  platform: Platform;
  item_id: string;
  sku_id: string;
  url: string;
  purchase_price: string;
  domestic_freight: string;
  stock: string;
  price_alert_threshold: string;
  order_channel: OrderChannel;
  followed: boolean;
}

const EMPTY: OfferForm = {
  platform: '1688',
  item_id: '',
  sku_id: '',
  url: '',
  purchase_price: '',
  domestic_freight: '0',
  stock: '0',
  price_alert_threshold: '',
  order_channel: 'self_use',
  followed: false,
};

const CHANNEL_LABELS: Record<OrderChannel, string> = {
  self_use: '1688 自用版（默认）',
  cross_border: '1688 跨境版',
  manual: '人工渠道（拼多多 / 淘宝）',
};

export function OfferFormDialog() {
  const [modal, setModal] = useAtom(offerModalAtom);
  const filters = useAtomValue(offerFiltersAtom);
  const { createOffer, updateOffer, loadOffers } = useOfferActions();
  const [form, setForm] = useState<OfferForm>(EMPTY);
  const [error, setError] = useState('');
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    if (!modal) return;
    setError('');
    if (modal.mode === 'edit') {
      const o = modal.offer;
      setForm({
        platform: o.platform,
        item_id: o.item_id,
        sku_id: o.sku_id,
        url: o.url,
        purchase_price: o.purchase_price,
        domestic_freight: o.domestic_freight,
        stock: String(o.stock),
        price_alert_threshold: o.price_alert_threshold ?? '',
        order_channel: o.order_channel,
        followed: o.followed,
      });
    } else {
      setForm(EMPTY);
    }
  }, [modal]);

  function patch(p: Partial<OfferForm>): void {
    setForm((prev) => ({ ...prev, ...p }));
  }

  async function onSubmit(): Promise<void> {
    if (!form.item_id.trim()) return setError('请填写货源商品 ID');
    if (!form.purchase_price.trim() || Number.isNaN(Number(form.purchase_price))) return setError('采购价请填数字');
    if (form.platform === '1688' && !form.sku_id.trim()) return setError('1688 货源要填规格 ID（sku_id）');

    setSubmitting(true);
    const body = {
      platform: form.platform,
      item_id: form.item_id.trim(),
      sku_id: form.sku_id.trim(),
      url: form.url.trim(),
      purchase_price: form.purchase_price.trim(),
      domestic_freight: form.domestic_freight.trim() || '0',
      stock: Number(form.stock) || 0,
      price_alert_threshold: form.price_alert_threshold.trim() || null,
      order_channel: form.order_channel,
      followed: form.followed,
    };
    const ok = modal?.mode === 'edit' ? await updateOffer(modal.offer.id, body) : await createOffer(body);
    setSubmitting(false);
    if (ok) {
      toast.success(modal?.mode === 'edit' ? '货源已更新' : '货源已添加');
      setModal(null);
      void loadOffers(filters);
    }
  }

  return (
    <Dialog open={!!modal} onOpenChange={(open) => !open && setModal(null)}>
      {modal && (
        <DialogContent className='max-w-lg'>
          <DialogHeader>
            <DialogTitle>{modal.mode === 'edit' ? '编辑货源商品' : '添加货源商品'}</DialogTitle>
          </DialogHeader>
          <div className='grid grid-cols-2 gap-4'>
            <div className='space-y-2'>
              <Label>平台</Label>
              <Select value={form.platform} onValueChange={(v) => patch({ platform: v as Platform })}>
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
              <Select value={form.order_channel} onValueChange={(v) => patch({ order_channel: v as OrderChannel })}>
                <SelectTrigger className='w-full'>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {(Object.keys(CHANNEL_LABELS) as OrderChannel[]).map((c) => (
                    <SelectItem key={c} value={c}>
                      {CHANNEL_LABELS[c]}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className='space-y-2'>
              <Label htmlFor='offer-item'>商品 ID</Label>
              <Input id='offer-item' value={form.item_id} onChange={(e) => patch({ item_id: e.target.value })} />
            </div>
            <div className='space-y-2'>
              <Label htmlFor='offer-sku'>规格 ID（sku_id）</Label>
              <Input id='offer-sku' value={form.sku_id} onChange={(e) => patch({ sku_id: e.target.value })} />
            </div>
            <div className='col-span-2 space-y-2'>
              <Label htmlFor='offer-url'>商品链接</Label>
              <Input id='offer-url' value={form.url} onChange={(e) => patch({ url: e.target.value })} />
            </div>
            <div className='space-y-2'>
              <Label htmlFor='offer-price'>采购价</Label>
              <Input
                id='offer-price'
                value={form.purchase_price}
                onChange={(e) => patch({ purchase_price: e.target.value })}
              />
            </div>
            <div className='space-y-2'>
              <Label htmlFor='offer-freight'>境内运费</Label>
              <Input
                id='offer-freight'
                value={form.domestic_freight}
                onChange={(e) => patch({ domestic_freight: e.target.value })}
              />
            </div>
            <div className='space-y-2'>
              <Label htmlFor='offer-stock'>库存</Label>
              <Input
                id='offer-stock'
                type='number'
                value={form.stock}
                onChange={(e) => patch({ stock: e.target.value })}
              />
            </div>
            <div className='space-y-2'>
              <Label htmlFor='offer-threshold'>比价告警阈值（可空 = 默认 10%）</Label>
              <Input
                id='offer-threshold'
                value={form.price_alert_threshold}
                onChange={(e) => patch({ price_alert_threshold: e.target.value })}
              />
            </div>
            <label className='col-span-2 flex items-center gap-2 text-sm'>
              <Checkbox checked={form.followed} onCheckedChange={(v) => patch({ followed: v === true })} />
              已在 1688 关注该商品（库存 / 失效推送的前提）
            </label>
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
