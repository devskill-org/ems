import { useState, useEffect } from "react";

export interface AppConfig {
  miner_names: Record<string, string>;
  /** Hide all miner-related UI elements. Enabled by default. */
  hide_miners: boolean;
}

const defaultConfig: AppConfig = {
  miner_names: {},
  hide_miners: true,
};

let cachedConfig: AppConfig | null = null;

export function useConfig(): AppConfig {
  const [config, setConfig] = useState<AppConfig>(cachedConfig ?? defaultConfig);

  useEffect(() => {
    if (cachedConfig) {
      setConfig(cachedConfig);
      return;
    }

    fetch("/config.json")
      .then((res) => {
        if (!res.ok) throw new Error(`Failed to load config: ${res.status}`);
        return res.json();
      })
      .then((data: Partial<AppConfig>) => {
        const merged: AppConfig = { ...defaultConfig, ...data };
        cachedConfig = merged;
        setConfig(merged);
      })
      .catch((err) => {
        console.warn("Could not load config.json, using defaults:", err);
      });
  }, []);

  return config;
}