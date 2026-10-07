import { useAtom, useAtomValue, useSetAtom } from 'jotai';
import { KeyRound, Plus } from 'lucide-react';
import { useEffect } from 'react';
import { toast } from 'sonner';
import type { CredentialDTO } from '@/api/credentials';
import { ConfirmDialog } from '@/components/ConfirmDialog';
import { DataTable } from '@/components/DataTable';
import { PageHeader } from '@/components/PageHeader';
import { StatusBadge } from '@/components/StatusBadge';
import { Button } from '@/components/ui/button';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { formatDate, formatDateTime } from '@/lib/format';
import { credentialKind, shopMode, shopStatus } from '@/lib/labels';
import { CredentialFormDialog } from './components/CredentialFormDialog';
import { StoreFormDialog } from './components/StoreFormDialog';
import { useCredentialActions, useStoreActions } from './actions';
import {
  credentialModalAtom,
  credentialsAtom,
  credentialsLoadingAtom,
  credentialStoreFilterAtom,
  pageTabAtom,
  storeDeletingAtom,
  storeModalAtom,
  storesAtom,
  storesLoadingAtom,
} from './store';

/** 到期高亮：≤14 天黄、≤7 天红（子 spec 验收：到期 ≤ 14 天高亮）。 */
function expiryClass(days: number | null): string {
  if (days === null) return '';
  if (days < 0) return 'font-medium text-destructive';
  if (days <= 7) return 'text-destructive';
  if (days <= 14) return 'text-amber-700';
  return '';
}

function expiryText(c: CredentialDTO): string {
  if (c.days_left === null) return '—';
  if (c.days_left < 0) return `已过期 ${-c.days_left} 天`;
  return `${c.days_left} 天`;
}

export default function StoresPage() {
  const [tab, setTab] = useAtom(pageTabAtom);
  const stores = useAtomValue(storesAtom);
  const storesLoading = useAtomValue(storesLoadingAtom);
  const credentials = useAtomValue(credentialsAtom);
  const credentialsLoading = useAtomValue(credentialsLoadingAtom);
  const [credStoreFilter, setCredStoreFilter] = useAtom(credentialStoreFilterAtom);
  const setStoreModal = useSetAtom(storeModalAtom);
  const [storeDeleting, setStoreDeleting] = useAtom(storeDeletingAtom);
  const setCredentialModal = useSetAtom(credentialModalAtom);

  const { loadStores, removeStore } = useStoreActions();
  const { loadCredentials } = useCredentialActions();

  useEffect(() => {
    void loadStores();
    void loadCredentials();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  async function onDeleteStore(): Promise<void> {
    if (!storeDeleting) return;
    const ok = await removeStore(storeDeleting.id);
    if (ok) toast.success('店铺已删除');
    setStoreDeleting(null);
  }

  return (
    <div>
      <PageHeader title='店铺与凭据' description='10+ 店的接入信息与密钥（只显示脱敏尾号）' />

      <Tabs value={tab} onValueChange={(v) => setTab(v as 'stores' | 'credentials')}>
        <TabsList>
          <TabsTrigger value='stores'>店铺</TabsTrigger>
          <TabsTrigger value='credentials'>凭据</TabsTrigger>
        </TabsList>

        <TabsContent value='stores' className='mt-4 space-y-3'>
          <div className='flex justify-end'>
            <Button size='sm' onClick={() => setStoreModal({ mode: 'create' })}>
              <Plus className='size-4' />
              新建店铺
            </Button>
          </div>
          <DataTable
            data={stores}
            loading={storesLoading}
            getRowId={(s) => s.id}
            emptyText='还没有店铺，先建一家'
            columns={[
              { id: 'name', header: '店铺名', cell: ({ row }) => row.original.name },
              { id: 'mode', header: '模式', cell: ({ row }) => shopMode(row.original.mode) },
              {
                id: 'client_id',
                header: 'Client-Id',
                cell: ({ row }) => <span className='font-mono text-xs'>{row.original.client_id || '—'}</span>,
              },
              { id: 'currency', header: '币种', cell: ({ row }) => row.original.currency },
              { id: 'push', header: '推送', cell: ({ row }) => (row.original.push_enabled ? '已开' : '未开') },
              { id: 'ship_early', header: '提前备货', cell: ({ row }) => (row.original.ship_early ? '是' : '否') },
              { id: 'last_sync', header: '最近同步', cell: ({ row }) => formatDateTime(row.original.last_sync_at) },
              {
                id: 'status',
                header: '状态',
                cell: ({ row }) => <StatusBadge spec={shopStatus(row.original.status)} />,
              },
              {
                id: 'actions',
                header: '操作',
                cell: ({ row }) => (
                  <div className='flex gap-1'>
                    <Button
                      variant='ghost'
                      size='sm'
                      onClick={() => setStoreModal({ mode: 'edit', store: row.original })}
                    >
                      编辑
                    </Button>
                    <Button
                      variant='ghost'
                      size='sm'
                      className='text-destructive hover:text-destructive'
                      onClick={() => setStoreDeleting(row.original)}
                    >
                      删除
                    </Button>
                  </div>
                ),
              },
            ]}
          />
        </TabsContent>

        <TabsContent value='credentials' className='mt-4 space-y-3'>
          <div className='flex items-center justify-between'>
            <Select
              value={credStoreFilter || '_all'}
              onValueChange={(v) => {
                const val = v === '_all' ? '' : v;
                setCredStoreFilter(val);
                void loadCredentials(val || undefined);
              }}
            >
              <SelectTrigger className='w-52'>
                <SelectValue placeholder='按店铺过滤' />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value='_all'>全部</SelectItem>
                <SelectItem value=''>企业级</SelectItem>
                {stores.map((s) => (
                  <SelectItem key={s.id} value={s.id}>
                    {s.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Button size='sm' onClick={() => setCredentialModal({ mode: 'create' })}>
              <KeyRound className='size-4' />
              新增/更新凭据
            </Button>
          </div>
          <DataTable
            data={credentials}
            loading={credentialsLoading}
            getRowId={(c) => c.id}
            emptyText='还没有凭据'
            columns={[
              { id: 'store', header: '归属', cell: ({ row }) => row.original.store_name || '企业级' },
              { id: 'kind', header: '种类', cell: ({ row }) => credentialKind(row.original.kind) },
              {
                id: 'masked',
                header: '脱敏尾号',
                cell: ({ row }) => <span className='font-mono text-xs'>{row.original.masked || '—'}</span>,
              },
              { id: 'expires', header: '到期时间', cell: ({ row }) => formatDate(row.original.expires_at) },
              {
                id: 'days_left',
                header: '剩余',
                cell: ({ row }) => (
                  <span className={expiryClass(row.original.days_left)}>{expiryText(row.original)}</span>
                ),
              },
              { id: 'verified', header: '最近校验', cell: ({ row }) => formatDateTime(row.original.last_verified_at) },
              { id: 'rotated', header: '最近轮换', cell: ({ row }) => formatDateTime(row.original.rotated_at) },
              {
                id: 'actions',
                header: '操作',
                cell: ({ row }) => (
                  <Button
                    variant='ghost'
                    size='sm'
                    onClick={() => setCredentialModal({ mode: 'rotate', credential: row.original })}
                  >
                    轮换
                  </Button>
                ),
              },
            ]}
          />
        </TabsContent>
      </Tabs>

      <StoreFormDialog />
      <CredentialFormDialog />
      <ConfirmDialog
        open={!!storeDeleting}
        onOpenChange={(open) => !open && setStoreDeleting(null)}
        title={`删除店铺「${storeDeleting?.name ?? ''}」？`}
        description='软删除；该店的凭据与映射不受影响，但不再参与拉单。'
        confirmText='删除'
        destructive
        onConfirm={() => void onDeleteStore()}
      />
    </div>
  );
}
