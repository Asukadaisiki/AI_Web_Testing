import { createContext, useContext, useEffect, useState } from "react";
import type { PropsWithChildren } from "react";

import { AUTH_UNAUTHORIZED_EVENT, getCurrentUser, login as loginRequest, logout as logoutRequest } from "../services/api";
import type { CurrentUser, LoginPayload } from "../types/api";

interface AuthContextValue {
  currentUser: CurrentUser | null;
  isAuthResolved: boolean;
  isAuthenticated: boolean;
  login: (payload: LoginPayload) => Promise<CurrentUser>;
  logout: () => Promise<void>;
}

const AuthContext = createContext<AuthContextValue | null>(null);

export function AuthProvider({ children }: PropsWithChildren) {
  const [currentUser, setCurrentUser] = useState<CurrentUser | null>(null);
  const [isAuthResolved, setIsAuthResolved] = useState(false);

  useEffect(() => {
    let cancelled = false;

    getCurrentUser()
      .then((user) => {
        if (cancelled) {
          return;
        }
        setCurrentUser(user);
      })
      .catch(() => {
        if (cancelled) {
          return;
        }
        setCurrentUser(null);
      })
      .finally(() => {
        if (!cancelled) {
          setIsAuthResolved(true);
        }
      });

    const handleUnauthorized = () => {
      if (!cancelled) {
        setCurrentUser(null);
        setIsAuthResolved(true);
      }
    };

    window.addEventListener(AUTH_UNAUTHORIZED_EVENT, handleUnauthorized);
    return () => {
      cancelled = true;
      window.removeEventListener(AUTH_UNAUTHORIZED_EVENT, handleUnauthorized);
    };
  }, []);

  async function login(payload: LoginPayload) {
    const user = await loginRequest(payload);
    setCurrentUser(user);
    setIsAuthResolved(true);
    return user;
  }

  async function logout() {
    try {
      await logoutRequest();
    } finally {
      setCurrentUser(null);
      setIsAuthResolved(true);
    }
  }

  return (
    <AuthContext.Provider
      value={{
        currentUser,
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
