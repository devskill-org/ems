import { useState, useEffect, useCallback } from "react";
import type { EmsConfig } from "../types/api";
import { useEmsConfig } from "../hooks/useEmsConfig";
import "./ConfigMenu.css";

interface ConfigMenuProps {
  onClose: () => void;
}

type FieldType = "text" | "number" | "boolean" | "select" | "duration" | "numericSelect";

interface FieldDef {
  key: keyof EmsConfig;
  label: string;
  type: FieldType;
  unit?: string;
  hint?: string;
  min?: number;
  max?: number;
  step?: number;
  options?: string[];
  numericOptions?: { value: number; label: string }[];
  sensitive?: boolean;
}

interface Section {
  title: string;
  fields: FieldDef[];
}

const SECTIONS: Section[] = [
  {
    title: "Scheduler",
    fields: [
      {
        key: "price_limit",
        label: "Price Limit",
        type: "number",
        unit: "EUR/MWh",
        hint: "Mining stops above this price",
        step: 0.1,
      },
      {
        key: "network",
        label: "Network",
        type: "text",
        hint: "CIDR notation, e.g. 192.168.1.0/24",
      },
      {
        key: "check_price_interval",
        label: "Check Price Interval",
        type: "duration",
        hint: "How often to evaluate prices (e.g. 15m)",
      },
      {
        key: "miners_state_check_interval",
        label: "Miners State Check Interval",
        type: "duration",
        hint: "How often to poll miner state",
      },
      {
        key: "miner_discovery_interval",
        label: "Miner Discovery Interval",
        type: "duration",
        hint: "How often to scan for new miners",
      },
      {
        key: "miner_max_consecutive_errors",
        label: "Max Consecutive Errors",
        type: "number",
        min: 1,
        hint: "Evict miner after this many errors in a row",
      },
      {
        key: "dry_run",
        label: "Dry Run",
        type: "boolean",
        hint: "Simulate actions without executing them",
      },
    ],
  },
  {
    title: "API",
    fields: [
      {
        key: "security_token",
        label: "ENTSO-E Security Token",
        type: "text",
        sensitive: true,
        hint: "Your ENTSO-E API security token",
      },
      {
        key: "api_timeout",
        label: "API Timeout",
        type: "duration",
        hint: "Timeout for ENTSO-E API calls (e.g. 30s)",
      },
      {
        key: "url_format",
        label: "URL Format",
        type: "text",
        hint: "ENTSO-E API URL template",
      },
    ],
  },
  {
    title: "Logging",
    fields: [
      {
        key: "log_level",
        label: "Log Level",
        type: "select",
        options: ["debug", "info", "warn", "error"],
      },
      {
        key: "log_format",
        label: "Log Format",
        type: "select",
        options: ["text", "json"],
      },
    ],
  },
  {
    title: "Timezone & Connectivity",
    fields: [
      {
        key: "location",
        label: "Timezone Location",
        type: "text",
        hint: "e.g. CET, Europe/Riga",
      },
      {
        key: "miner_timeout",
        label: "Miner Timeout",
        type: "duration",
        hint: "Timeout for miner operations (e.g. 5s)",
      },
      {
        key: "health_check_port",
        label: "Health Check Port",
        type: "number",
        min: 0,
        max: 65535,
        hint: "0 = disabled",
      },
    ],
  },
  {
    title: "Miner Power Modes",
    fields: [
      {
        key: "fanr_high_threshold",
        label: "FanR High Threshold",
        type: "number",
        min: 0,
        max: 100,
        unit: "%",
        hint: "Decrease work mode above this FanR",
      },
      {
        key: "fanr_low_threshold",
        label: "FanR Low Threshold",
        type: "number",
        min: 0,
        max: 100,
        unit: "%",
        hint: "Increase work mode below this FanR",
      },
      {
        key: "miner_work_mode_upgrade_checks",
        label: "Work Mode Upgrade Checks",
        type: "number",
        min: 1,
        hint: "Consecutive checks at current work mode required before upgrading",
      },
      {
        key: "miner_max_work_mode",
        label: "Max Work Mode",
        type: "numericSelect",
        numericOptions: [
          { value: 0, label: "Eco" },
          { value: 1, label: "Standard" },
          { value: 2, label: "Super" },
        ],
        hint: "Highest work mode miners are allowed to reach",
      },
      {
        key: "miners_power_limit",
        label: "Total Power Limit",
        type: "number",
        min: 0,
        step: 0.1,
        unit: "kW",
        hint: "Maximum combined power for all miners",
      },
      {
        key: "miner_power_standby",
        label: "Standby Power",
        type: "number",
        min: 0,
        step: 0.01,
        unit: "kW",
      },
      {
        key: "miner_power_eco",
        label: "Eco Power",
        type: "number",
        min: 0,
        step: 0.01,
        unit: "kW",
      },
      {
        key: "miner_power_standard",
        label: "Standard Power",
        type: "number",
        min: 0,
        step: 0.01,
        unit: "kW",
      },
      {
        key: "miner_power_super",
        label: "Super Power",
        type: "number",
        min: 0,
        step: 0.01,
        unit: "kW",
      },
      {
        key: "pv_power_control_price_limit",
        label: "PV Power Control Price Limit",
        type: "number",
        min: 0,
        step: 1,
        unit: "EUR/MWh",
        hint: "Enable PV-based miner control when market price is at or above this threshold",
      },
    ],
  },
  {
    title: "PV / Modbus",
    fields: [
      {
        key: "plant_modbus_address",
        label: "Plant Modbus Address",
        type: "text",
        hint: "IP:PORT, e.g. 192.168.1.100:502",
      },
      {
        key: "device_id",
        label: "Device ID",
        type: "number",
        min: 0,
        hint: "Device ID for metrics table",
      },
      {
        key: "pv_poll_interval",
        label: "PV Poll Interval",
        type: "duration",
        hint: "How often to sample PV power (e.g. 10s)",
      },
      {
        key: "pv_integration_period",
        label: "PV Integration Period",
        type: "duration",
        hint: "Integration window for PV energy (e.g. 15m)",
      },
      {
        key: "postgres_conn_string",
        label: "PostgreSQL Connection String",
        type: "text",
        sensitive: true,
        hint: "Leave blank to disable DB integration",
      },
    ],
  },
  {
    title: "Weather",
    fields: [
      {
        key: "weather_update_interval",
        label: "Weather Update Interval",
        type: "duration",
        hint: "How often to refresh weather data (e.g. 1h)",
      },
      {
        key: "latitude",
        label: "Latitude",
        type: "number",
        min: -90,
        max: 90,
        step: 0.0001,
        hint: "Decimal degrees",
      },
      {
        key: "longitude",
        label: "Longitude",
        type: "number",
        min: -180,
        max: 180,
        step: 0.0001,
        hint: "Decimal degrees",
      },
      {
        key: "user_agent",
        label: "User Agent",
        type: "text",
        hint: "Sent to the weather API (e.g. MyApp/1.0 (user@example.com))",
      },
    ],
  },
  {
    title: "Battery / MPC",
    fields: [
      {
        key: "battery_capacity",
        label: "Battery Capacity",
        type: "number",
        min: 0,
        step: 0.1,
        unit: "kWh",
      },
      {
        key: "battery_max_charge",
        label: "Max Charge Rate",
        type: "number",
        min: 0,
        step: 0.1,
        unit: "kW",
      },
      {
        key: "battery_max_discharge",
        label: "Max Discharge Rate",
        type: "number",
        min: 0,
        step: 0.1,
        unit: "kW",
      },
      {
        key: "battery_min_soc",
        label: "Min State of Charge",
        type: "number",
        min: 0,
        max: 1,
        step: 0.01,
        hint: "0–1 (e.g. 0.1 = 10%)",
      },
      {
        key: "battery_max_soc",
        label: "Max State of Charge",
        type: "number",
        min: 0,
        max: 1,
        step: 0.01,
        hint: "0–1 (e.g. 0.9 = 90%)",
      },
      {
        key: "battery_efficiency",
        label: "Round-trip Efficiency",
        type: "number",
        min: 0,
        max: 1,
        step: 0.01,
        hint: "0–1 (e.g. 0.92 = 92%)",
      },
      {
        key: "battery_degradation_cost",
        label: "Degradation Cost",
        type: "number",
        min: 0,
        step: 0.001,
        unit: "$/kWh cycled",
      },
      {
        key: "max_grid_import",
        label: "Max Grid Import",
        type: "number",
        min: 0,
        step: 0.1,
        unit: "kW",
      },
      {
        key: "max_grid_export",
        label: "Max Grid Export",
        type: "number",
        min: 0,
        step: 0.1,
        unit: "kW",
      },
      {
        key: "max_solar_power",
        label: "Peak Solar Capacity",
        type: "number",
        min: 0,
        step: 0.1,
        unit: "kW",
      },
      {
        key: "mpc_execution_interval",
        label: "MPC Execution Interval",
        type: "duration",
        hint: "How often to re-execute the current MPC decision",
      },
      {
        key: "battery_balancing_soc_threshold",
        label: "Balancing SOC Threshold",
        type: "number",
        min: 0,
        max: 1,
        step: 0.001,
        hint: "SOC level (0–1) above which CV-phase balancing begins; 0 disables",
      },
      {
        key: "battery_balancing_efficiency_factor",
        label: "Balancing Efficiency Factor",
        type: "number",
        min: 0,
        max: 1,
        step: 0.01,
        hint: "Multiplier on efficiency during CV phase (e.g. 0.1 = ~10× more input energy); 0 disables",
      },
      {
        key: "battery_balancing_bonus",
        label: "Balancing Bonus",
        type: "number",
        min: 0,
        step: 0.01,
        unit: "$",
        hint: "One-time profit bonus awarded when battery first reaches Max SOC within the horizon; 0 disables",
      },
    ],
  },
  {
    title: "Battery Thermal",
    fields: [
      {
        key: "battery_preheat_power",
        label: "Pre-heat Power",
        type: "number",
        min: 0,
        step: 0.01,
        unit: "kW",
        hint: "Power used for battery preheating",
      },
      {
        key: "battery_preheat_temp_threshold",
        label: "Pre-heat Temp Threshold",
        type: "number",
        min: -50,
        max: 100,
        step: 0.1,
        unit: "°C",
        hint: "Activate preheating below this temperature",
      },
      {
        key: "battery_thermal_time_constant",
        label: "Thermal Time Constant",
        type: "number",
        min: 0,
        max: 1,
        step: 0.001,
        hint: "Fraction per hour toward air temp (0–1)",
      },
    ],
  },
  {
    title: "Price Adjustments",
    fields: [
      {
        key: "import_price_operator_fee",
        label: "Import Operator Fee",
        type: "number",
        min: 0,
        step: 0.1,
        unit: "EUR/MWh",
      },
      {
        key: "import_price_delivery_fee",
        label: "Import Delivery Fee",
        type: "number",
        min: 0,
        step: 0.1,
        unit: "EUR/MWh",
      },
      {
        key: "export_price_operator_fee",
        label: "Export Operator Fee",
        type: "number",
        min: 0,
        step: 0.1,
        unit: "EUR/MWh",
        hint: "Subtracted from export price",
      },
    ],
  },
];

// ─── helpers ────────────────────────────────────────────────────────────────

function getNestedValue(obj: EmsConfig, key: keyof EmsConfig): unknown {
  return obj[key];
}

function formatDisplayValue(value: unknown, type: FieldType): string {
  if (value === null || value === undefined) return "";
  if (type === "boolean") return String(value);
  return String(value);
}

// ─── Field renderer ─────────────────────────────────────────────────────────

interface FieldProps {
  def: FieldDef;
  value: unknown;
  onChange: (key: keyof EmsConfig, value: unknown) => void;
}

function ConfigField({ def, value, onChange }: FieldProps) {
  const [showSensitive, setShowSensitive] = useState(false);

  const handleChange = useCallback(
    (raw: string | boolean) => {
      if (def.type === "boolean") {
        onChange(def.key, raw as boolean);
        return;
      }
      if (def.type === "number" || def.type === "numericSelect") {
        const parsed = raw === "" ? 0 : Number(raw);
        onChange(def.key, isNaN(parsed) ? 0 : parsed);
        return;
      }
      onChange(def.key, raw);
    },
    [def.key, def.type, onChange],
  );

  const displayValue = formatDisplayValue(value, def.type);

  return (
    <div className="config-field">
      <label className="config-field-label">
        {def.label}
        {def.unit && <span className="config-field-unit"> ({def.unit})</span>}
      </label>

      {def.type === "boolean" ? (
        <div className="config-field-toggle">
          <button
            type="button"
            className={`toggle-btn ${value ? "toggle-on" : "toggle-off"}`}
            onClick={() => handleChange(!value as boolean)}
            aria-label={`Toggle ${def.label}`}
          >
            <span className="toggle-thumb" />
          </button>
          <span className="toggle-label">{value ? "Enabled" : "Disabled"}</span>
        </div>
      ) : def.type === "select" ? (
        <select
          className="config-input"
          value={displayValue}
          onChange={(e) => handleChange(e.target.value)}
        >
          {def.options?.map((opt) => (
            <option key={opt} value={opt}>
              {opt}
            </option>
          ))}
        </select>
      ) : def.type === "numericSelect" ? (
        <select
          className="config-input"
          value={String(value ?? 0)}
          onChange={(e) => handleChange(e.target.value)}
        >
          {def.numericOptions?.map((opt) => (
            <option key={opt.value} value={String(opt.value)}>
              {opt.label}
            </option>
          ))}
        </select>
      ) : def.type === "number" ? (
        <input
          type="number"
          className="config-input"
          value={displayValue}
          min={def.min}
          max={def.max}
          step={def.step ?? 1}
          onChange={(e) => handleChange(e.target.value)}
        />
      ) : (
        <div className="config-input-wrapper">
          <input
            type={def.sensitive && !showSensitive ? "password" : "text"}
            className="config-input"
            value={displayValue}
            onChange={(e) => handleChange(e.target.value)}
            autoComplete="off"
            spellCheck={false}
          />
          {def.sensitive && (
            <button
              type="button"
              className="sensitive-toggle"
              onClick={() => setShowSensitive((s) => !s)}
              title={showSensitive ? "Hide" : "Show"}
            >
              {showSensitive ? "🙈" : "👁"}
            </button>
          )}
        </div>
      )}

      {def.hint && <p className="config-field-hint">{def.hint}</p>}
    </div>
  );
}

// ─── Main component ─────────────────────────────────────────────────────────

export function ConfigMenu({ onClose }: ConfigMenuProps) {
  const {
    config,
    loading,
    error,
    saving,
    saveError,
    saveSuccess,
    saveConfig,
    fetchConfig,
  } = useEmsConfig();

  const [draft, setDraft] = useState<EmsConfig | null>(null);
  const [activeSection, setActiveSection] = useState<string>(SECTIONS[0].title);
  const [dirtyKeys, setDirtyKeys] = useState<Set<keyof EmsConfig>>(new Set());

  // Populate draft once config is loaded
  useEffect(() => {
    if (config && !draft) {
      setDraft({ ...config });
    }
  }, [config, draft]);

  const handleFieldChange = useCallback(
    (key: keyof EmsConfig, value: unknown) => {
      setDraft((prev) => (prev ? { ...prev, [key]: value } : prev));
      setDirtyKeys((prev) => new Set(prev).add(key));
    },
    [],
  );

  const handleReset = useCallback(() => {
    if (config) {
      setDraft({ ...config });
      setDirtyKeys(new Set());
    }
  }, [config]);

  const handleSave = useCallback(async () => {
    if (!draft) return;
    await saveConfig(draft);
    setDirtyKeys(new Set());
  }, [draft, saveConfig]);

  const handleRefresh = useCallback(() => {
    setDraft(null);
    setDirtyKeys(new Set());
    fetchConfig();
  }, [fetchConfig]);

  const isDirty = dirtyKeys.size > 0;

  // Close on Escape
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    window.addEventListener("keydown", handler);
    return () => window.removeEventListener("keydown", handler);
  }, [onClose]);

  const currentSection =
    SECTIONS.find((s) => s.title === activeSection) ?? SECTIONS[0];

  return (
    <div
      className="config-overlay"
      role="dialog"
      aria-modal="true"
      aria-label="Configuration"
    >
      {/* Backdrop */}
      <div className="config-backdrop" onClick={onClose} />

      <div className="config-modal">
        {/* Header */}
        <div className="config-modal-header">
          <h2>⚙ Configuration</h2>
          <div className="config-header-actions">
            <button
              type="button"
              className="config-btn config-btn-ghost"
              onClick={handleRefresh}
              disabled={loading || saving}
              title="Reload config from server"
            >
              🔄 Reload
            </button>
            <button
              type="button"
              className="config-close-btn"
              onClick={onClose}
              aria-label="Close configuration"
            >
              ✕
            </button>
          </div>
        </div>

        {/* Status bar */}
        {(error || saveError || saveSuccess) && (
          <div
            className={`config-status-bar ${
              saveSuccess ? "config-status-success" : "config-status-error"
            }`}
          >
            {saveSuccess && "✓ Configuration saved successfully."}
            {saveError && `✗ ${saveError}`}
            {error && `✗ ${error}`}
          </div>
        )}

        {/* Body */}
        <div className="config-modal-body">
          {/* Sidebar navigation */}
          <nav className="config-nav">
            {SECTIONS.map((section) => (
              <button
                key={section.title}
                type="button"
                className={`config-nav-item ${activeSection === section.title ? "config-nav-item-active" : ""}`}
                onClick={() => setActiveSection(section.title)}
              >
                {section.title}
              </button>
            ))}
          </nav>

          {/* Fields panel */}
          <div className="config-panel">
            {loading && !draft ? (
              <div className="config-loading">Loading configuration…</div>
            ) : !draft ? (
              <div className="config-loading config-error-text">
                {error ?? "Could not load configuration."}
              </div>
            ) : (
              <>
                <h3 className="config-section-title">{currentSection.title}</h3>
                <div className="config-fields-grid">
                  {currentSection.fields.map((field) => (
                    <ConfigField
                      key={field.key}
                      def={field}
                      value={getNestedValue(draft, field.key)}
                      onChange={handleFieldChange}
                    />
                  ))}
                </div>
              </>
            )}
          </div>
        </div>

        {/* Footer */}
        <div className="config-modal-footer">
          <span className="config-dirty-indicator">
            {isDirty &&
              `${dirtyKeys.size} unsaved change${dirtyKeys.size !== 1 ? "s" : ""}`}
          </span>
          <div className="config-footer-actions">
            <button
              type="button"
              className="config-btn config-btn-ghost"
              onClick={handleReset}
              disabled={!isDirty || saving}
            >
              Reset
            </button>
            <button
              type="button"
              className="config-btn config-btn-primary"
              onClick={handleSave}
              disabled={!isDirty || saving || !draft}
            >
              {saving ? "Saving…" : "Save Changes"}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
