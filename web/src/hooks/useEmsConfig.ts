import { useState, useEffect, useCallback } from "react";
import type { EmsConfig } from "../types/api";

export interface UseEmsConfigResult {
  config: EmsConfig | null;
  loading: boolean;
  error: string | null;
  saving: boolean;
  saveError: string | null;
  saveSuccess: boolean;
  fetchConfig: () => Promise<void>;
  saveConfig: (updated: EmsConfig) => Promise<void>;
}

export function useEmsConfig(): UseEmsConfigResult {
  const [config, setConfig] = useState<EmsConfig | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [saveSuccess, setSaveSuccess] = useState(false);

  const fetchConfig = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const res = await fetch("/api/config");
      if (!res.ok) {
        throw new Error(`Server returned ${res.status}: ${res.statusText}`);
      }
      const data: EmsConfig = await res.json();
      setConfig(data);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load configuration");
    } finally {
      setLoading(false);
    }
  }, []);

  const saveConfig = useCallback(async (updated: EmsConfig) => {
    setSaving(true);
    setSaveError(null);
    setSaveSuccess(false);
    try {
      const res = await fetch("/api/config", {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(updated),
      });
      const body = await res.json();
      if (!res.ok) {
        throw new Error(body?.error ?? `Server returned ${res.status}`);
      }
      setConfig(body as EmsConfig);
      setSaveSuccess(true);
      setTimeout(() => setSaveSuccess(false), 3000);
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : "Failed to save configuration");
    } finally {
      setSaving(false);
    }
  }, []);

  // Auto-fetch on mount
  useEffect(() => {
    fetchConfig();
  }, [fetchConfig]);

  return { config, loading, error, saving, saveError, saveSuccess, fetchConfig, saveConfig };
}
