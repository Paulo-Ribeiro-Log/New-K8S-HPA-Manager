import { useState, useEffect, useCallback } from "react";

export interface InAppNotification {
  id: string;
  title: string;
  message: string;
  severity: "critical" | "warning" | "info";
  timestamp: string;
  read: boolean;
}

export interface NotificationsResponse {
  notifications: InAppNotification[];
  unreadCount: number;
}

const API_BASE = "/api/v1/notifications/in-app";

export function useNotifications(pollingInterval = 10000) {
  const [notifications, setNotifications] = useState<InAppNotification[]>([]);
  const [unreadCount, setUnreadCount] = useState(0);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Buscar todas as notificações
  const fetchNotifications = useCallback(async () => {
    try {
      setLoading(true);
      setError(null);

      const response = await fetch(API_BASE, {
        headers: {
          Authorization: `Bearer ${localStorage.getItem("token") || "poc-token-123"}`,
        },
      });

      if (!response.ok) {
        throw new Error(`Failed to fetch notifications: ${response.statusText}`);
      }

      const data: NotificationsResponse = await response.json();
      setNotifications(data.notifications || []);
      setUnreadCount(data.unreadCount || 0);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Unknown error");
      console.error("Error fetching notifications:", err);
    } finally {
      setLoading(false);
    }
  }, []);

  // Marcar como lida
  const markAsRead = useCallback(async (id: string) => {
    try {
      const response = await fetch(`${API_BASE}/${id}/read`, {
        method: "PUT",
        headers: {
          Authorization: `Bearer ${localStorage.getItem("token") || "poc-token-123"}`,
        },
      });

      if (!response.ok) {
        throw new Error(`Failed to mark as read: ${response.statusText}`);
      }

      // Atualizar estado local
      setNotifications((prev) =>
        prev.map((n) => (n.id === id ? { ...n, read: true } : n))
      );
      setUnreadCount((prev) => Math.max(0, prev - 1));
    } catch (err) {
      console.error("Error marking notification as read:", err);
    }
  }, []);

  // Marcar todas como lidas
  const markAllAsRead = useCallback(async () => {
    try {
      const response = await fetch(`${API_BASE}/read-all`, {
        method: "PUT",
        headers: {
          Authorization: `Bearer ${localStorage.getItem("token") || "poc-token-123"}`,
        },
      });

      if (!response.ok) {
        throw new Error(`Failed to mark all as read: ${response.statusText}`);
      }

      // Atualizar estado local
      setNotifications((prev) => prev.map((n) => ({ ...n, read: true })));
      setUnreadCount(0);
    } catch (err) {
      console.error("Error marking all as read:", err);
    }
  }, []);

  // Limpar todas
  const clearAll = useCallback(async () => {
    try {
      const response = await fetch(API_BASE, {
        method: "DELETE",
        headers: {
          Authorization: `Bearer ${localStorage.getItem("token") || "poc-token-123"}`,
        },
      });

      if (!response.ok) {
        throw new Error(`Failed to clear notifications: ${response.statusText}`);
      }

      // Atualizar estado local
      setNotifications([]);
      setUnreadCount(0);
    } catch (err) {
      console.error("Error clearing notifications:", err);
    }
  }, []);

  // Polling automático
  useEffect(() => {
    // Buscar imediatamente
    fetchNotifications();

    // Configurar polling
    const interval = setInterval(fetchNotifications, pollingInterval);

    return () => clearInterval(interval);
  }, [fetchNotifications, pollingInterval]);

  return {
    notifications,
    unreadCount,
    loading,
    error,
    fetchNotifications,
    markAsRead,
    markAllAsRead,
    clearAll,
  };
}
