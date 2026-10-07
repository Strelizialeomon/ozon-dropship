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
import { taskPrepSheetAtom } from '../store';

/** 备料单（人工渠道 / 人工执行器）：地址 = 中转点、备注带 posting_number、不含买家个人信息（总纲 §7.3）。 */
export function PrepSheetDialog() {
  const [sheet, setSheet] = useAtom(taskPrepSheetAtom);

  async function copySheet(): Promise<void> {
    if (!sheet) return;
    const text = [
      `商品链接：${sheet.item_url}`,
      `规格/SKU：${sheet.sku}`,
      `数量：${sheet.qty}`,
      `收货地址：${sheet.address}`,
      `备注：${sheet.note}`,
      sheet.expected_days ? `期望时效：${sheet.expected_days} 天内` : '',
    ]
      .filter(Boolean)
      .join('\n');
    try {
      await navigator.clipboard.writeText(text);
      toast.success('备料单已复制');
    } catch {
      toast.error('复制失败，请手动选择文本');
    }
  }

  return (
    <Dialog open={!!sheet} onOpenChange={(open) => !open && setSheet(null)}>
      {sheet && (
        <DialogContent className='max-w-md'>
          <DialogHeader>
            <DialogTitle>备料单</DialogTitle>
            <DialogDescription>{sheet.posting_number} · 照着这单去 {sheet.platform} 人工下单</DialogDescription>
          </DialogHeader>
          <div className='space-y-3 text-sm'>
            <div>
              <span className='text-muted-foreground'>商品链接：</span>
              <a
                className='inline-flex items-center gap-1 text-primary underline'
                href={sheet.item_url}
                target='_blank'
                rel='noreferrer'
              >
                {sheet.item_url}
                <ExternalLink className='size-3.5' />
              </a>
            </div>
            <div>
              <span className='text-muted-foreground'>规格/SKU：</span>
              {sheet.sku}
            </div>
            <div>
              <span className='text-muted-foreground'>数量：</span>
              {sheet.qty}
            </div>
            <div>
              <span className='text-muted-foreground'>收货地址（中转点）：</span>
              <p className='mt-1 whitespace-pre-wrap rounded-md bg-muted p-2'>{sheet.address}</p>
            </div>
            <div>
              <span className='text-muted-foreground'>备注：</span>
              <p className='mt-1 whitespace-pre-wrap rounded-md bg-muted p-2'>{sheet.note}</p>
            </div>
            {sheet.expected_days !== null && (
              <div>
                <span className='text-muted-foreground'>期望时效：</span>
                {sheet.expected_days} 天内
              </div>
            )}
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
