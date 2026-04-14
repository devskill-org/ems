import { useCallback, useEffect, useRef, useState } from "react";
import uPlot from "uplot";
import "uplot/dist/uPlot.min.css";
import { MPCDecisionInfo } from "../types/api";
import "../App.css";

// Helper function to format timestamp
const formatTimestamp = (timestamp: number): string => {
  // Check if timestamp is valid (not 0, undefined, or null)
  if (!timestamp || timestamp === 0) {
    return "N/A";
  }

  const date = new Date(timestamp * 1000);

  // Check if date is valid
  if (isNaN(date.getTime())) {
    return "Invalid Date";
  }

  const month = date.getMonth() + 1;
  const day = date.getDate();
  const hour = date.getHours().toString().padStart(2, "0");
  const minute = date.getMinutes().toString().padStart(2, "0");

  return `${month}/${day} ${hour}:${minute}`;
};

// Helper function to convert weather symbol to emoji
const getWeatherIcon = (symbol: string): string => {
  if (!symbol) return "❓";

  const s = symbol.toLowerCase();

  // Thunder conditions
  if (s.includes("thunder")) {
    if (s.includes("snow")) return "⛈️❄️";
    if (s.includes("sleet")) return "⛈️🌧️";
    return "⛈️";
  }

  // Snow conditions
  if (s.includes("snow")) {
    if (s.includes("heavy")) return "❄️❄️";
    if (s.includes("light")) return "🌨️";
    return "❄️";
  }

  // Sleet conditions
  if (s.includes("sleet")) {
    if (s.includes("heavy")) return "🌧️❄️";
    return "🌨️";
  }

  // Rain conditions
  if (s.includes("rain")) {
    if (s.includes("heavy")) return "🌧️🌧️";
    if (s.includes("light")) return "🌦️";
    return "🌧️";
  }

  // Fog
  if (s.includes("fog")) return "🌫️";

  // Cloud conditions
  if (s.includes("cloudy")) return "☁️";
  if (s.includes("partlycloudy")) {
    if (s.includes("_night")) return "☁️🌙";
    return "⛅";
  }

  // Clear/Fair conditions
  if (s.includes("clearsky") || s.includes("fair")) {
    if (s.includes("_night") || s.includes("polartwilight")) return "🌙";
    return "☀️";
  }

  return "❓";
};

interface MPCDecisionsProps {
  decisions?: MPCDecisionInfo[];
}

export function MPCDecisions({ decisions }: MPCDecisionsProps) {
  const chartInstanceRef = useRef<uPlot | null>(null);
  const tooltipRef = useRef<HTMLDivElement>(null);
  const [showTable, setShowTable] = useState(false);
  const [chartReady, setChartReady] = useState(false);
  const tableContainerRef = useRef<HTMLDivElement>(null);
  const [canScrollLeft, setCanScrollLeft] = useState(false);
  const [canScrollRight, setCanScrollRight] = useState(false);

  // Store decisions and derived data in refs so chart callbacks can access them
  const decisionsRef = useRef<MPCDecisionInfo[]>([]);
  const highsRef = useRef<number[]>([]);

  // Check scroll position
  const checkScroll = useCallback(() => {
    const container = tableContainerRef.current;
    if (container) {
      setCanScrollLeft(container.scrollLeft > 0);
      setCanScrollRight(
        container.scrollLeft <
          container.scrollWidth - container.clientWidth - 1,
      );
    }
  }, []);

  // Scroll table
  const scrollTable = useCallback((direction: "left" | "right") => {
    const container = tableContainerRef.current;
    if (container) {
      const scrollAmount = container.clientWidth * 0.8; // Scroll 80% of visible width
      container.scrollBy({
        left: direction === "left" ? -scrollAmount : scrollAmount,
        behavior: "smooth",
      });
    }
  }, []);

  // Check scroll on mount and when table is shown
  useEffect(() => {
    if (showTable) {
      checkScroll();
      const container = tableContainerRef.current;
      if (container) {
        container.addEventListener("scroll", checkScroll);
        window.addEventListener("resize", checkScroll);
        return () => {
          container.removeEventListener("scroll", checkScroll);
          window.removeEventListener("resize", checkScroll);
        };
      }
    }
  }, [showTable, checkScroll]);

  // Helper function to determine battery action
  const getBatteryAction = useCallback((decision: MPCDecisionInfo) => {
    if (decision.battery_charge > 0.1) {
      return { action: "charge", power: decision.battery_charge };
    } else if (decision.battery_discharge > 0.1) {
      return { action: "discharge", power: decision.battery_discharge };
    }
    return { action: "idle", power: 0 };
  }, []);

  // Helper function to determine grid action
  const getGridAction = useCallback((decision: MPCDecisionInfo) => {
    if (decision.grid_import > 0.1) {
      return { action: "import", power: decision.grid_import };
    } else if ((decision.battery_charge_from_grid ?? 0) > 0.1) {
      // Grid is used to charge the battery even when net grid_import rounds to ~0
      return { action: "import", power: decision.battery_charge_from_grid };
    } else if (decision.grid_export > 0.1) {
      return { action: "export", power: decision.grid_export };
    }
    return { action: "idle", power: 0 };
  }, []);

  // Helper function to get action color class
  const getActionClass = useCallback((action: string) => {
    switch (action) {
      case "charge":
      case "import":
        return "action-import";
      case "discharge":
      case "export":
        return "action-export";
      default:
        return "action-idle";
    }
  }, []);

  // Callback ref to create chart when div is mounted
  const chartRef = useCallback(
    (node: HTMLDivElement | null) => {
      if (!node) {
        return;
      }

      // If chart already exists, just return
      if (chartInstanceRef.current) {
        return;
      }

      // Add a small delay to ensure container is properly sized
      setTimeout(() => {
        // Ensure we have a valid width
        const width =
          node.clientWidth || node.parentElement?.clientWidth || 800;

        const opts: uPlot.Options = {
          width: width,
          height: 400,
          title: "Price Trends & MPC Actions",
          tzDate: (ts) => new Date(ts * 1000),
          padding: [25, 20, 0, 0],
          plugins: [
            {
              hooks: {
                setCursor: (u) => {
                  const tooltip = tooltipRef.current;
                  if (!tooltip) return;

                  const { left, top, idx } = u.cursor;

                  if (
                    idx == null ||
                    idx < 0 ||
                    idx >= decisionsRef.current.length
                  ) {
                    tooltip.style.display = "none";
                    return;
                  }

                  const decision = decisionsRef.current[idx];
                  const batteryAction = getBatteryAction(decision);
                  const gridAction = getGridAction(decision);

                  tooltip.innerHTML = `
                <div style="font-weight: bold; margin-bottom: 8px; border-bottom: 1px solid var(--color-border); padding-bottom: 4px;">
                  ${formatTimestamp(decision.timestamp)}
                </div>
                <div style="display: grid; grid-template-columns: auto 1fr; gap: 4px 8px; font-size: 12px;">
                  <div style="color: var(--color-text-secondary);">Battery:</div>
                  <div class="${getActionClass(batteryAction.action)}">
                    ${batteryAction.power > 0 ? `${batteryAction.power.toFixed(1)} kW` : "Idle"}
                  </div>

                  <div style="color: var(--color-text-secondary);">Grid:</div>
                  <div class="${getActionClass(gridAction.action)}">
                    ${gridAction.power > 0 ? `${gridAction.power.toFixed(1)} kW` : "Idle"}
                  </div>

                  <div style="color: var(--color-text-secondary);">SOC:</div>
                  <div>${(decision.battery_soc * 100).toFixed(1)}%</div>

                  <div style="color: var(--color-text-secondary);">Import:</div>
                  <div>${(decision.import_price * 1000).toFixed(2)} €/MWh</div>

                  <div style="color: var(--color-text-secondary);">Export:</div>
                  <div>${(decision.export_price * 1000).toFixed(2)} €/MWh</div>

                  <div style="color: var(--color-text-secondary);">Solar:</div>
                  <div>${decision.solar_forecast.toFixed(1)} kW</div>

                  <div style="color: var(--color-text-secondary);">Load:</div>
                  <div>${decision.load_forecast.toFixed(1)} kW</div>

                  <div style="color: var(--color-text-secondary);">Cloud:</div>
                  <div>${decision.cloud_coverage.toFixed(0)}%</div>

                  <div style="color: var(--color-text-secondary);">Weather:</div>
                  <div>${getWeatherIcon(decision.weather_symbol)}</div>

                  <div style="color: var(--color-text-secondary);">Profit:</div>
                  <div class="${decision.profit >= 0 ? "value-success" : "value-error"}">
                    €${decision.profit.toFixed(3)}
                  </div>
                </div>
              `;

                  tooltip.style.display = "block";

                  // Position tooltip
                  const chartRect = u.root.getBoundingClientRect();
                  const tooltipRect = tooltip.getBoundingClientRect();

                  let tooltipLeft = left! + 10;
                  let tooltipTop = top! + 10;

                  // Keep tooltip within chart bounds
                  if (tooltipLeft + tooltipRect.width > chartRect.width) {
                    tooltipLeft = left! - tooltipRect.width - 10;
                  }

                  if (tooltipTop + tooltipRect.height > chartRect.height) {
                    tooltipTop = chartRect.height - tooltipRect.height - 10;
                  }

                  tooltip.style.left = `${tooltipLeft}px`;
                  tooltip.style.top = `${tooltipTop}px`;
                },
                draw: (u) => {
                  // Draw candlesticks with battery action colors and grid action indicators
                  const ctx = u.ctx;
                  if (!ctx) return;

                  ctx.save();

                  const xdata = u.data[0];
                  const open = u.data[1];
                  const high = u.data[2];
                  const low = u.data[3];
                  const close = u.data[4];

                  const bodyMaxWidth = 16;

                  // Draw candlesticks
                  for (let i = 0; i < decisionsRef.current.length; i++) {
                    const xVal = xdata[i];
                    const yOpen = open[i];
                    const yHigh = high[i];
                    const yLow = low[i];
                    const yClose = close[i];

                    if (
                      xVal == null ||
                      yOpen == null ||
                      yHigh == null ||
                      yLow == null ||
                      yClose == null
                    ) {
                      continue;
                    }

                    const x = Math.round(u.valToPos(xVal, "x", true) ?? 0);
                    const yO = Math.round(u.valToPos(yOpen, "y", true) ?? 0);
                    const yH = Math.round(u.valToPos(yHigh, "y", true) ?? 0);
                    const yL = Math.round(u.valToPos(yLow, "y", true) ?? 0);
                    const yC = Math.round(u.valToPos(yClose, "y", true) ?? 0);

                    const width = Math.min(
                      bodyMaxWidth,
                      Math.max(3, u.bbox.width / xdata.length - 2),
                    );

                    // Draw wick (high-low line)
                    ctx.strokeStyle = "#f1f5f9";
                    ctx.lineWidth = 1;
                    ctx.beginPath();
                    ctx.moveTo(x, yL);
                    ctx.lineTo(x, yH);
                    ctx.stroke();

                    // Draw body with battery action color
                    const bodyTop = Math.min(yO, yC);
                    const bodyHeight = Math.abs(yO - yC);

                    const decision = decisionsRef.current[i];
                    const batteryAction = getBatteryAction(decision);

                    if (batteryAction.action === "charge") {
                      ctx.fillStyle = "#ea580c"; // Orange for charge
                    } else if (batteryAction.action === "discharge") {
                      ctx.fillStyle = "#16a34a"; // Green for discharge
                    } else {
                      ctx.fillStyle = "#94a3b8"; // Gray for idle
                    }

                    ctx.fillRect(
                      x - width / 2,
                      bodyTop,
                      width,
                      bodyHeight || 1,
                    );

                    // Draw border
                    ctx.strokeStyle = "#f1f5f9";
                    ctx.lineWidth = 1;
                    ctx.strokeRect(
                      x - width / 2,
                      bodyTop,
                      width,
                      bodyHeight || 1,
                    );
                  }

                  // Draw grid action indicators
                  ctx.font = "bold 16px sans-serif";

                  for (let i = 0; i < decisionsRef.current.length; i++) {
                    const decision = decisionsRef.current[i];
                    const gridAction = getGridAction(decision);

                    const cx = u.valToPos(decision.timestamp, "x", true);
                    const cy = u.valToPos(highsRef.current[i], "y", true);

                    if (cx && cy) {
                      // Draw grid action indicator with bigger arrows
                      if (gridAction.action === "export") {
                        ctx.fillStyle = "#16a34a";
                        ctx.fillText("↑", cx + 6, cy - 10);
                      } else if (gridAction.action === "import") {
                        ctx.fillStyle = "#ea580c";
                        ctx.fillText("↓", cx + 6, cy - 10);
                      }
                    }
                  }

                  ctx.restore();
                },
              },
            },
          ],
          scales: {
            x: {
              time: true,
              range: (_u, min, max) => {
                const timeRange = max - min;
                const padding = timeRange * 0.02; // 2% padding on each side
                return [min, max + padding];
              },
            },
            y: {
              range: (_u, min, max) => {
                // uPlot's auto-scaling only considers visible series, but our candlestick
                // series are hidden. We need to manually calculate from all data.
                const allHighs = highsRef.current || [];
                const allLows =
                  decisionsRef.current?.map((d) => d.export_price * 1000) || [];

                if (allHighs.length === 0) {
                  return [min, max + 100];
                }

                const actualMax = Math.max(...allHighs);
                const actualMin = Math.min(...allLows);
                const range = actualMax - actualMin;
                const padding = range * 0.1; // 10% padding

                return [actualMin - padding * 0.5, actualMax + padding];
              },
            },
          },
          axes: [
            {
              stroke: "#94a3b8",
              grid: {
                show: true,
                stroke: "#334155",
                width: 1,
              },
              ticks: {
                stroke: "#334155",
              },
              values: (_u, vals) =>
                vals.map((v) => {
                  const date = new Date(v * 1000);
                  return date
                    .toLocaleString("en-US", {
                      month: "numeric",
                      day: "numeric",
                      hour: "2-digit",
                      hour12: false,
                    })
                    .replace(/,/, "");
                }),
            },
            {
              stroke: "#94a3b8",
              label: "Price (€/MWh)",
              labelSize: 30,
              labelFont: "12px sans-serif",
              size: 70,
              gap: 10,
              grid: {
                show: true,
                stroke: "#334155",
                width: 1,
              },
              ticks: {
                stroke: "#334155",
              },
              values: (_u, vals) => vals.map((v) => v.toFixed(2)),
            },
          ],
          series: [
            {
              label: "Time",
            },
            {
              label: "Export (Open)",
              stroke: "#f1f5f9",
              width: 0,
              points: { show: false },
            },
            {
              label: "High (Import)",
              stroke: "#f1f5f9",
              width: 0,
              points: { show: false },
              show: false,
            },
            {
              label: "Low (Export)",
              stroke: "#f1f5f9",
              width: 0,
              points: { show: false },
              show: false,
            },
            {
              label: "Import (Close)",
              stroke: "#f1f5f9",
              width: 0,
              points: { show: false },
              show: false,
            },
          ],
          cursor: {
            points: {
              show: false,
            },
            drag: {
              x: false,
              y: false,
            },
            sync: {
              key: "mpc",
            },
          },
          legend: {
            show: false,
          },
        };

        // Create chart with empty initial data
        const emptyData: uPlot.AlignedData = [[], [], [], [], []];
        const chart = new uPlot(opts, emptyData, node);
        chartInstanceRef.current = chart;

        // Mark chart as ready
        setChartReady(true);

        // Add ResizeObserver to handle container size changes
        const resizeObserver = new ResizeObserver(() => {
          if (chartInstanceRef.current && node.clientWidth > 0) {
            const newWidth = node.clientWidth;
            chartInstanceRef.current.setSize({ width: newWidth, height: 400 });
          }
        });

        resizeObserver.observe(node);

        // Store cleanup function
        (node as any)._resizeObserver = resizeObserver;
      }, 100); // 100ms delay to ensure DOM is ready
    },
    [getBatteryAction, getGridAction, getActionClass],
  );

  // Cleanup on unmount
  useEffect(() => {
    return () => {
      // Cleanup ResizeObserver if it exists
      const chartNode = chartInstanceRef.current?.root;
      if (chartNode && (chartNode as any)._resizeObserver) {
        (chartNode as any)._resizeObserver.disconnect();
      }

      if (chartInstanceRef.current) {
        chartInstanceRef.current.destroy();
        chartInstanceRef.current = null;
      }

      setChartReady(false);
    };
  }, []);

  // Update chart data when decisions change
  useEffect(() => {
    if (!decisions || decisions.length === 0) {
      return;
    }

    if (!chartInstanceRef.current || !chartReady) {
      return;
    }

    // Update refs with current decisions data
    decisionsRef.current = decisions;

    // Prepare data for candlestick chart
    const timestamps = decisions.map((d) => d.timestamp);

    // Convert prices from €/MWh to €/kWh for better visualization
    const importPrices = decisions.map((d) => d.import_price * 1000);
    const exportPrices = decisions.map((d) => d.export_price * 1000);

    // Calculate open, high, low, close for candlesticks
    const opens = exportPrices;
    const closes = importPrices;
    const highs = importPrices;
    const lows = exportPrices;

    // Store highs in ref for chart callbacks
    highsRef.current = highs;

    const data: uPlot.AlignedData = [timestamps, opens, highs, lows, closes];

    // Update the existing chart with new data
    chartInstanceRef.current.setData(data);
  }, [decisions, chartReady]);

  if (!decisions || decisions.length === 0) {
    return (
      <section className="card">
        <h2>Model Predictive Control Optimization Results</h2>
        <div className="info-grid">
          <p>No optimization data available yet.</p>
        </div>
      </section>
    );
  }

  // Calculate total profit
  const totalProfit = decisions.reduce((sum, dec) => sum + dec.profit, 0);

  return (
    <section className="card">
      <h2>Model Predictive Control Optimization Results</h2>
      <div className="mpc-summary">
        <div className="mpc-summary-item">
          <span className="mpc-summary-label">Decisions:</span>
          <span className="mpc-summary-value">
            {decisions.length} intervals (15min)
          </span>
        </div>
        <div className="mpc-summary-item">
          <span className="mpc-summary-label">Total Expected Profit:</span>
          <span
            className={`mpc-summary-value ${totalProfit >= 0 ? "value-success" : "value-error"}`}
          >
            €{totalProfit.toFixed(2)}
          </span>
        </div>
      </div>

      {/* Candlestick Chart */}
      <div
        className="mpc-chart-container"
        style={{ marginBottom: "1.5rem", position: "relative" }}
      >
        <div
          ref={chartRef}
          style={{
            backgroundColor: "rgba(30, 41, 59, 0.5)",
            borderRadius: "6px",
            padding: "1rem",
            border: "1px solid var(--color-border)",
            minHeight: "450px",
            width: "100%",
          }}
        />
        <div
          ref={tooltipRef}
          style={{
            display: "none",
            position: "absolute",
            backgroundColor: "rgba(30, 41, 59, 0.95)",
            border: "1px solid var(--color-border)",
            borderRadius: "6px",
            padding: "8px 12px",
            pointerEvents: "none",
            zIndex: 1000,
            fontSize: "13px",
            minWidth: "200px",
            boxShadow: "0 4px 6px -1px rgba(0, 0, 0, 0.3)",
          }}
        />
        <div
          className="mpc-chart-legend"
          style={{
            marginTop: "1rem",
            padding: "1rem",
            backgroundColor: "rgba(51, 65, 85, 0.5)",
            borderRadius: "6px",
            display: "flex",
            flexWrap: "wrap",
            gap: "1rem",
            fontSize: "0.875rem",
          }}
        >
          <div style={{ display: "flex", alignItems: "center", gap: "0.5rem" }}>
            <div
              style={{
                width: "20px",
                height: "20px",
                backgroundColor: "#16a34a",
                border: "2px solid #f1f5f9",
                borderRadius: "2px",
              }}
            ></div>
            <span>Battery Discharge</span>
          </div>
          <div style={{ display: "flex", alignItems: "center", gap: "0.5rem" }}>
            <div
              style={{
                width: "20px",
                height: "20px",
                backgroundColor: "#ea580c",
                border: "2px solid #f1f5f9",
                borderRadius: "2px",
              }}
            ></div>
            <span>Battery Charge</span>
          </div>
          <div style={{ display: "flex", alignItems: "center", gap: "0.5rem" }}>
            <div
              style={{
                width: "20px",
                height: "20px",
                backgroundColor: "#94a3b8",
                border: "2px solid #f1f5f9",
                borderRadius: "2px",
              }}
            ></div>
            <span>Battery Idle</span>
          </div>
          <div style={{ display: "flex", alignItems: "center", gap: "0.5rem" }}>
            <span style={{ color: "#16a34a", fontSize: "16px" }}>↑</span>
            <span>Grid Export</span>
          </div>
          <div style={{ display: "flex", alignItems: "center", gap: "0.5rem" }}>
            <span style={{ color: "#ea580c", fontSize: "16px" }}>↓</span>
            <span>Grid Import</span>
          </div>
        </div>
      </div>

      <div
        style={{
          marginTop: "1rem",
          marginBottom: "0.5rem",
          textAlign: "center",
        }}
      >
        <button
          onClick={() => setShowTable(!showTable)}
          style={{
            padding: "0.5rem 1rem",
            backgroundColor: "var(--color-primary)",
            color: "white",
            border: "none",
            borderRadius: "4px",
            cursor: "pointer",
            fontSize: "0.875rem",
            fontWeight: "600",
          }}
        >
          {showTable ? "Hide Table" : "Show Table"}
        </button>
      </div>

      {showTable && (
        <div style={{ position: "relative" }}>
          {/* Scroll navigation buttons */}
          {canScrollLeft && (
            <button
              onClick={() => scrollTable("left")}
              style={{
                position: "absolute",
                left: "130px",
                top: "50%",
                transform: "translateY(-50%)",
                zIndex: 20,
                backgroundColor: "var(--color-primary)",
                color: "white",
                border: "none",
                borderRadius: "50%",
                width: "40px",
                height: "40px",
                cursor: "pointer",
                boxShadow: "0 4px 12px rgba(0, 0, 0, 0.5)",
                display: "flex",
                alignItems: "center",
                justifyContent: "center",
                fontSize: "20px",
                fontWeight: "bold",
                transition: "all 0.2s",
              }}
              onMouseEnter={(e) => {
                e.currentTarget.style.transform = "translateY(-50%) scale(1.1)";
              }}
              onMouseLeave={(e) => {
                e.currentTarget.style.transform = "translateY(-50%) scale(1)";
              }}
            >
              ←
            </button>
          )}
          {canScrollRight && (
            <button
              onClick={() => scrollTable("right")}
              style={{
                position: "absolute",
                right: "10px",
                top: "50%",
                transform: "translateY(-50%)",
                zIndex: 20,
                backgroundColor: "var(--color-primary)",
                color: "white",
                border: "none",
                borderRadius: "50%",
                width: "40px",
                height: "40px",
                cursor: "pointer",
                boxShadow: "0 4px 12px rgba(0, 0, 0, 0.5)",
                display: "flex",
                alignItems: "center",
                justifyContent: "center",
                fontSize: "20px",
                fontWeight: "bold",
                transition: "all 0.2s",
              }}
              onMouseEnter={(e) => {
                e.currentTarget.style.transform = "translateY(-50%) scale(1.1)";
              }}
              onMouseLeave={(e) => {
                e.currentTarget.style.transform = "translateY(-50%) scale(1)";
              }}
            >
              →
            </button>
          )}
          <div className="mpc-table-container" ref={tableContainerRef}>
            <table className="mpc-table">
              <thead>
                {/* Date row */}
                <tr>
                  <th rowSpan={3}>Metric</th>
                  {(() => {
                    const dateGroups: Array<{
                      dateKey: string;
                      count: number;
                    }> = [];
                    let currentDateKey: string | null = null;

                    decisions.forEach((decision) => {
                      const date = new Date(decision.timestamp * 1000);
                      const dateKey = `${date.getMonth() + 1}/${date.getDate()}`;

                      if (currentDateKey === dateKey) {
                        dateGroups[dateGroups.length - 1].count++;
                      } else {
                        dateGroups.push({ dateKey, count: 1 });
                        currentDateKey = dateKey;
                      }
                    });

                    return dateGroups.map(({ dateKey, count }) => (
                      <th
                        key={dateKey}
                        colSpan={count}
                        style={{
                          textAlign: "center",
                          borderBottom: "1px solid var(--color-border)",
                        }}
                      >
                        {dateKey}
                      </th>
                    ));
                  })()}
                </tr>
                {/* Hour row */}
                <tr>
                  {(() => {
                    const hourGroups: Array<{
                      groupKey: string;
                      count: number;
                      hour: number;
                    }> = [];
                    let currentGroupKey: string | null = null;

                    decisions.forEach((decision) => {
                      const date = new Date(decision.timestamp * 1000);
                      const dateKey = `${date.getMonth() + 1}/${date.getDate()}`;
                      const hour = date.getHours();
                      const groupKey = `${dateKey}-${hour}`;

                      if (currentGroupKey === groupKey) {
                        hourGroups[hourGroups.length - 1].count++;
                      } else {
                        hourGroups.push({ groupKey, count: 1, hour });
                        currentGroupKey = groupKey;
                      }
                    });

                    return hourGroups.map(({ groupKey, count, hour }) => (
                      <th
                        key={groupKey}
                        colSpan={count}
                        style={{
                          textAlign: "center",
                          borderBottom: "1px solid var(--color-border)",
                          borderLeft: "2px solid var(--color-border)",
                        }}
                      >
                        {hour.toString().padStart(2, "0")}:00
                      </th>
                    ));
                  })()}
                </tr>
                {/* Minute row */}
                <tr>
                  {decisions.map((decision, index) => {
                    const date = new Date(decision.timestamp * 1000);
                    const minute = date
                      .getMinutes()
                      .toString()
                      .padStart(2, "0");

                    // Check if this is the start of a new hour
                    const isHourStart =
                      index === 0 ||
                      new Date(
                        decisions[index - 1].timestamp * 1000,
                      ).getHours() !== date.getHours();

                    return (
                      <th
                        key={decision.timestamp}
                        style={{
                          fontSize: "0.8em",
                          borderLeft: isHourStart
                            ? "2px solid var(--color-border)"
                            : undefined,
                        }}
                      >
                        {minute}
                      </th>
                    );
                  })}
                </tr>
              </thead>
              <tbody>
                <tr>
                  <th>Battery Action (kW)</th>
                  {decisions.map((decision, index) => {
                    const batteryAction = getBatteryAction(decision);
                    const date = new Date(decision.timestamp * 1000);
                    const isHourStart =
                      index === 0 ||
                      new Date(
                        decisions[index - 1].timestamp * 1000,
                      ).getHours() !== date.getHours();

                    return (
                      <td
                        key={decision.timestamp}
                        style={{
                          borderLeft: isHourStart
                            ? "2px solid var(--color-border)"
                            : undefined,
                        }}
                      >
                        <span className={getActionClass(batteryAction.action)}>
                          {batteryAction.power > 0
                            ? batteryAction.power.toFixed(1)
                            : ""}
                        </span>
                      </td>
                    );
                  })}
                </tr>
                <tr>
                  <th>Grid Action (kW)</th>
                  {decisions.map((decision, index) => {
                    const gridAction = getGridAction(decision);
                    const date = new Date(decision.timestamp * 1000);
                    const isHourStart =
                      index === 0 ||
                      new Date(
                        decisions[index - 1].timestamp * 1000,
                      ).getHours() !== date.getHours();

                    return (
                      <td
                        key={decision.timestamp}
                        style={{
                          borderLeft: isHourStart
                            ? "2px solid var(--color-border)"
                            : undefined,
                        }}
                      >
                        <span className={getActionClass(gridAction.action)}>
                          {gridAction.power > 0
                            ? gridAction.power.toFixed(1)
                            : ""}
                        </span>
                      </td>
                    );
                  })}
                </tr>
                <tr>
                  <th>SOC (%)</th>
                  {decisions.map((decision, index) => {
                    const date = new Date(decision.timestamp * 1000);
                    const isHourStart =
                      index === 0 ||
                      new Date(
                        decisions[index - 1].timestamp * 1000,
                      ).getHours() !== date.getHours();

                    return (
                      <td
                        key={decision.timestamp}
                        style={{
                          borderLeft: isHourStart
                            ? "2px solid var(--color-border)"
                            : undefined,
                        }}
                      >
                        {(decision.battery_soc * 100).toFixed(1)}
                      </td>
                    );
                  })}
                </tr>

                <tr>
                  <th>Solar (kW)</th>
                  {decisions.map((decision, index) => {
                    const date = new Date(decision.timestamp * 1000);
                    const isHourStart =
                      index === 0 ||
                      new Date(
                        decisions[index - 1].timestamp * 1000,
                      ).getHours() !== date.getHours();

                    return (
                      <td
                        key={decision.timestamp}
                        style={{
                          borderLeft: isHourStart
                            ? "2px solid var(--color-border)"
                            : undefined,
                        }}
                      >
                        {decision.solar_forecast.toFixed(1)}
                      </td>
                    );
                  })}
                </tr>
                <tr>
                  <th>Load (kW)</th>
                  {decisions.map((decision, index) => {
                    const date = new Date(decision.timestamp * 1000);
                    const isHourStart =
                      index === 0 ||
                      new Date(
                        decisions[index - 1].timestamp * 1000,
                      ).getHours() !== date.getHours();

                    return (
                      <td
                        key={decision.timestamp}
                        style={{
                          borderLeft: isHourStart
                            ? "2px solid var(--color-border)"
                            : undefined,
                        }}
                      >
                        {decision.load_forecast.toFixed(1)}
                      </td>
                    );
                  })}
                </tr>
                <tr>
                  <th>Cloud (%)</th>
                  {(() => {
                    const hourGroups: Array<{
                      groupKey: string;
                      count: number;
                      cloudCoverage: number;
                    }> = [];
                    let currentGroupKey: string | null = null;

                    decisions.forEach((decision) => {
                      const date = new Date(decision.timestamp * 1000);
                      const dateKey = `${date.getMonth() + 1}/${date.getDate()}`;
                      const hour = date.getHours();
                      const groupKey = `${dateKey}-${hour}`;

                      if (currentGroupKey === groupKey) {
                        hourGroups[hourGroups.length - 1].count++;
                      } else {
                        hourGroups.push({
                          groupKey,
                          count: 1,
                          cloudCoverage: decision.cloud_coverage,
                        });
                        currentGroupKey = groupKey;
                      }
                    });

                    return hourGroups.map(
                      ({ groupKey, count, cloudCoverage }) => (
                        <td
                          key={groupKey}
                          colSpan={count}
                          style={{
                            textAlign: "center",
                            borderLeft: "2px solid var(--color-border)",
                          }}
                        >
                          {cloudCoverage.toFixed(0)}
                        </td>
                      ),
                    );
                  })()}
                </tr>
                <tr>
                  <th>Weather</th>
                  {(() => {
                    const hourGroups: Array<{
                      groupKey: string;
                      count: number;
                      weatherSymbol: string;
                    }> = [];
                    let currentGroupKey: string | null = null;

                    decisions.forEach((decision) => {
                      const date = new Date(decision.timestamp * 1000);
                      const dateKey = `${date.getMonth() + 1}/${date.getDate()}`;
                      const hour = date.getHours();
                      const groupKey = `${dateKey}-${hour}`;

                      if (currentGroupKey === groupKey) {
                        hourGroups[hourGroups.length - 1].count++;
                      } else {
                        hourGroups.push({
                          groupKey,
                          count: 1,
                          weatherSymbol: decision.weather_symbol,
                        });
                        currentGroupKey = groupKey;
                      }
                    });

                    return hourGroups.map(
                      ({ groupKey, count, weatherSymbol }) => (
                        <td
                          key={groupKey}
                          colSpan={count}
                          title={weatherSymbol || "Unknown"}
                          style={{
                            textAlign: "center",
                            borderLeft: "2px solid var(--color-border)",
                          }}
                        >
                          {getWeatherIcon(weatherSymbol)}
                        </td>
                      ),
                    );
                  })()}
                </tr>
                <tr>
                  <th>Air Temp (°C)</th>
                  {(() => {
                    const hourGroups: Array<{
                      groupKey: string;
                      count: number;
                      airTemperature: number;
                    }> = [];
                    let currentGroupKey: string | null = null;

                    decisions.forEach((decision) => {
                      const date = new Date(decision.timestamp * 1000);
                      const dateKey = `${date.getMonth() + 1}/${date.getDate()}`;
                      const hour = date.getHours();
                      const groupKey = `${dateKey}-${hour}`;

                      if (currentGroupKey === groupKey) {
                        hourGroups[hourGroups.length - 1].count++;
                      } else {
                        hourGroups.push({
                          groupKey,
                          count: 1,
                          airTemperature: decision.air_temperature,
                        });
                        currentGroupKey = groupKey;
                      }
                    });

                    return hourGroups.map(
                      ({ groupKey, count, airTemperature }) => (
                        <td
                          key={groupKey}
                          colSpan={count}
                          style={{
                            textAlign: "center",
                            borderLeft: "2px solid var(--color-border)",
                          }}
                        >
                          {airTemperature.toFixed(1)}
                        </td>
                      ),
                    );
                  })()}
                </tr>
                <tr>
                  <th>Avg Cell Temp (°C)</th>
                  {decisions.map((decision, index) => {
                    const date = new Date(decision.timestamp * 1000);
                    const isHourStart =
                      index === 0 ||
                      new Date(
                        decisions[index - 1].timestamp * 1000,
                      ).getHours() !== date.getHours();

                    return (
                      <td
                        key={decision.timestamp}
                        style={{
                          borderLeft: isHourStart
                            ? "2px solid var(--color-border)"
                            : undefined,
                        }}
                      >
                        {decision.battery_avg_cell_temp?.toFixed(1) || "-"}
                      </td>
                    );
                  })}
                </tr>
                <tr>
                  <th>Profit (€)</th>
                  {decisions.map((decision, index) => {
                    const date = new Date(decision.timestamp * 1000);
                    const isHourStart =
                      index === 0 ||
                      new Date(
                        decisions[index - 1].timestamp * 1000,
                      ).getHours() !== date.getHours();

                    return (
                      <td
                        key={decision.timestamp}
                        style={{
                          borderLeft: isHourStart
                            ? "2px solid var(--color-border)"
                            : undefined,
                        }}
                      >
                        <span
                          className={
                            decision.profit >= 0
                              ? "value-success"
                              : "value-error"
                          }
                        >
                          {Math.abs(decision.profit).toFixed(2)}
                        </span>
                      </td>
                    );
                  })}
                </tr>
              </tbody>
            </table>
          </div>
          {/* Scroll hint */}
          {(canScrollLeft || canScrollRight) && (
            <div
              style={{
                textAlign: "center",
                marginTop: "0.5rem",
                fontSize: "0.75rem",
                color: "var(--color-text-secondary)",
              }}
            >
              {canScrollLeft &&
                canScrollRight &&
                "← Scroll horizontally to view all data →"}
              {canScrollLeft &&
                !canScrollRight &&
                "← Scroll left to view earlier data"}
              {!canScrollLeft &&
                canScrollRight &&
                "Scroll right to view more data →"}
            </div>
          )}
        </div>
      )}
    </section>
  );
}
