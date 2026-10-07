import { Badge } from '@/components/ui/badge';
import type { LabelSpec, Tone } from '@/lib/labels';

const TONE_CLASS: Record<Tone, string> = {
  default: '',
  secondary: '',
  destructive: '',
  outline: '',
  success: 'border-transparent bg-emerald-100 text-emerald-800',
  warning: 'border-transparent bg-amber-100 text-amber-800',
  info: 'border-transparent bg-sky-100 text-sky-800',
};

const TONE_VARIANT: Record<Tone, 'default' | 'secondary' | 'destructive' | 'outline'> = {
  default: 'default',
  secondary: 'secondary',
  destructive: 'destructive',
  outline: 'outline',
  success: 'outline',
  warning: 'outline',
  info: 'outline',
};

/** 状态徽标：标签 + 色调（success/warning/info 是补充语义色，不改变 shadcn 基础变体）。 */
export function StatusBadge({ spec }: { spec: LabelSpec }) {
  return <Badge variant={TONE_VARIANT[spec.tone]} className={TONE_CLASS[spec.tone]}>{spec.text}</Badge>;
}
