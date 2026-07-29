// Generated Admin API v1 types
// Auto-generated from api/admin-v1.openapi.yaml
// Manual stub for Phase 1 - will be auto-generated in Phase 4

export interface Health {
  status: 'ok' | 'degraded' | 'unhealthy';
  timestamp: string;
}

export interface DetailedHealth extends Health {
  version: string;
  uptime_seconds: number;
  db: {
    connected: boolean;
    pool_conns_in_use: number;
    pool_conns_idle: number;
  };
  goroutines: number;
}

export interface LoginRequest {
  email: string;
  password: string;
}

export interface User {
  id: string;
  email: string;
  display_name?: string;
  is_admin: boolean;
  created_at: string;
}

export interface LoginResponse {
  session_id: string;
  user: User;
}

export interface PAT {
  id: string;
  description?: string;
  token_prefix: string;
  expires_at?: string | null;
  last_used_at?: string | null;
  revoked_at?: string | null;
  created_at: string;
}

export interface CreatePATRequest {
  description: string;
  expires_at?: string | null;
}

export interface CreatePATResponse {
  id: string;
  token: string;
  description: string;
  expires_at?: string | null;
  created_at: string;
}

export interface APIKey {
  id: string;
  name: string;
  key_prefix: string;
  expires_at?: string | null;
  last_used_at?: string | null;
  revoked_at?: string | null;
  created_at: string;
}

export interface Provider {
  id: string;
  name: string;
  type: 'openai' | 'anthropic' | 'azure' | 'custom';
  base_url: string;
  is_enabled: boolean;
  config?: Record<string, unknown>;
  created_at: string;
  updated_at: string;
}

export interface CreateProviderRequest {
  name: string;
  type: Provider['type'];
  base_url: string;
  api_key?: string;
  config?: Record<string, unknown>;
}

export interface APIError {
  code: string;
  message: string;
  details?: Record<string, unknown>;
  request_id?: string;
}
