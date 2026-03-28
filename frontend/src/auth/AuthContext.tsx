import { useQueryClient } from "@tanstack/react-query";
import { createContext, useContext, useEffect, useRef, useState } from "react";
import type { PropsWithChildren } from "react";

import {
  AUTH_UNAUTHORIZED_EVENT,
  getCurrentUser,
  login as loginRequest,
  logout as logoutRequest,
} from "../services/api";
import type { CurrentUser, LoginPayload } from "../types/api";

interface AuthContextValue {
  currentUser: CurrentUser | null;
  authErrorMessage: string | null;
  isAuthResolved: boolean;
  isAuthenticated: boolean;
  login: (payload: LoginPayload) => Promise<CurrentUser>;
  logout: () => Promise<void>;
}

const AuthContext = createContext<AuthContextValue | null>(null);

function getErrorStatus(error: unknown): number | null {
  if (typeof error !== "object" || error === null || !("status" in error)) {
    return null;
  }
  const status = (error as { status?: unknown }).status;
  return typeof status === "number" ? status : null;
}

export function AuthProvider({ children }: PropsWithChildren) {
  const queryClient = useQueryClient();
  const authBootstrapVersionRef = useRef(0);
  const [currentUser, setCurrentUser] = useState<CurrentUser | null>(null);
  const [authErrorMessage, setAuthErrorMessage] = useState<string | null>(null);
  const [isAuthResolved, setIsAuthResolved] = useState(false);

  useEffect(() => {
    let cancelled = false;
    const bootstrapVersion = authBootstrapVersionRef.current + 1;
    authBootstrapVersionRef.current = bootstrapVersion;

    getCurrentUser()
      .then((user) => {
        if (cancelled || authBootstrapVersionRef.current != bootstrapVersion) {
          return;
        }
        setCurrentUser(user);
        setAuthErrorMessage(null);
      })
      .catch((error: unknown) => {
        if (cancelled || authBootstrapVersionRef.current != bootstrapVersion) {
          return;
        }
        setCurrentUser(null);
        if (getErrorStatus(error) === 401) {
          setAuthErrorMessage(null);
          return;
        }
        setAuthErrorMessage(error instanceof Error ? error.message : "认证状态加载失败，请稍后重试。");
      })
      .finally(() => {
        if (!cancelled && authBootstrapVersionRef.current == bootstrapVersion) {
          setIsAuthResolved(true);
        }
      });

    const handleUnauthorized = () => {
      if (!cancelled) {
        authBootstrapVersionRef.current += 1;
        queryClient.clear();
        setCurrentUser(null);
        setAuthErrorMessage(null);
        setIsAuthResolved(true);
      }
    };

    window.addEventListener(AUTH_UNAUTHORIZED_EVENT, handleUnauthorized);
    return () => {
      cancelled = true;
      window.removeEventListener(AUTH_UNAUTHORIZED_EVENT, handleUnauthorized);
    };
  }, [queryClient]);

  async function login(payload: LoginPayload) {
    const user = await loginRequest(payload);
    authBootstrapVersionRef.current += 1;
    setCurrentUser(user);
    setAuthErrorMessage(null);
    setIsAuthResolved(true);
    return user;
  }

  async function logout() {
    try {
      await logoutRequest();
    } finally {
      authBootstrapVersionRef.current += 1;
      queryClient.clear();
      setCurrentUser(null);
      setAuthErrorMessage(null);
      setIsAuthResolved(true);
    }
  }

  return (
    <AuthContext.Provider
      value={{
        currentUser,
        authErrorMessage,
        isAuthResolved,
        isAuthenticated: currentUser !== null,
        login,
        logout,
      }}
    >
      {children}
    </AuthContext.Provider>
  );
}

export function useAuth() {
  const context = useContext(AuthContext);
  if (context === null) {
    throw new Error("useAuth must be used inside AuthProvider.");
  }
  return context;
}
