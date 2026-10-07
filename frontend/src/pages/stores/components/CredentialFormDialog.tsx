import { useAtom, useAtomValue } from 'jotai';
import { useEffect, useState } from 'react';
import { useForm } from 'react-hook-form';
import { toast } from 'sonner';
import type { CredentialKind } from '@/api/credentials';
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
import { useCredentialActions } from '../actions';
import { credentialModalAtom, storesAtom } from '../store';

// 每种凭据需要哪些载荷字段（照 backend/internal/store/credential.go 的 kindRequiredFields）。
const KIND_FIELDS: Record<CredentialKind, { key: string; label: string; type?: string }[]> = {
  ozon_api_key: [{ key: 'api_key', label: 'Ozon Api-Key', type: 'password' }],
  alibaba_app: [
    { key: 'app_key', label: 'App Key' },
    { key: 'app_secret', label: 'App Secret', type: 'password' },
  ],
  alibaba_token: [
    { key: 'access_token', label: 'Access Token', type: 'password' },
    { key: 'refresh_token', label: 'Refresh Token（可选）', type: 'password' },
  ],
};

const KIND_OPTIONS: { value: CredentialKind; label: string }[] = [
  { value: 'ozon_api_key', label: 'Ozon Api-Key（按店）' },
  { value: 'alibaba_app', label: '1688 应用密钥（企业级）' },
  { value: 'alibaba_token', label: '1688 买家 token（企业级）' },
];

interface CredentialForm {
  store_id: string;
  kind: CredentialKind;
  expires_at: string; // datetime-local；空 = 不设置/保留原值
  api_key: string;
  app_key: string;
  app_secret: string;
  access_token: string;
  refresh_token: string;
}

const EMPTY: CredentialForm = {
  store_id: '',
  kind: 'ozon_api_key',
  expires_at: '',
  api_key: '',
  app_key: '',
  app_secret: '',
  access_token: '',
  refresh_token: '',
};

export function CredentialFormDialog() {
  const [modal, setModal] = useAtom(credentialModalAtom);
  const stores = useAtomValue(storesAtom);
  const { saveCredential } = useCredentialActions();
  const [submitting, setSubmitting] = useState(false);
  const [formError, setFormError] = useState('');

  const form = useForm<CredentialForm>({ defaultValues: EMPTY });
  const kind = form.watch('kind');
  const storeId = form.watch('store_id');

  const rotateTarget = modal?.mode === 'rotate' ? modal.credential : null;

  useEffect(() => {
    if (!modal) return;
    setFormError('');
    if (modal.mode === 'create') {
      form.reset({ ...EMPTY, store_id: modal.storeId ?? '' });
    } else {
      form.reset({ ...EMPTY, store_id: modal.credential.store_id, kind: modal.credential.kind });
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [modal]);

  async function onSubmit(values: CredentialForm): Promise<void> {
    setFormError('');
    // 按 kind 组装载荷 + 必填校验
    const fields = KIND_FIELDS[values.kind];
    const payload: Record<string, string> = {};
    for (const f of fields) {
      const v = (values[f.key as keyof CredentialForm] as string)?.trim() ?? '';
      const required = f.key !== 'refresh_token';
      if (required && !v) {
        setFormError(`请填写 ${f.label}`);
        return;
      }
      if (v) payload[f.key] = v;
    }
    setSubmitting(true);
    const ok = await saveCredential({
      store_id: values.store_id,
      kind: values.kind,
      payload,
      // 空 = 不传（后端语义：保留原到期时间，轮换时不会误抹）
      expires_at: values.expires_at ? new Date(values.expires_at).toISOString() : null,
    });
    setSubmitting(false);
    if (ok) {
      toast.success(rotateTarget ? '凭据已轮换' : '凭据已保存');
      setModal(null);
    }
  }

  return (
    <Dialog open={!!modal} onOpenChange={(open) => !open && setModal(null)}>
      {modal && (
        <DialogContent className='max-w-md'>
          <DialogHeader>
            <DialogTitle>{rotateTarget ? '轮换凭据' : '新增凭据'}</DialogTitle>
            <DialogDescription>
              {rotateTarget
                ? `${rotateTarget.store_name || '企业级'} · ${
                  KIND_OPTIONS.find((k) => k.value === rotateTarget.kind)?.label ?? rotateTarget.kind
                }`
                : '密钥只上行、不回落；列表里永远只看得到脱敏尾号。'}
            </DialogDescription>
          </DialogHeader>
          <form className='space-y-4' onSubmit={form.handleSubmit(onSubmit)}>
            {!rotateTarget && (
              <div className='grid grid-cols-2 gap-4'>
                <div className='space-y-2'>
                  <Label>归属</Label>
                  <Select value={storeId} onValueChange={(v) => form.setValue('store_id', v)}>
                    <SelectTrigger className='w-full'>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value=''>企业级（1688）</SelectItem>
                      {stores.map((s) => (
                        <SelectItem key={s.id} value={s.id}>
                          {s.name}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
                <div className='space-y-2'>
                  <Label>种类</Label>
                  <Select value={kind} onValueChange={(v) => form.setValue('kind', v as CredentialKind)}>
                    <SelectTrigger className='w-full'>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {KIND_OPTIONS.map((k) => (
                        <SelectItem key={k.value} value={k.value}>
                          {k.label}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
              </div>
            )}

            {KIND_FIELDS[kind].map((f) => (
              <div className='space-y-2' key={f.key}>
                <Label htmlFor={`cred-${f.key}`}>{f.label}</Label>
                <Input
                  id={`cred-${f.key}`}
                  type={f.type ?? 'text'}
                  autoComplete='off'
                  {...form.register(f.key as keyof CredentialForm)}
                />
              </div>
            ))}

            <div className='space-y-2'>
              <Label htmlFor='cred-expires'>到期时间（可空；轮换时留空 = 保留原到期时间）</Label>
              <Input id='cred-expires' type='datetime-local' {...form.register('expires_at')} />
            </div>

            {formError && <p className='text-sm text-destructive'>{formError}</p>}

            <DialogFooter>
              <Button type='button' variant='outline' onClick={() => setModal(null)} disabled={submitting}>
                取消
              </Button>
              <Button type='submit' disabled={submitting}>
                {submitting ? '保存中…' : rotateTarget ? '轮换' : '保存'}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      )}
    </Dialog>
  );
}
