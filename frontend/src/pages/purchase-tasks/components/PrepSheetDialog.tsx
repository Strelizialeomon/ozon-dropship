import { useAtom } from 'jotai';
import { Copy, ExternalLink } from 'lucide-react';
import { toast } from 'sonner';
import { Button } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { formatDateTime, formatMoney } from '@/lib/format';
import { taskPrepSheetAtom } from '../store';

/** 备料单（后端 BuildMaterialSheet）：地址 = 中转点、备注带 posting_number、不含买家个人信息（总纲 §7.3）。 */
export function PrepSheetDialog() {
  const [sheet, setSheet] = useAtom(taskPrepSheetAtom);

  async function copySheet(): Promise<void> {
    if (!sheet) return;
    try {
      // 后端给了人读版文本（text），直接复制它，别自己拼
      await navigator.clipboard.writeText(sheet.text);
      toast.success('备料单已复制');
    } catch {
      toast.error('复制失败，请手动选择文本');
    }
  }

  return (
    <Dialog open={!!sheet} onOpenChange={(open) => !open && setSheet(null)}>
      {sheet && (
        <DialogContent className='max-w-lg'>
          <DialogHeader>
            <DialogTitle>备料单</DialogTitle>
            <DialogDescription>{sheet.posting_number} · 照着这单去平台人工下单</DialogDescription>
          </DialogHeader>
          <div className='space-y-3 text-sm'>
            <div>
              <span className='text-muted-foreground'>收货（中转点）：</span>
              {sheet.receiver}
              {sheet.contact ? `（联系人：${sheet.contact}）` : ''}
            </div>
            <div>
              <span className='text-muted-foreground'>收货地址：</span>
              <p className='mt-1 whitespace-pre-wrap rounded-md bg-muted p-2'>{sheet.address || '—'}</p>
            </div>
            <div>
              <span className='text-muted-foreground'>期望时效：</span>
              {sheet.deadline ? `${formatDateTime(sheet.deadline)} 前送达` : '—'}
            </div>
            <div>
              <span className='text-muted-foreground'>备注（务必带上 posting）：</span>
              <p className='mt-1 whitespace-pre-wrap rounded-md bg-muted p-2'>{sheet.note || sheet.posting_number}</p>
            </div>

            <div className='overflow-hidden rounded-md border'>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>商品</TableHead>
                    <TableHead>规格</TableHead>
                    <TableHead>数量</TableHead>
                    <TableHead>参考价</TableHead>
                    <TableHead>链接</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {sheet.items.length === 0 && (
                    <TableRow>
                      <TableCell colSpan={5} className='text-center text-muted-foreground'>
                        没有商品行
                      </TableCell>
                    </TableRow>
                  )}
                  {sheet.items.map((it, i) => (
                    <TableRow key={`${it.item_id}-${i}`}>
                      <TableCell className='font-mono text-xs'>{it.item_id}</TableCell>
                      <TableCell className='font-mono text-xs'>{it.sku_id || '—'}</TableCell>
                      <TableCell>{it.qty}</TableCell>
                      <TableCell>{formatMoney(it.price, it.currency)}</TableCell>
                      <TableCell>
                        {it.url
                          ? (
                            <a
                              className='inline-flex items-center gap-1 text-primary underline'
                              href={it.url}
                              target='_blank'
                              rel='noreferrer'
                            >
                              打开
                              <ExternalLink className='size-3.5' />
                            </a>
                          )
                          : '—'}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          </div>
          <DialogFooter>
            <Button variant='outline' onClick={() => setSheet(null)}>
              关闭
            </Button>
            <Button onClick={() => void copySheet()}>
              <Copy className='size-4' />
              复制备料单
            </Button>
          </DialogFooter>
        </DialogContent>
      )}
    </Dialog>
  );
}
