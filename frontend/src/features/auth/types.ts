export interface CurrentUser {
  id: number;
  email: string;
  display_name: string;
}

export interface LoginPayload {
  email: string;
  password: string;
}

export interface LogoutResponse {
  success: boolean;
}
