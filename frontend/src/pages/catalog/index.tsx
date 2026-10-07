import { useAtom, useAtomValue, useSetAtom } from 'jotai';
import { Plus, Search } from 'lucide-react';
import { useEffect } from 'react';
import { toast } from 'sonner';
import { storeOptionsAtom, useStoreOptionsActions } from '@/atoms/storeOptions';
import { ConfirmDialog } from '@/components/ConfirmDialog';
import { DataTable, type ServerPagination } from '@/components/DataTable';
import { PageHeader } from '@/components/PageHeader';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { formatMoney } from '@/lib/format';
import { channelLabel, platformLabel } from '@/lib/labels';
import { LinkFormDialog } from './components/LinkFormDialog';
import { OfferFormDialog } from './components/OfferFormDialog';
import { useLinkActions, useOfferActions } from './actions';
import {
  catalogTabAtom,
  linkDeletingAtom,
  linkFiltersAtom,
  linkModalAtom,
  linksAtom,
  linksLoadingAtom,
  linksTotalAtom,
  offerDeletingAtom,
  offerFiltersAtom,
  offerModalAtom,
  offersAtom,
  offersLoadingAtom,
  offersTotalAtom,
} from './store';

export default function CatalogPage() {
  const [tab, setTab] = useAtom(catalogTabAtom);

  // ── 货源商品 ──────────────────────────────────────────────────────────
  const offers = useAtomValue(offersAtom);
  const offersTotal = useAtomValue(offersTotalAtom);
  const offersLoading = useAtomValue(offersLoadingAtom);
  const [offerFilters, setOfferFilters] = useAtom(offerFiltersAtom);
  const setOfferModal = useSetAtom(offerModalAtom);
  const [offerDeleting, setOfferDeleting] = useAtom(offerDeletingAtom);

  // ── 按店映射 ──────────────────────────────────────────────────────────
  const links = useAtomValue(linksAtom);
  const linksTotal = useAtomValue(linksTotalAtom);
  const linksLoading = useAtomValue(linksLoadingAtom);
  const [linkFilters, setLinkFilters] = useAtom(linkFiltersAtom);
  const setLinkModal = useSetAtom(linkModalAtom);
  const [linkDeleting, setLinkDeleting] = useAtom(linkDeletingAtom);
  const storeOptions = useAtomValue(storeOptionsAtom);

  const offerActions = useOfferActions();
  const linkActions = useLinkActions();
  const { ensureStores } = useStoreOptionsActions();

  useEffect(() => {
    void ensureStores();
    void offerActions.loadOffers(offerFilters);
    void linkActions.loadLinks(linkFilters);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  async function onDeleteOffer(): Promise<void> {
    if (!offerDeleting) return;
    const ok = await offerActions.deleteOffer(offerDeleting.id);
    if (ok) toast.success('货源已删除');
    setOfferDeleting(null);
  }

  async function onDeleteLink(): Promise<void> {
    if (!linkDeleting) return;
    const ok = await linkActions.deleteLink(linkDeleting.id);
    if (ok) toast.success('映射已删除');
    setLinkDeleting(null);
  }

  const offerPagination: ServerPagination = {
    page: offerFilters.page,
    pageSize: offerFilters.page_size,
    total: offersTotal,
    onPageChange: (p) => offerActions.changePage(p),
  };
  const linkPagination: ServerPagination = {
    page: linkFilters.page,
    pageSize: linkFilters.page_size,
    total: linksTotal,
    onPageChange: (p) => linkActions.changePage(p),
  };

  return (
    <div>
      <PageHeader title='映射与报价' description='货源商品库 + 按店映射（主备货源、目标库存）' />

      <Tabs value={tab} onValueChange={(v) => setTab(v as typeof tab)}>
        <TabsList>
          <TabsTrigger value='offers'>货源商品</TabsTrigger>
          <TabsTrigger value='links'>按店映射</TabsTrigger>
        </TabsList>

        <TabsContent value='offers' className='mt-4 space-y-3'>
          <div className='flex flex-wrap items-center justify-between gap-2'>
            <div className='flex items-center gap-2'>
              <Select
                value={offerFilters.platform || '_all'}
                onValueChange={(v) => offerActions.applyFilters({ platform: v === '_all' ? '' : v })}
              >
                <SelectTrigger className='w-32'>
                  <SelectValue placeholder='平台' />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value='_all'>全部平台</SelectItem>
                  <SelectItem value='1688'>1688</SelectItem>
                  <SelectItem value='pdd'>拼多多</SelectItem>
                  <SelectItem value='taobao'>淘宝</SelectItem>
                </SelectContent>
              </Select>
              <Input
                className='w-52'
                placeholder='商品 ID / 名称'
                value={offerFilters.keyword}
                onChange={(e) => setOfferFilters((prev) => ({ ...prev, keyword: e.target.value }))}
                onKeyDown={(e) => e.key === 'Enter' && offerActions.applyFilters({ keyword: offerFilters.keyword })}
              />
              <Button
                variant='outline'
                size='icon'
                onClick={() => offerActions.applyFilters({ keyword: offerFilters.keyword })}
              >
                <Search className='size-4' />
              </Button>
            </div>
            <Button size='sm' onClick={() => setOfferModal({ mode: 'create' })}>
              <Plus className='size-4' />
              添加货源
            </Button>
          </div>

          <DataTable
            data={offers}
            loading={offersLoading}
            getRowId={(o) => o.id}
            pagination={offerPagination}
            emptyText='还没有货源商品'
            columns={[
              { id: 'platform', header: '平台', cell: ({ row }) => platformLabel(row.original.platform) },
              {
                id: 'item',
                header: '商品 / 规格',
                cell: ({ row }) => (
                  <div className='text-xs'>
                    <div className='font-mono'>{row.original.item_id}</div>
                    {row.original.sku_id && <div className='text-muted-foreground'>{row.original.sku_id}</div>}
                  </div>
                ),
              },
              { id: 'price', header: '采购价', cell: ({ row }) => formatMoney(row.original.purchase_price) },
              { id: 'freight', header: '境内运费', cell: ({ row }) => formatMoney(row.original.domestic_freight) },
              { id: 'stock', header: '库存', cell: ({ row }) => row.original.stock },
              {
                id: 'channel',
                header: '通道',
                cell: ({ row }) => <span className='text-xs'>{channelLabel(row.original.order_channel).text}</span>,
              },
              { id: 'followed', header: '已关注', cell: ({ row }) => (row.original.followed ? '是' : '否') },
              {
                id: 'url',
                header: '链接',
                cell: ({ row }) =>
                  row.original.url
                    ? (
                      <a
                        className='text-xs text-primary underline'
                        href={row.original.url}
                        target='_blank'
                        rel='noreferrer'
                      >
                        打开
                      </a>
                    )
                    : (
                      '—'
                    ),
              },
              {
                id: 'actions',
                header: '操作',
                cell: ({ row }) => (
                  <div className='flex gap-1'>
                    <Button
                      variant='ghost'
                      size='sm'
                      onClick={() => setOfferModal({ mode: 'edit', offer: row.original })}
                    >
                      编辑
                    </Button>
                    <Button
                      variant='ghost'
                      size='sm'
                      className='text-destructive hover:text-destructive'
                      onClick={() => setOfferDeleting(row.original)}
                    >
                      删除
                    </Button>
                  </div>
                ),
              },
            ]}
          />
        </TabsContent>

        <TabsContent value='links' className='mt-4 space-y-3'>
          <div className='flex flex-wrap items-center justify-between gap-2'>
            <div className='flex items-center gap-2'>
              <Select
                value={linkFilters.store_id || '_all'}
                onValueChange={(v) => linkActions.applyFilters({ store_id: v === '_all' ? '' : v })}
              >
                <SelectTrigger className='w-44'>
                  <SelectValue placeholder='全部店铺' />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value='_all'>全部店铺</SelectItem>
                  {storeOptions.map((s) => (
                    <SelectItem key={s.id} value={s.id}>
                      {s.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <Input
                className='w-52'
                placeholder='Ozon offer_id'
                value={linkFilters.keyword}
                onChange={(e) => setLinkFilters((prev) => ({ ...prev, keyword: e.target.value }))}
                onKeyDown={(e) => e.key === 'Enter' && linkActions.applyFilters({ keyword: linkFilters.keyword })}
              />
              <Button
                variant='outline'
                size='icon'
                onClick={() => linkActions.applyFilters({ keyword: linkFilters.keyword })}
              >
                <Search className='size-4' />
              </Button>
            </div>
            <Button size='sm' onClick={() => setLinkModal({ mode: 'create' })}>
              <Plus className='size-4' />
              添加映射
            </Button>
          </div>

          <DataTable
            data={links}
            loading={linksLoading}
            getRowId={(l) => l.id}
            pagination={linkPagination}
            emptyText='还没有映射'
            columns={[
              { id: 'store', header: '店铺', cell: ({ row }) => row.original.store_name },
              {
                id: 'ozon',
                header: 'Ozon offer_id',
                cell: ({ row }) => <span className='font-mono text-xs'>{row.original.ozon_offer_id}</span>,
              },
              {
                id: 'supplier',
                header: '货源',
                cell: ({ row }) => (
                  <div className='text-xs'>
                    <div>{row.original.supplier_offer ? platformLabel(row.original.supplier_offer.platform) : '—'}</div>
                    <div className='font-mono text-muted-foreground'>
                      {row.original.supplier_offer?.item_id ?? row.original.supplier_offer_id}
                    </div>
                  </div>
                ),
              },
              { id: 'priority', header: '优先级', cell: ({ row }) => row.original.priority },
              { id: 'target', header: '目标库存', cell: ({ row }) => row.original.target_stock },
              {
                id: 'pushed',
                header: '已推库存',
                cell: ({ row }) => (row.original.last_pushed_stock === null ? '—' : row.original.last_pushed_stock),
              },
              {
                id: 'actions',
                header: '操作',
                cell: ({ row }) => (
                  <div className='flex gap-1'>
                    <Button
                      variant='ghost'
                      size='sm'
                      onClick={() => setLinkModal({ mode: 'edit', link: row.original })}
                    >
                      编辑
                    </Button>
                    <Button
                      variant='ghost'
                      size='sm'
                      className='text-destructive hover:text-destructive'
                      onClick={() => setLinkDeleting(row.original)}
                    >
                      删除
                    </Button>
                  </div>
                ),
              },
            ]}
          />
        </TabsContent>
      </Tabs>

      <OfferFormDialog />
      <LinkFormDialog />
      <ConfirmDialog
        open={!!offerDeleting}
        onOpenChange={(open) => !open && setOfferDeleting(null)}
        title='删除这个货源商品？'
        description='引用了它的映射会失去货源，请先改映射。'
        confirmText='删除'
        destructive
        onConfirm={() => void onDeleteOffer()}
      />
      <ConfirmDialog
        open={!!linkDeleting}
        onOpenChange={(open) => !open && setLinkDeleting(null)}
        title='删除这条映射？'
        description='该店该商品将回到「未映射」，不再参与库存同步与自动采购。'
        confirmText='删除'
        destructive
        onConfirm={() => void onDeleteLink()}
      />
    </div>
  );
}
