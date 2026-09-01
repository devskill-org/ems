import { useState, useEffect, useCallback, useRef, DragEvent } from "react";
import "./MarketDataUpload.css";

interface CacheEntry {
  date: string;
  uploaded_at: string;
  source: "upload" | "download" | string;
}

interface MarketDataUploadProps {
  onClose: () => void;
}

export function MarketDataUpload({ onClose }: MarketDataUploadProps) {
  // Upload form state
  const [date, setDate] = useState<string>(() => {
    const now = new Date();
    return now.toISOString().slice(0, 10);
  });
  const [file, setFile] = useState<File | null>(null);
  const [uploading, setUploading] = useState(false);
  const [uploadError, setUploadError] = useState<string | null>(null);
  const [uploadSuccess, setUploadSuccess] = useState<string | null>(null);
  const [dragging, setDragging] = useState(false);

  // Cached entries state
  const [entries, setEntries] = useState<CacheEntry[]>([]);
  const [loadingEntries, setLoadingEntries] = useState(false);
  const [deletingDate, setDeletingDate] = useState<string | null>(null);

  const fileInputRef = useRef<HTMLInputElement>(null);

  // ── Fetch entries ──────────────────────────────────────────
  const fetchEntries = useCallback(async () => {
    setLoadingEntries(true);
    try {
      const res = await fetch("/api/market-data/cache");
      if (!res.ok) throw new Error(`Server returned ${res.status}`);
      const data = await res.json();
      setEntries(
        Array.isArray(data.entries)
          ? [...data.entries].sort((a: CacheEntry, b: CacheEntry) =>
              a.date.localeCompare(b.date),
            )
          : [],
      );
    } catch {
      // silently ignore – the list will just be empty
    } finally {
      setLoadingEntries(false);
    }
  }, []);

  useEffect(() => {
    fetchEntries();
  }, [fetchEntries]);

  // Close on Escape
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    window.addEventListener("keydown", handler);
    return () => window.removeEventListener("keydown", handler);
  }, [onClose]);

  // ── File selection ─────────────────────────────────────────
  const handleFileChange = useCallback(
    (selected: File | null) => {
      if (!selected) return;
      if (
        !selected.name.endsWith(".xml") &&
        selected.type !== "application/xml" &&
        selected.type !== "text/xml"
      ) {
        setUploadError("Only XML files are accepted.");
        return;
      }
      setFile(selected);
      setUploadError(null);
      setUploadSuccess(null);
    },
    [],
  );

  const handleInputChange = useCallback(
    (e: React.ChangeEvent<HTMLInputElement>) => {
      handleFileChange(e.target.files?.[0] ?? null);
    },
    [handleFileChange],
  );

  // ── Drag & drop ────────────────────────────────────────────
  const handleDragOver = useCallback((e: DragEvent<HTMLDivElement>) => {
    e.preventDefault();
    setDragging(true);
  }, []);

  const handleDragLeave = useCallback((e: DragEvent<HTMLDivElement>) => {
    e.preventDefault();
    setDragging(false);
  }, []);

  const handleDrop = useCallback(
    (e: DragEvent<HTMLDivElement>) => {
      e.preventDefault();
      setDragging(false);
      handleFileChange(e.dataTransfer.files?.[0] ?? null);
    },
    [handleFileChange],
  );

  // ── Upload ─────────────────────────────────────────────────
  const handleUpload = useCallback(async () => {
    if (!file || !date) return;

    setUploading(true);
    setUploadError(null);
    setUploadSuccess(null);

    try {
      const form = new FormData();
      form.append("date", date);
      form.append("file", file);

      const res = await fetch("/api/market-data/upload", {
        method: "POST",
        body: form,
      });

      const body = await res.json();

      if (!res.ok) {
        throw new Error(
          body?.error ?? `Server returned ${res.status}: ${res.statusText}`,
        );
      }

      setUploadSuccess(`Market data for ${body.date ?? date} cached successfully.`);
      setFile(null);
      if (fileInputRef.current) fileInputRef.current.value = "";
      await fetchEntries();
    } catch (err) {
      setUploadError(
        err instanceof Error ? err.message : "Upload failed. Please try again.",
      );
    } finally {
      setUploading(false);
    }
  }, [file, date, fetchEntries]);

  // ── Delete ─────────────────────────────────────────────────
  const handleDelete = useCallback(
    async (entryDate: string) => {
      setDeletingDate(entryDate);
      try {
        const res = await fetch(
          `/api/market-data/cache?date=${encodeURIComponent(entryDate)}`,
          { method: "DELETE" },
        );
        if (!res.ok) {
          const body = await res.json().catch(() => ({}));
          throw new Error(body?.error ?? `Server returned ${res.status}`);
        }
        await fetchEntries();
      } catch (err) {
        setUploadError(
          err instanceof Error ? err.message : "Failed to delete entry.",
        );
      } finally {
        setDeletingDate(null);
      }
    },
    [fetchEntries],
  );

  // ── Helpers ────────────────────────────────────────────────
  const todayStr = new Date().toISOString().slice(0, 10);

  const formatUploadedAt = (iso: string) => {
    try {
      return new Date(iso).toLocaleString(undefined, {
        dateStyle: "medium",
        timeStyle: "short",
      });
    } catch {
      return iso;
    }
  };

  const canUpload = !!file && !!date && !uploading;

  return (
    <div
      className="mdu-overlay"
      role="dialog"
      aria-modal="true"
      aria-label="Market Data Cache"
    >
      {/* Backdrop */}
      <div className="mdu-backdrop" onClick={onClose} />

      <div className="mdu-modal">
        {/* Header */}
        <div className="mdu-header">
          <h2>📋 Market Data Cache</h2>
          <button
            type="button"
            className="mdu-close-btn"
            onClick={onClose}
            aria-label="Close"
          >
            ✕
          </button>
        </div>

        {/* Body */}
        <div className="mdu-body">
          {/* ── Upload section ── */}
          <section>
            <h3 className="mdu-section-title">Upload XML Document</h3>
            <div className="mdu-form">
              {/* Date picker */}
              <div className="mdu-field">
                <label htmlFor="mdu-date" className="mdu-label">
                  Date
                </label>
                <input
                  id="mdu-date"
                  type="date"
                  className="mdu-input"
                  value={date}
                  onChange={(e) => setDate(e.target.value)}
                  disabled={uploading}
                />
              </div>

              {/* File drop zone */}
              <div className="mdu-field">
                <label className="mdu-label">XML File</label>
                <div
                  className={[
                    "mdu-dropzone",
                    dragging ? "mdu-dropzone-active" : "",
                    file ? "mdu-dropzone-has-file" : "",
                  ]
                    .filter(Boolean)
                    .join(" ")}
                  onDragOver={handleDragOver}
                  onDragLeave={handleDragLeave}
                  onDrop={handleDrop}
                  onClick={() => fileInputRef.current?.click()}
                  role="button"
                  tabIndex={0}
                  onKeyDown={(e) => {
                    if (e.key === "Enter" || e.key === " ")
                      fileInputRef.current?.click();
                  }}
                  aria-label="Click or drag an XML file here"
                >
                  <input
                    ref={fileInputRef}
                    type="file"
                    accept=".xml,application/xml,text/xml"
                    className="mdu-dropzone-input"
                    onChange={handleInputChange}
                    disabled={uploading}
                    tabIndex={-1}
                    onClick={(e) => e.stopPropagation()}
                  />
                  {file ? (
                    <>
                      <span className="mdu-dropzone-icon">✅</span>
                      <span className="mdu-file-name">{file.name}</span>
                      <span className="mdu-dropzone-hint">
                        {(file.size / 1024).toFixed(1)} KB — click to replace
                      </span>
                    </>
                  ) : (
                    <>
                      <span className="mdu-dropzone-icon">📄</span>
                      <span className="mdu-dropzone-label">
                        Drop XML file here or click to browse
                      </span>
                      <span className="mdu-dropzone-hint">
                        Accepts .xml files (Publication_MarketDocument)
                      </span>
                    </>
                  )}
                </div>
              </div>

              {/* Status messages */}
              {uploadSuccess && (
                <p className="mdu-status mdu-status-success">✓ {uploadSuccess}</p>
              )}
              {uploadError && (
                <p className="mdu-status mdu-status-error">✗ {uploadError}</p>
              )}

              {/* Upload button */}
              <div className="mdu-actions">
                <button
                  type="button"
                  className="mdu-btn mdu-btn-primary"
                  onClick={handleUpload}
                  disabled={!canUpload}
                >
                  {uploading ? "⏳ Uploading…" : "⬆ Upload to Cache"}
                </button>
              </div>
            </div>
          </section>

          {/* ── Cached entries section ── */}
          <section>
            <h3 className="mdu-section-title">
              Cached Documents
              {entries.length > 0 && (
                <span style={{ fontWeight: 400, fontSize: "0.8125rem", color: "var(--color-text-secondary)", marginLeft: "0.5rem" }}>
                  ({entries.length})
                </span>
              )}
            </h3>

            {loadingEntries ? (
              <div className="mdu-loading">Loading…</div>
            ) : entries.length === 0 ? (
              <p className="mdu-entries-empty">
                No cached documents yet. Upload an XML file above to bypass the
                ENTSO-E API for a specific date.
              </p>
            ) : (
              <div className="mdu-entries-list">
                {entries.map((entry) => {
                  const isToday = entry.date === todayStr;
                  const isTomorrow =
                    entry.date ===
                    new Date(Date.now() + 86_400_000).toISOString().slice(0, 10);
                  const label = isToday
                    ? "Today"
                    : isTomorrow
                      ? "Tomorrow"
                      : null;

                  return (
                    <div key={entry.date} className="mdu-entry">
                      <div className="mdu-entry-info">
                        <span className="mdu-entry-date">{entry.date}</span>
                        <span className="mdu-entry-meta">
                          Cached {formatUploadedAt(entry.uploaded_at)}
                        </span>
                      </div>

                      <div style={{ display: "flex", alignItems: "center", gap: "0.5rem" }}>
                        <span
                          className={`mdu-entry-badge ${entry.source === "upload" ? "mdu-badge-upload" : "mdu-badge-download"}`}
                          title={entry.source === "upload" ? "Uploaded manually" : "Downloaded from ENTSO-E API"}
                        >
                          {entry.source === "upload" ? "⬆ Uploaded" : "⬇ Downloaded"}
                        </span>
                        {label && (
                          <span
                            className={`mdu-entry-badge${isToday ? " mdu-badge-today" : ""}`}
                          >
                            {label}
                          </span>
                        )}
                        <a
                          href={`/api/market-data/download?date=${encodeURIComponent(entry.date)}`}
                          download={`Energy_Prices_${entry.date}.xml`}
                          className="mdu-download-btn"
                          title={`Download XML document for ${entry.date}`}
                          aria-label={`Download XML document for ${entry.date}`}
                        >
                          📥
                        </a>
                        <button
                          type="button"
                          className="mdu-delete-btn"
                          onClick={() => handleDelete(entry.date)}
                          disabled={deletingDate === entry.date}
                          title={`Remove cached data for ${entry.date}`}
                          aria-label={`Delete cache entry for ${entry.date}`}
                        >
                          {deletingDate === entry.date ? "⏳" : "🗑"}
                        </button>
                      </div>
                    </div>
                  );
                })}
              </div>
            )}
          </section>
        </div>
      </div>
    </div>
  );
}