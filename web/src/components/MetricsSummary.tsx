import { useEffect, useState, useRef } from "react";
import { MetricsSummary as MetricsSummaryType } from "../types/api";
import "../App.css";

// Check if we're in demo mode
const isDemoMode = typeof __DEMO_MODE__ !== 'undefined' && __DEMO_MODE__;

// Generate mock metrics summary data for demo mode
function generateMockMetricsSummary(date: Date): MetricsSummaryType {
  const dayOfMonth = date.getDate();
  
  // Use day of month to generate consistent but varying data
  const seed = dayOfMonth / 31;
  
  // Generate realistic import/export values
  const totalImportKwh = 50 + seed * 100; // 50-150 kWh
  const totalExportKwh = 30 + seed * 80; // 30-110 kWh
  
  // Typical electricity prices in €/kWh
  const avgImportPrice = 0.12; // 12 cents per kWh
  const avgExportPrice = 0.08; // 8 cents per kWh
  
  const totalImportCost = totalImportKwh * avgImportPrice;
  const totalExportCost = totalExportKwh * avgExportPrice;
  
  const startTime = new Date(date);
  startTime.setHours(0, 0, 0, 0);
  
  const endTime = new Date(date);
  endTime.setHours(23, 59, 59, 999);
  
  return {
    total_import_cost: totalImportCost,
    total_export_cost: totalExportCost,
    total_import_kwh: totalImportKwh,
    total_export_kwh: totalExportKwh,
    start_time: startTime.toISOString(),
    end_time: endTime.toISOString(),
  };
}

// Generate mock monthly summary by summing mock daily data for the month
function generateMockMonthlySummary(date: Date): MetricsSummaryType {
  const { start, end } = getMonthRange(date);
  const today = new Date();
  const result: MetricsSummaryType = {
    total_import_cost: 0,
    total_export_cost: 0,
    total_import_kwh: 0,
    total_export_kwh: 0,
    start_time: start.toISOString(),
    end_time: end.toISOString(),
  };
  for (
    const d = new Date(start);
    d <= end && d <= today;
    d.setDate(d.getDate() + 1)
  ) {
    const day = generateMockMetricsSummary(d);
    result.total_import_cost += day.total_import_cost;
    result.total_export_cost += day.total_export_cost;
    result.total_import_kwh += day.total_import_kwh;
    result.total_export_kwh += day.total_export_kwh;
  }
  return result;
}

function getMonthRange(date: Date): { start: Date; end: Date } {
  const start = new Date(date.getFullYear(), date.getMonth(), 1, 0, 0, 0, 0);
  const end = new Date(
    date.getFullYear(),
    date.getMonth() + 1,
    0,
    23,
    59,
    59,
    999,
  );
  return { start, end };
}

async function fetchSummary(
  start: Date,
  end: Date,
): Promise<MetricsSummaryType> {
  const url = `/api/metrics/summary?start_time=${encodeURIComponent(start.toISOString())}&end_time=${encodeURIComponent(end.toISOString())}`;
  const response = await fetch(url);
  if (!response.ok) {
    throw new Error(`HTTP error! status: ${response.status}`);
  }
  return response.json();
}

function SummaryRows({ summary }: { summary: MetricsSummaryType }) {
  // Import cost is spending, export cost is revenue
  const net = summary.total_import_cost - summary.total_export_cost;
  return (
    <>
      <div className="mpc-summary-item">
        <span className="mpc-summary-label">Import Cost:</span>
        <span className="mpc-summary-value value-error">
          €{summary.total_import_cost.toFixed(2)} (
          {summary.total_import_kwh.toFixed(2)} kWh)
        </span>
      </div>
      <div className="mpc-summary-item">
        <span className="mpc-summary-label">Export Revenue:</span>
        <span className="mpc-summary-value value-success">
          €{summary.total_export_cost.toFixed(2)} (
          {summary.total_export_kwh.toFixed(2)} kWh)
        </span>
      </div>
      <div className="mpc-summary-item">
        <span className="mpc-summary-label">
          {net <= 0 ? "Net Revenue:" : "Net Cost:"}
        </span>
        <span
          className={`mpc-summary-value ${net <= 0 ? "value-success" : "value-error"}`}
        >
          €{Math.abs(net).toFixed(2)}
        </span>
      </div>
    </>
  );
}

export function MetricsSummary() {
  const [metricsSummary, setMetricsSummary] =
    useState<MetricsSummaryType | null>(null);
  const [monthlySummary, setMonthlySummary] =
    useState<MetricsSummaryType | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [selectedDate, setSelectedDate] = useState<Date>(() => {
    const today = new Date();
    today.setHours(0, 0, 0, 0);
    return today;
  });
  const isFetchingRef = useRef(false);

  useEffect(() => {
    const fetchMetricsSummary = async () => {
      // Prevent duplicate fetches
      if (isFetchingRef.current) {
        console.log("Already fetching metrics, skipping...");
        return;
      }

      isFetchingRef.current = true;

      try {
        setLoading(true);

        // In demo mode, use mock data
        if (isDemoMode) {
          // Simulate network delay
          await new Promise(resolve => setTimeout(resolve, 300));
          setMetricsSummary(generateMockMetricsSummary(selectedDate));
          setMonthlySummary(generateMockMonthlySummary(selectedDate));
          setError(null);
        } else {
          // Selected calendar day (midnight to midnight) and its calendar month
          const dayEnd = new Date(selectedDate);
          dayEnd.setHours(23, 59, 59, 999);
          const month = getMonthRange(selectedDate);

          const [daily, monthly] = await Promise.all([
            fetchSummary(selectedDate, dayEnd),
            fetchSummary(month.start, month.end),
          ]);
          setMetricsSummary(daily);
          setMonthlySummary(monthly);
          setError(null);
        }
      } catch (error) {
        console.error("Failed to fetch metrics summary:", error);
        setError("Failed to load data");
      } finally {
        setLoading(false);
        isFetchingRef.current = false;
      }
    };

    fetchMetricsSummary();
  }, [selectedDate]);

  const handleDateNavigation = (dayShift: number | null) => {
    if (dayShift === null) {
      // Navigate to today
      const today = new Date();
      today.setHours(0, 0, 0, 0);
      setSelectedDate(today);
    } else {
      // Navigate by day shift (positive or negative)
      setSelectedDate((prev) => {
        const newDate = new Date(prev);
        newDate.setDate(newDate.getDate() + dayShift);
        return newDate;
      });
    }
  };

  const formatDateDisplay = (date: Date): string => {
    return date.toLocaleDateString("en-US", {
      year: "numeric",
      month: "long",
      day: "numeric",
    });
  };

  const isToday = () => {
    const today = new Date();
    today.setHours(0, 0, 0, 0);
    return selectedDate.getTime() === today.getTime();
  };

  return (
    <section className="card">
      <div
        style={{
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
          marginBottom: "1rem",
        }}
      >
        <h2 style={{ margin: 0 }}>
          Data - {formatDateDisplay(selectedDate)}
        </h2>
        <div style={{ display: "flex", gap: "0.5rem", alignItems: "center" }}>
          <button
            onClick={() => handleDateNavigation(-1)}
            style={{
              padding: "0.25rem 0.5rem",
              cursor: "pointer",
              border: "1px solid var(--color-border)",
              borderRadius: "4px",
              backgroundColor: "var(--color-bg-secondary)",
              color: "var(--color-text)",
            }}
            title="Previous day"
          >
            ←
          </button>
          <button
            onClick={() => handleDateNavigation(null)}
            disabled={isToday()}
            style={{
              padding: "0.25rem 0.75rem",
              cursor: isToday() ? "not-allowed" : "pointer",
              border: "1px solid var(--color-border)",
              borderRadius: "4px",
              backgroundColor: isToday()
                ? "var(--color-bg)"
                : "var(--color-bg-secondary)",
              color: isToday()
                ? "var(--color-text-secondary)"
                : "var(--color-text)",
              opacity: isToday() ? 0.5 : 1,
            }}
            title="Today"
          >
            Today
          </button>
          <button
            onClick={() => handleDateNavigation(1)}
            disabled={isToday()}
            style={{
              padding: "0.25rem 0.5rem",
              cursor: isToday() ? "not-allowed" : "pointer",
              border: "1px solid var(--color-border)",
              borderRadius: "4px",
              backgroundColor: isToday()
                ? "var(--color-bg)"
                : "var(--color-bg-secondary)",
              color: isToday()
                ? "var(--color-text-secondary)"
                : "var(--color-text)",
              opacity: isToday() ? 0.5 : 1,
            }}
            title="Next day"
          >
            →
          </button>
        </div>
      </div>

      <div className="mpc-summary" style={{ flexDirection: "column", gap: "1rem" }}>
        {loading && (
          <div className="mpc-summary-item">
            <span className="mpc-summary-label">Loading...</span>
          </div>
        )}
        {error && (
          <div className="mpc-summary-item">
            <span
              className="mpc-summary-label"
              style={{ color: "var(--color-error)" }}
            >
              {error}
            </span>
          </div>
        )}
        {metricsSummary && !loading && (
          <div>
            <h3 style={{ margin: "0 0 0.5rem" }}>Day</h3>
            <div style={{ display: "flex", gap: "2rem", flexWrap: "wrap" }}>
              <SummaryRows summary={metricsSummary} />
            </div>
          </div>
        )}
        {monthlySummary && !loading && (
          <div>
            <h3 style={{ margin: "0 0 0.5rem" }}>
              Month -{" "}
              {selectedDate.toLocaleDateString("en-US", {
                year: "numeric",
                month: "long",
              })}
            </h3>
            <div style={{ display: "flex", gap: "2rem", flexWrap: "wrap" }}>
              <SummaryRows summary={monthlySummary} />
            </div>
          </div>
        )}
      </div>
    </section>
  );
}
