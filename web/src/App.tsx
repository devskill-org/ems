import "./App.css";
import emsIcon from "../assets/icon-32.png";
import { InfoItem } from "./components/InfoItem";
import { StatusBadge } from "./components/StatusBadge";
import { PowerDisplay } from "./components/PowerDisplay";
import { SolarInfo } from "./components/SolarInfo";
import { MPCDecisions } from "./components/MPCDecisions";
import { MetricsSummary } from "./components/MetricsSummary";
import { DemoInfo } from "./components/DemoInfo";
import { ConfigMenu } from "./components/ConfigMenu";
import { useWebSocket } from "./hooks/useWebSocket";
import { useConfig } from "./hooks/useConfig";
import { useState, useEffect, useCallback } from "react";

// Check if we're in demo mode
const isDemoMode = typeof __DEMO_MODE__ !== "undefined" && __DEMO_MODE__;

function App() {
  const { health, status, loading, error, wsConnected, triggerDiscovery } =
    useWebSocket();
  const config = useConfig();
  const [showDemoInfo, setShowDemoInfo] = useState(false);
  const [showConfig, setShowConfig] = useState(false);
  const [discoveryLoading, setDiscoveryLoading] = useState(false);
  const [discoveryMessage, setDiscoveryMessage] = useState<string | null>(null);

  const handleDiscovery = useCallback(async () => {
    setDiscoveryLoading(true);
    setDiscoveryMessage(null);
    try {
      await triggerDiscovery();
      setDiscoveryMessage("Discovery started");
    } catch {
      setDiscoveryMessage("Failed to trigger discovery");
    } finally {
      setDiscoveryLoading(false);
      setTimeout(() => setDiscoveryMessage(null), 4000);
    }
  }, [triggerDiscovery]);

  // Show demo info automatically on first load in demo mode
  useEffect(() => {
    if (isDemoMode) {
      const hasSeenDemo = localStorage.getItem("ems-demo-info-seen");
      if (!hasSeenDemo) {
        setShowDemoInfo(true);
        localStorage.setItem("ems-demo-info-seen", "true");
      }
    }
  }, []);

  if (loading) {
    return (
      <div className="app">
        <div className="loading">Connecting to server...</div>
      </div>
    );
  }

  if (error && !wsConnected) {
    return (
      <div className="app">
        <div className="error">
          <p>Error: {error}</p>
          <p>Attempting to reconnect...</p>
        </div>
      </div>
    );
  }

  const isHealthy = health?.status === "healthy";
  const currentPrice = status?.price_data?.current_avg_price;
  const priceLimit = status?.price_data?.limit;

  return (
    <div className="app">
      <header className="header">
        <h1>
          <img
            src={emsIcon}
            alt="EMS"
            style={{
              height: "32px",
              marginRight: "12px",
              verticalAlign: "middle",
            }}
          />
          Energy Management System
        </h1>
        <div className="status-badges">
          {isDemoMode && (
            <StatusBadge
              isActive={true}
              activeLabel="🎭 Demo Mode"
              inactiveLabel=""
            />
          )}
          {isDemoMode && (
            <button
              className="demo-info-trigger"
              onClick={() => setShowDemoInfo(true)}
              title="Learn about Demo Mode"
            >
              ℹ️
            </button>
          )}
          <StatusBadge
            isActive={isHealthy}
            activeLabel="✓ Healthy"
            inactiveLabel="✗ Unhealthy"
          />
          <StatusBadge
            isActive={wsConnected}
            activeLabel="🔗 Connected"
            inactiveLabel="⚠️ Disconnected"
          />
          <button
            className="demo-info-trigger"
            onClick={() => setShowConfig(true)}
            title="Open configuration"
            aria-label="Open configuration"
          >
            ⚙️
          </button>
        </div>
      </header>

      <main className="main">
        <section className="card">
          <h2>Scheduler Status</h2>
          <div className="info-grid">
            <InfoItem
              label="Running:"
              value={health?.scheduler.is_running ? "Yes" : "No"}
              valueClassName={
                health?.scheduler.is_running ? "value-success" : "value-error"
              }
            />
            <InfoItem label="Network:" value={health?.scheduler.network} />
            <InfoItem label="Miners Count:" value={status?.miners.count || 0} />
            <InfoItem
              label="Market Data:"
              value={
                health?.scheduler.has_market_data
                  ? "Available"
                  : "Not Available"
              }
              valueClassName={
                health?.scheduler.has_market_data
                  ? "value-success"
                  : "value-warning"
              }
            />
          </div>
        </section>

        {currentPrice !== undefined && priceLimit !== undefined && (
          <section className="card">
            <h2>Price Information</h2>
            <div className="info-grid">
              <InfoItem
                label="Current Avg Price:"
                value={`${currentPrice.toFixed(2)} €/MWh`}
              />
              <InfoItem
                label="Price Limit:"
                value={`${priceLimit.toFixed(2)} €/MWh`}
              />
            </div>
          </section>
        )}

        <section className="card">
          <h2>
            Discovered Miners
            <button
              className="refresh-button"
              onClick={handleDiscovery}
              disabled={discoveryLoading}
              title="Trigger miners discovery"
              style={{ marginLeft: "12px" }}
            >
              {discoveryLoading ? "⏳ Discovering…" : "🔍 Refresh"}
            </button>
            {discoveryMessage && (
              <span className="discovery-message">{discoveryMessage}</span>
            )}
          </h2>
          {status?.miners.list && status.miners.list.length > 0 ? (
            <div className="miners-list">
              {status.miners.list.map((miner, index) => (
                <div key={miner.dna ?? index} className="miner-item">
                  {(miner.filter_usage !== undefined ||
                    miner.fan_r !== undefined) && (
                    <div className="miner-badges">
                      {miner.filter_usage !== undefined && (
                        <div
                          className={`miner-filter-usage ${
                            miner.filter_usage >= 75
                              ? "filter-usage-high"
                              : "filter-usage-low"
                          }`}
                          title="Filter usage: cumulative filter runtime as % of 120 000 s cleaning interval"
                        >
                          Filter: {miner.filter_usage}%
                        </div>
                      )}
                      {miner.fan_r !== undefined && (
                        <div
                          className={`miner-filter-usage ${
                            miner.fan_r >= 75
                              ? "filter-usage-high"
                              : "filter-usage-low"
                          }`}
                          title="Fan speed percentage"
                        >
                          FanR: {miner.fan_r}%
                        </div>
                      )}
                    </div>
                  )}
                  <div
                    className="miner-ip miner-ip-link"
                    onClick={() =>
                      window.open(
                        `http://${miner.ip}`,
                        "_blank",
                        "noopener,noreferrer",
                      )
                    }
                    title={`Open http://${miner.ip}`}
                    style={{ cursor: "pointer" }}
                  >
                    {(miner.dna && config.miner_names[miner.dna]) ??
                      miner.dna ??
                      miner.ip}
                  </div>
                  <div
                    className={`miner-status status-${miner.status?.toLowerCase()}`}
                  >
                    {miner.status || "Unknown"}
                  </div>
                </div>
              ))}
            </div>
          ) : (
            <p className="no-miners">No miners discovered yet.</p>
          )}
        </section>

        <section className="card devices-section">
          <h2>Devices</h2>
          <div className="devices-content" style={{ position: "relative" }}>
            <div
              className="power-display-wrapper"
              data-mobile-label="Solar Power"
            >
              <PowerDisplay
                value={health?.ems?.current_pv_power}
                invertColors={true}
                style={{ position: "absolute", top: "202px", right: "170px" }}
              />
            </div>

            <div
              className="power-display-wrapper"
              data-mobile-label={
                health?.ems?.ess_soc !== undefined
                  ? `Battery (${health.ems.ess_soc.toFixed(1)}%)`
                  : "Battery"
              }
            >
              <PowerDisplay
                value={health?.ems?.ess_power}
                label={
                  health?.ems?.ess_soc !== undefined
                    ? `${health.ems.ess_soc.toFixed(1)}%`
                    : "N/A"
                }
                style={{ position: "absolute", top: "643px", right: "622px" }}
              />
            </div>

            <div
              className="power-display-wrapper"
              data-mobile-label="Grid Power"
            >
              <PowerDisplay
                value={
                  health?.ems?.grid_sensor_status === 1
                    ? health?.ems?.grid_sensor_active_power
                    : undefined
                }
                style={{ position: "absolute", top: "556px", left: "220px" }}
              />
            </div>

            <div
              className="power-display-wrapper"
              data-mobile-label="Load Power"
            >
              <PowerDisplay
                value={
                  health?.ems !== undefined
                    ? health.ems.current_pv_power +
                      health.ems.grid_sensor_active_power -
                      health.ems.ess_power
                    : undefined
                }
                label="Load Power"
                invertColors={false}
                showLabel={true}
                style={{ position: "absolute", top: "194px", left: "223px" }}
              />
            </div>

            <div
              className="power-display-wrapper"
              data-mobile-label={
                health?.ems?.dc_charger_vehicle_soc !== undefined
                  ? `EV Charger (${health.ems.dc_charger_vehicle_soc.toFixed(1)}%)`
                  : "EV Charger"
              }
            >
              <PowerDisplay
                value={health?.ems?.dc_charger_output_power}
                label={
                  health?.ems?.dc_charger_vehicle_soc !== undefined
                    ? `${health.ems.dc_charger_vehicle_soc.toFixed(1)}%`
                    : "N/A"
                }
                style={{ position: "absolute", top: "428px", right: "162px" }}
              />
            </div>

            <SolarInfo
              solarAngle={health?.sun?.solar_angle}
              sunrise={health?.sun?.sunrise}
              sunset={health?.sun?.sunset}
              style={{
                position: "absolute",
                top: "-75px",
                left: "100px",
                width: "175px",
              }}
            />
          </div>
        </section>

        <MPCDecisions decisions={health?.scheduler.mpc_decisions} />

        <MetricsSummary />

        <section className="card system-info">
          <h2>System Information</h2>
          <div className="info-grid">
            <InfoItem label="Version:" value={health?.version} />
            <InfoItem label="Uptime:" value={health?.system.uptime} />
            <InfoItem
              label="Last Updated:"
              value={
                status?.timestamp
                  ? new Date(status.timestamp).toLocaleString()
                  : "N/A"
              }
            />
          </div>
        </section>
      </main>

      <footer className="footer">
        <p>
          Avalon miners scheduler based on electricity prices and plant
          available power
        </p>
      </footer>

      {isDemoMode && showDemoInfo && (
        <DemoInfo onClose={() => setShowDemoInfo(false)} />
      )}

      {showConfig && <ConfigMenu onClose={() => setShowConfig(false)} />}
    </div>
  );
}

export default App;
