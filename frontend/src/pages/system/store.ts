import { atom } from 'jotai';
import type { SystemStatusDTO } from '@/api/system';

export const systemStatusAtom = atom<SystemStatusDTO | null>(null);
export const systemLoadingAtom = atom<boolean>(false);
