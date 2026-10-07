import { type ClassValue, clsx } from 'clsx';
import { twMerge } from 'tailwind-merge';

/** shadcn/ui 约定的类名合并工具（Tailwind 冲突类以后者为准）。 */
export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}
