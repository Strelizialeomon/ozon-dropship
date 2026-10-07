import { useAtom, useAtomValue, useSetAtom } from 'jotai';
import { Download, Plus } from 'lucide-react';
import { useEffect, useMemo } from 'react';
import { toast } from 'sonner';
import { handoverExportUrl, type ShipmentListRowDTO } from '@/api/shipments';
import { storeOptionsAtom, useStoreOptionsActions } from '@/atoms/storeOptions';
import { ConfirmDialog } from '@/components/ConfirmDialog';
import { DataTable, type ServerPagination } from '@/components/DataTable';
import { PageHeader } from '@/components/PageHeader';
import { StatusBadge } from '@/components/StatusBadge';
import { Button } from '@/components/ui/button';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { formatDateTime } from '@/lib/format';
import { orderStatus, relayKind, tplIntegration, TRACKING_SOURCE } from '@/lib/labels';
import { RelayPointFormDialog } from './components/RelayPointFormDialog';
import { TrackingDialog } from './components/TrackingDialog';
import { useRelayPointActions, useShipmentActions } from './actions';
import {
  relayDeletingAtom,
  relayModalAtom,
  relayPointsAtom,
  relayPointsLoadingAtom,
  shipmentFiltersAtom,
  shipmentReceiveTargetAtom,
  shipmentsAtom,
  shipmentShipTargetAtom,
  shipmentsLoadingAtom,
  shipmentsTotalAtom,
  shipmentSubmittingAtom,
  shipmentTabAtom,
  shipmentTrackingTargetAtom,
} from './store';

/** 行上能做什么：状态推进链 + 后端给的 tracking_action（总纲 §5.7 / §7.4）。 */
function rowActions(row: ShipmentListRowDTO) {
  const atRelayOrLater = row.order_status === 'at_relay' || row.order_status === 'handed_over';
  return {
    receive: row.order_status === 'inbound',
    ship: row.order_status === 'at_relay',
    label: atRelayOrLater,
    tracking: row.tracking_action === 'set' && atRelayOrLater,
  };
}

export default function ShipmentsPage() {
  const [tab, setTab] = useAtom(shipmentTabAtom);
  const shipments = useAtomValue(shipmentsAtom);
  const shipmentsTotal = useAtomValue(shipmentsTotalAtom);
  const shipmentsLoading = useAtomValue(shipmentsLoadingAtom);
  const filters = useAtomValue(shipmentFiltersAtom);
  const submitting = useAtomValue(shipmentSubmittingAtom);
  const [receiveTarget, setReceiveTarget] = useAtom(shipmentReceiveTargetAtom);
  const [shipTarget, setShipTarget] = useAtom(shipmentShipTargetAtom);
  const setTrackingTarget = useSetAtom(shipmentTrackingTargetAtom);

  const relayPoints = useAtomValue(relayPointsAtom);
  const relayPointsLoading = useAtomValue(relayPointsLoadingAtom);
  const setRelayModal = useSetAtom(relayModalAtom);
  const [relayDeleting, setRelayDeleting] = useAtom(relayDeletingAtom);
  const storeOptions = useAtomValue(storeOptionsAtom);

  const { loadShipments, reloadShipments, applyFilters, changePage, receive, ship, openLabel } = useShipmentActions();
  const relayActions = useRelayPointActions();
  const { ensureStores } = useStoreOptionsActions();

  const storeName = useMemo(() => new Map(storeOptions.map((s) => [s.id, s.name])), [storeOptions]);
  const relayName = useMemo(() => new Map(relayPoints.map((p) => [p.id, p.name])), [relayPoints]);

  useEffect(() => {
    void ensureStores();
    relayActions.loadRelayPoints();
    void loadShipments(filters);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  async function onReceive(): Promise<void> {
    if (!receiveTarget) return;
    const ok = await receive(receiveTarget.order_id);
    if (ok) {
      toast.success('已登记签收');
      void reloadShipments();
    }
    setReceiveTarget(null);
  }

  async function onShip(): Promise<void> {
    if (!shipTarget) return;
    const res = await ship(shipTarget.order_id);
    if (res.ok) {
      toast.success('已备货交运', { description: res.notes?.join('；') || undefined });
      void reloadShipments();
    }
    setShipTarget(null);
  }

  async function onDeleteRelay(): Promise<void> {
    if (!relayDeleting) return;
    const ok = await relayActions.removeRelayPoint(relayDeleting.id);
    if (ok) toast.success('中转点已删除');
    setRelayDeleting(null);
  }

  const pagination: ServerPagination = {
    page: filters.page,
    pageSize: filters.page_size,
    total: shipmentsTotal,
    onPageChange: (p) => changePage(p),
  };

  const selectedRelay = relayPoints.find((p) => p.id === filters.relay_point_id);

  return (
    <div>
      <PageHeader title='中转点与打包' description='国内段到货签收 → 备货贴单 → 交运；两类中转点（货代仓 / 自有仓）' />

      <Tabs value={tab} onValueChange={(v) => setTab(v as typeof tab)}>
        <TabsList>
          <TabsTrigger value='handover'>打包交接</TabsTrigger>
          <TabsTrigger value='relay-points'>中转点</TabsTrigger>
        </TabsList>

        <TabsContent value='handover' className='mt-4 space-y-3'>
          <div className='flex flex-wrap items-center gap-2'>
            <Select
              value={filters.relay_point_id || '_all'}
              onValueChange={(v) => applyFilters({ relay_point_id: v === '_all' ? '' : v })}
            >
              <SelectTrigger className='w-44'>
                <SelectValue placeholder='全部中转点' />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value='_all'>全部中转点</SelectItem>
                {relayPoints.map((p) => (
                  <SelectItem key={p.id} value={p.id}>
                    {p.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>

            <Select
              value={filters.stage || '_all'}
              onValueChange={(v) => applyFilters({ stage: (v === '_all' ? '' : v) as typeof filters.stage })}
            >
              <SelectTrigger className='w-40'>
                <SelectValue placeholder='全部阶段' />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value='_all'>全部阶段</SelectItem>
                <SelectItem value='inbound'>国内在途</SelectItem>
                <SelectItem value='at_relay'>待打包（已签收）</SelectItem>
                <SelectItem value='handed_over'>已交运</SelectItem>
              </SelectContent>
            </Select>

            <Button
              variant='outline'
              size='sm'
              className='ml-auto'
              disabled={!selectedRelay}
              title={selectedRelay ? `导出「${selectedRelay.name}」的对照表` : '先选一个中转点再导出'}
              onClick={() => {
                if (selectedRelay) window.open(handoverExportUrl(selectedRelay.id), '_blank', 'noopener');
              }}
            >
              <Download className='size-4' />
              导出交接对照表
            </Button>
          </div>

          <DataTable
            data={shipments}
            loading={shipmentsLoading}
            getRowId={(s) => s.order_id}
            pagination={pagination}
            emptyText='没有待处理的包裹'
            columns={[
              {
                id: 'posting',
                header: 'posting',
                accessorFn: (s) => s.posting_number,
                cell: ({ row }) => <span className='font-mono text-xs'>{row.original.posting_number}</span>,
              },
              {
                id: 'store',
                header: '店铺',
                accessorFn: (s) => storeName.get(s.store_id) ?? s.store_id,
                cell: ({ row }) => storeName.get(row.original.store_id) ?? row.original.store_id,
              },
              {
                id: 'status',
                header: '订单状态',
                accessorFn: (s) => s.order_status,
                cell: ({ row }) => <StatusBadge spec={orderStatus(row.original.order_status)} />,
              },
              {
                id: 'relay',
                header: '中转点',
                accessorFn: (s) => (s.relay_point_id ? relayName.get(s.relay_point_id) ?? '' : ''),
                cell: ({ row }) =>
                  row.original.relay_point_id
                    ? relayName.get(row.original.relay_point_id) ?? row.original.relay_point_id
                    : '—',
              },
              {
                id: 'domestic',
                header: '国内件',
                accessorFn: (s) => s.domestic_tracking_no,
                cell: ({ row }) => (
                  <div className='text-xs'>
                    <div className='font-mono'>{row.original.domestic_tracking_no || '—'}</div>
                    {row.original.domestic_carrier && (
                      <div className='text-muted-foreground'>{row.original.domestic_carrier}</div>
                    )}
                  </div>
                ),
              },
              {
                id: 'deadline',
                header: '发货截止',
                accessorFn: (s) => s.ship_deadline ?? '',
                cell: ({ row }) => {
                  const d = row.original.ship_deadline;
                  const urgent = d && new Date(d).getTime() - Date.now() < 24 * 3600_000;
                  return <span className={urgent ? 'text-amber-700' : undefined}>{formatDateTime(d)}</span>;
                },
              },
              {
                id: 'tracking',
                header: '国际段单号',
                accessorFn: (s) => s.shipment?.tracking_no ?? '',
                cell: ({ row }) => {
                  const sh = row.original.shipment;
                  return (
                    <div className='text-xs'>
                      <div className='font-mono'>{sh?.tracking_no || '—'}</div>
                      {sh?.tracking_source && (
                        <div className='text-muted-foreground'>
                          {TRACKING_SOURCE[sh.tracking_source] ?? sh.tracking_source}
                        </div>
                      )}
                    </div>
                  );
                },
              },
              {
                id: 'tpl',
                header: '物流方式',
                accessorFn: (s) => s.tpl_integration_type ?? '',
                cell: ({ row }) => (
                  <span className='text-xs text-muted-foreground'>
                    {tplIntegration(row.original.tpl_integration_type)}
                  </span>
                ),
              },
              {
                id: 'handed',
                header: '交运时间',
                accessorFn: (s) => s.shipment?.handed_over_at ?? '',
                cell: ({ row }) => formatDateTime(row.original.shipment?.handed_over_at ?? null),
              },
              {
                id: 'actions',
                header: '操作',
                cell: ({ row }) => {
                  const s = row.original;
                  const a = rowActions(s);
                  return (
                    <div className='flex flex-wrap gap-1'>
                      {a.receive && (
                        <Button
                          variant='secondary'
                          size='sm'
                          disabled={submitting}
                          onClick={() => setReceiveTarget(s)}
                        >
                          签收
                        </Button>
                      )}
                      {a.ship && (
                        <Button
                          variant='secondary'
                          size='sm'
                          disabled={submitting}
                          onClick={() => setShipTarget(s)}
                        >
                          备货交运
                        </Button>
                      )}
                      {a.label && (
                        <Button variant='ghost' size='sm' onClick={() => void openLabel(s.order_id)}>
                          面单
                        </Button>
                      )}
                      {a.tracking && (
                        <Button variant='ghost' size='sm' onClick={() => setTrackingTarget(s)}>
                          传单号
                        </Button>
                      )}
                    </div>
                  );
                },
              },
            ]}
          />
        </TabsContent>

        <TabsContent value='relay-points' className='mt-4 space-y-3'>
          <div className='flex justify-end'>
            <Button size='sm' onClick={() => setRelayModal({ mode: 'create' })}>
              <Plus className='size-4' />
              新建中转点
            </Button>
          </div>
          <DataTable
            data={relayPoints}
            loading={relayPointsLoading}
            getRowId={(p) => p.id}
            emptyText='还没有中转点，先建一个'
            columns={[
              { id: 'name', header: '名称', accessorFn: (p) => p.name, cell: ({ row }) => row.original.name },
              {
                id: 'kind',
                header: '类型',
                accessorFn: (p) => p.kind,
                cell: ({ row }) => relayKind(row.original.kind),
              },
              {
                id: 'address',
                header: '收货地址',
                accessorFn: (p) => p.address,
                cell: ({ row }) => (
                  <span className='line-clamp-2 max-w-md text-xs' title={row.original.address}>
                    {row.original.address || '—'}
                  </span>
                ),
              },
              {
                id: 'contact',
                header: '联系人',
                accessorFn: (p) => p.contact,
                cell: ({ row }) => row.original.contact || '—',
              },
              {
                id: 'status',
                header: '状态',
                accessorFn: (p) => p.status,
                cell: ({ row }) => (row.original.status === 'active' ? '启用' : '停用'),
              },
              {
                id: 'actions',
                header: '操作',
                cell: ({ row }) => (
                  <div className='flex gap-1'>
                    <Button
                      variant='ghost'
                      size='sm'
                      onClick={() => setRelayModal({ mode: 'edit', point: row.original })}
                    >
                      编辑
                    </Button>
                    <Button
                      variant='ghost'
                      size='sm'
                      className='text-destructive hover:text-destructive'
                      onClick={() => setRelayDeleting(row.original)}
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

      <RelayPointFormDialog />
      <TrackingDialog />

      <ConfirmDialog
        open={!!receiveTarget}
        onOpenChange={(open) => !open && setReceiveTarget(null)}
        title='登记中转点签收？'
        description={`${
          receiveTarget?.posting_number ?? ''
        } 的国内件已到中转点（货代回传 / 自有仓扫码），订单推进到「中转点已收」。`}
        confirmText='登记签收'
        busy={submitting}
        onConfirm={() => void onReceive()}
      />
      <ConfirmDialog
        open={!!shipTarget}
        onOpenChange={(open) => !open && setShipTarget(null)}
        title='备货并交运？'
        description={`${
          shipTarget?.posting_number ?? ''
        } 将调 Ozon 备货（贴单交付）；返回后系统复核 substatus，ship_failed 会进异常池。`}
        confirmText='备货交运'
        busy={submitting}
        onConfirm={() => void onShip()}
      />
      <ConfirmDialog
        open={!!relayDeleting}
        onOpenChange={(open) => !open && setRelayDeleting(null)}
        title={`删除中转点「${relayDeleting?.name ?? ''}」？`}
        description='已配为该默认中转点的店铺需要改配。'
        confirmText='删除'
        destructive
        busy={submitting}
        onConfirm={() => void onDeleteRelay()}
      />
    </div>
  );
}
