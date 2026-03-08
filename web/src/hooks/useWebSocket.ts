import { useEffect, useState, useCallback, useRef } from "react";
import { HealthResponse, StatusResponse, WebSocketMessage } from "../types/api";
import { createMockWebSocket } from "../utils/mockData";

interface UseWebSocketReturn {
  health: HealthResponse | null;
  status: StatusResponse | null;
  loading: boolean;
  error: string | null;
  wsConnected: boolean;
  triggerDiscovery: () => Promise<void>;
}

// Check if we're in demo mode
const isDemoMode = typeof __DEMO_MODE__ !== 'undefined' && __DEMO_MODE__;

/**
 * Hook for WebSocket connection to EMS backend
 * Automatically switches to mock data in demo mode
 */
export function useWebSocket(): UseWebSocketReturn {
  const [health, setHealth] = useState<HealthResponse | null>(null);
  const [status, setStatus] = useState<StatusResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [wsConnected, setWsConnected] = useState(false);
  const [, setDiscoveryPending] = useState(false);

  const wsRef = useRef<WebSocket | null>(null);
  const reconnectTimeoutRef = useRef<number | null>(null);
  const reconnectAttemptsRef = useRef(0);
  const isConnectingRef = useRef(false);
  const mockWsRef = useRef<{ close: () => void } | null>(null);

  const connectWebSocket = useCallback(() => {
    // Prevent duplicate connections
    if (
      isConnectingRef.current ||
      (wsRef.current && wsRef.current.readyState === WebSocket.OPEN)
    ) {
      console.log("Already connecting or connected, skipping...");
      return;
    }

    isConnectingRef.current = true;

    // Clear any existing reconnect timeout
    if (reconnectTimeoutRef.current) {
      clearTimeout(reconnectTimeoutRef.current);
      reconnectTimeoutRef.current = null;
    }

    // Close existing connection if any
    if (wsRef.current) {
      wsRef.current.close();
    }

    const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
    const wsUrl = `${protocol}//${window.location.host}/api/ws`;

    console.log("Connecting to WebSocket:", wsUrl);

    const ws = new WebSocket(wsUrl);
    wsRef.current = ws;

    ws.onopen = () => {
      console.log("WebSocket connected");
      setWsConnected(true);
      setError(null);
      setLoading(false);
      reconnectAttemptsRef.current = 0;
      isConnectingRef.current = false;
    };

    ws.onmessage = (event) => {
      try {
        const data: WebSocketMessage = JSON.parse(event.data);

        if (data.type === "status_update") {
          setHealth(data.health);
          setStatus(data.status);
          setError(null);
        }
      } catch (err) {
        console.error("Failed to parse WebSocket message:", err);
        setError("Failed to parse server data");
      }
    };

    ws.onerror = (event) => {
      console.error("WebSocket error:", event);
      setError("WebSocket connection error");
      setWsConnected(false);
      isConnectingRef.current = false;
    };

    ws.onclose = (event) => {
      console.log("WebSocket closed:", event.code, event.reason);
      setWsConnected(false);
      wsRef.current = null;
      isConnectingRef.current = false;

      // Attempt to reconnect with exponential backoff
      reconnectAttemptsRef.current += 1;
      const delay = Math.min(
        1000 * Math.pow(2, reconnectAttemptsRef.current),
        30000,
      );

      console.log(
        `Reconnecting in ${delay}ms (attempt ${reconnectAttemptsRef.current})`,
      );

      reconnectTimeoutRef.current = setTimeout(() => {
        connectWebSocket();
      }, delay);
    };
  }, []);

  const connectDemoMode = useCallback(() => {
    console.log("Demo mode: Using mock data");

    // Simulate initial connection delay
    const connectionTimeout = setTimeout(() => {
      setLoading(false);
      setWsConnected(true);
    }, 500);

    // Create mock WebSocket connection with updates every 10 seconds
    mockWsRef.current = createMockWebSocket((data) => {
      setHealth(data.health);
      setStatus(data.status);
    }, 10000);

    return () => {
      clearTimeout(connectionTimeout);
      if (mockWsRef.current) {
        mockWsRef.current.close();
      }
    };
  }, []);

  useEffect(() => {
    if (isDemoMode) {
      return connectDemoMode();
    } else {
      connectWebSocket();

      // Cleanup on unmount
      return () => {
        if (reconnectTimeoutRef.current) {
          clearTimeout(reconnectTimeoutRef.current);
        }
        if (wsRef.current) {
          wsRef.current.close();
        }
      };
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const triggerDiscovery = useCallback(async () => {
    if (isDemoMode) {
      console.log("Demo mode: Simulating miner discovery trigger");
      setDiscoveryPending(true);
      await new Promise((resolve) => setTimeout(resolve, 1500));
      setDiscoveryPending(false);
      return;
    }

    setDiscoveryPending(true);
    try {
      const response = await fetch("/api/miners/discover", { method: "POST" });
      if (!response.ok) {
        console.error("Failed to trigger miner discovery:", response.statusText);
      }
    } catch (err) {
      console.error("Error triggering miner discovery:", err);
    } finally {
      setDiscoveryPending(false);
    }
  }, []);

  return {
    health,
    status,
    loading,
    error,
    wsConnected,
    triggerDiscovery,
  };
}