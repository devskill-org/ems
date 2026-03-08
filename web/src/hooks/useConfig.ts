import { useState, useEffect } from "react";

export interface AppConfig {
  miner_names: Record<string, string>;
}

const defaultConfig: AppConfig = {
  miner_names: {},
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
      .then((data: AppConfig) => {
        cachedConfig = data;
        setConfig(data);
      })
      .catch((err) => {
        console.warn("Could not load config.json, using defaults:", err);
      });
  }, []);

  return config;
}