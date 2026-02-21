import { getPowerColor } from "../utils/powerUtils";

interface PowerDisplayProps {
  value: number | undefined;
  label?: string;
  unit?: string;
  decimals?: number;
  style?: React.CSSProperties;
  invertColors?: boolean;
  showLabel?: boolean;
}

export function PowerDisplay({
  value,
  label,
  unit = "kW",
  decimals = 2,
  style,
  invertColors = false,
  showLabel = false,
}: PowerDisplayProps) {
  const displayValue =
    value !== undefined ? `${Math.abs(value).toFixed(decimals)} ${unit}` : "N/A";
  const color = getPowerColor(value, invertColors);

  return (
    <div className="pv-power-display" style={style}>
      {showLabel && label && <div className="pv-power-label">{label}</div>}
      <div className="pv-power-value" style={{ color }}>
        {displayValue}
      </div>
      {!showLabel && label && (
        <div className="pv-power-label" style={{ marginTop: "8px" }}>
          {label}
        </div>
      )}
    </div>
  );
}
