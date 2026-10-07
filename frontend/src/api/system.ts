// 系统状态（S1-A 已实现，形状照 backend/internal/store/system.go）。
import { request } from './client';

export interface StoreSyncStatusDTO {
  id: string;
  name: string;
  status: string;
  last_sync_at: string | null;
}

export interface QueueStatDTO {
  pending: number;
  active: number;
  scheduled: number;
  retry: number;
  archived: number; // 重试用尽落这里 = 失败任务
}

export interface FailedTaskDTO {
  id: string;
  type: string;
  queue: string;
  retried: number;
  max_retry: number;
  last_error: string;
  last_failed_at: string;
}

export interface SystemStatusDTO {
  stores: StoreSyncStatusDTO[];
  queues: Record<string, QueueStatDTO>;
  failed_tasks: FailedTaskDTO[];
}

export function getSystemStatus(): Promise<SystemStatusDTO> {
  return request<SystemStatusDTO>({ method: 'GET', url: '/api/system' });
}
