// 凭据（S1-A 已实现，形状照 backend/internal/store/credential.go 的 Meta / credentialReq）。
// 只返回脱敏尾号与剩余天数；明文只在 POST 时上行。
import { request } from './client';

export type CredentialKind = 'ozon_api_key' | 'alibaba_app' | 'alibaba_token';

export interface CredentialDTO {
  id: string;
  store_id: string; // 空 = 企业级
  store_name: string;
  kind: CredentialKind;
  masked: string; // 如 ****abcd
  expires_at: string | null;
  days_left: number | null;
  last_verified_at: string | null;
  rotated_at: string | null;
}

export interface CredentialReq {
  store_id: string; // 空 = 企业级
  kind: CredentialKind;
  payload: Record<string, string>;
  expires_at?: string | null; // 不传 = 保留原到期时间（轮换不误抹）
}

/** GET /api/credentials?store_id=xxx
 *  storeId: undefined = 全部（不传参数）；'' = 企业级（后端语义：传空串 = store_id IS NULL）；其他 = 店 ID。 */
export function listCredentials(storeId?: string): Promise<CredentialDTO[]> {
  return request<CredentialDTO[]>({
    method: 'GET',
    url: '/api/credentials',
    params: storeId === undefined ? undefined : { store_id: storeId },
  });
}

/** POST /api/credentials（同店同 kind 覆盖 = 轮换）。 */
export function putCredential(body: CredentialReq): Promise<CredentialDTO> {
  return request<CredentialDTO>({ method: 'POST', url: '/api/credentials', data: body });
}
