import { useEffect } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Navigate, Outlet, useLocation } from "react-router-dom";

import { LoadingBlock } from "../../shared/ui/PageFeedback";
import { AUTH_UNAUTHORIZED_EVENT } from "../../shared/api/client";
import { getCurrentUser } from "./api";

export const currentUserQueryKey = ["current-user"] as const;

export function AuthGuard() {
  const location = useLocation();
  const queryClient = useQueryClient();
  const currentUserQuery = useQuery({
    queryKey: currentUserQueryKey,
    queryFn: async () => {
      try {
        return await getCurrentUser();
      } catch {
        return null;
      }
    },
    retry: false,
  });

  useEffect(() => {
    const handleUnauthorized = () => {
      queryClient.setQueryData(currentUserQueryKey, null);
      void queryClient.invalidateQueries({ queryKey: currentUserQueryKey });
    };
    window.addEventListener(AUTH_UNAUTHORIZED_EVENT, handleUnauthorized);
    return () => {
      window.removeEventListener(AUTH_UNAUTHORIZED_EVENT, handleUnauthorized);
    };
  }, [queryClient]);

  if (currentUserQuery.isLoading) {
    return <LoadingBlock />;
  }

  if (!currentUserQuery.data) {
    return (
      <Navigate
        to="/login"
        replace
        state={{ from: `${location.pathname}${location.search}` }}
      />
    );
  }

  return <Outlet />;
}
