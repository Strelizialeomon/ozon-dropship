import { zodResolver } from '@hookform/resolvers/zod';
import { useAtom } from 'jotai';
import { useEffect, useState } from 'react';
import { useForm } from 'react-hook-form';
import { toast } from 'sonner';
import { z } from 'zod';
import { Button } from '@/components/ui/button';
import { Checkbox } from '@/components/ui/checkbox';
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { listRelayPoints, type RelayPointDTO } from '@/api/shipments';
import { storeModalAtom } from '../store';
import { useStoreActions } from '../actions';

const schema = z.object({
  name: z.string().min(1, '请填写店铺名'),
  mode: z.enum(['rfbs', 'fbp', 'local']),
  client_id: z.string(),
  currency: z.string().min(1, '请填写币种'),
  default_relay_point_id: z.string(),
  push_enabled: z.boolean(),
  ship_early: z.boolean(),
  status: z.enum(['active', 'paused']),
});

type StoreForm = z.infer<typeof schema>;

const MODE_OPTIONS = [
  { value: 'rfbs', label: 'rFBS（跨境仓发）' },
  { value: 'fbp', label: 'FBP（平台仓，S2 接入）' },
  { value: 'local', label: '俄本土店' },
];

export function StoreFormDialog() {
  const [modal, setModal] = useAtom(storeModalAtom);
  const { createStore, updateStore } = useStoreActions();
  const [submitting, setLocalSubmitting] = useState(false);
  // 中转点下拉的候选（S1-D 已合并，/api/relay-points 可用）：本弹窗自己的选项数据，就近拉一次
  const [relayOptions, setRelayOptions] = useState<RelayPointDTO[]>([]);

  const form = useForm<StoreForm>({
    resolver: zodResolver(schema),
    defaultValues: emptyValues(),
  });

  useEffect(() => {
    if (!modal) return;
    void listRelayPoints()
      .then(setRelayOptions)
      .catch(() => {
        /* 拉不到就只显示「不指定」，拦截器已提示 */
      });
  }, [modal]);

  // eslint-disable-next-line react-hooks/exhaustive-deps
  useEffect(() => {
    if (!modal) return;
    if (modal.mode === 'edit') {
      const s = modal.store;
      form.reset({
        name: s.name,
        mode: s.mode,
        client_id: s.client_id,
        currency: s.currency || 'CNY',
        default_relay_point_id: s.default_relay_point_id ?? '',
        push_enabled: s.push_enabled,
        ship_early: s.ship_early,
        status: s.status,
      });
    } else {
      form.reset(emptyValues());
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [modal]);

  const pushEnabled = form.watch('push_enabled');
  const shipEarly = form.watch('ship_early');
  const mode = form.watch('mode');
  const status = form.watch('status');

  async function onSubmit(values: StoreForm): Promise<void> {
    setLocalSubmitting(true);
    const body = {
      name: values.name,
      mode: values.mode,
      client_id: values.client_id,
      currency: values.currency,
      default_relay_point_id: values.default_relay_point_id || null,
      push_enabled: values.push_enabled,
      ship_early: values.ship_early,
      status: values.status,
    };
    const ok = modal?.mode === 'edit' ? await updateStore(modal.store.id, body) : await createStore(body);
    setLocalSubmitting(false);
    if (ok) {
      toast.success(modal?.mode === 'edit' ? '店铺已更新' : '店铺已创建');
      setModal(null);
    }
  }

  return (
    <Dialog open={!!modal} onOpenChange={(open) => !open && setModal(null)}>
      {modal && (
        <DialogContent className='max-w-lg'>
          <DialogHeader>
            <DialogTitle>{modal.mode === 'edit' ? '编辑店铺' : '新建店铺'}</DialogTitle>
          </DialogHeader>
          <form className='space-y-4' onSubmit={form.handleSubmit(onSubmit)}>
            <div className='grid grid-cols-2 gap-4'>
              <div className='space-y-2'>
                <Label htmlFor='store-name'>店铺名</Label>
                <Input id='store-name' {...form.register('name')} />
                {form.formState.errors.name && (
                  <p className='text-sm text-destructive'>{form.formState.errors.name.message}</p>
                )}
              </div>
              <div className='space-y-2'>
                <Label>发货模式</Label>
                <Select value={mode} onValueChange={(v) => form.setValue('mode', v as StoreForm['mode'])}>
                  <SelectTrigger className='w-full'>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {MODE_OPTIONS.map((o) => (
                      <SelectItem key={o.value} value={o.value}>
                        {o.label}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              <div className='space-y-2'>
                <Label htmlFor='store-client-id'>Ozon Client-Id</Label>
                <Input id='store-client-id' {...form.register('client_id')} />
              </div>
              <div className='space-y-2'>
                <Label htmlFor='store-currency'>结算币种</Label>
                <Input id='store-currency' placeholder='CNY' {...form.register('currency')} />
              </div>
              <div className='space-y-2'>
                <Label>默认中转点</Label>
                <Select
                  value={form.watch('default_relay_point_id') || '_none'}
                  onValueChange={(v) => form.setValue('default_relay_point_id', v === '_none' ? '' : v)}
                >
                  <SelectTrigger className='w-full'>
                    <SelectValue placeholder='不指定' />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value='_none'>不指定</SelectItem>
                    {relayOptions.map((r) => (
                      <SelectItem key={r.id} value={r.id}>
                        {r.name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              <div className='space-y-2'>
                <Label>状态</Label>
                <Select value={status} onValueChange={(v) => form.setValue('status', v as StoreForm['status'])}>
                  <SelectTrigger className='w-full'>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value='active'>启用</SelectItem>
                    <SelectItem value='paused'>暂停</SelectItem>
                  </SelectContent>
                </Select>
              </div>
            </div>
            <div className='flex items-center gap-6'>
              <label className='flex items-center gap-2 text-sm'>
                <Checkbox
                  checked={pushEnabled}
                  onCheckedChange={(v) => form.setValue('push_enabled', v === true)}
                />
                已开推送（低频对账）
              </label>
              <label className='flex items-center gap-2 text-sm'>
                <Checkbox checked={shipEarly} onCheckedChange={(v) => form.setValue('ship_early', v === true)} />
                货到中转点前提前备货
              </label>
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

function emptyValues(): StoreForm {
  return {
    name: '',
    mode: 'rfbs',
    client_id: '',
    currency: 'CNY',
    default_relay_point_id: '',
    push_enabled: false,
    ship_early: false,
    status: 'active',
  };
}
