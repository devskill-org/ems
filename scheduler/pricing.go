package scheduler

import (
	"context"
	"fmt"
	"time"

	"github.com/devskill-org/ems/entsoe"
)

// StoreMarketDataXML parses the given XML bytes and stores the resulting document
// in the XML document cache under the given date key (YYYY-MM-DD format).
func (s *MinerScheduler) StoreMarketDataXML(date string, xmlData []byte) error {
	if err := s.xmlCache.Store(date, xmlData); err != nil {
		return err
	}
	go s.refreshMarketDataAndMPC()
	return nil
}

// DeleteMarketDataXML removes the cached XML document for the given date key.
func (s *MinerScheduler) DeleteMarketDataXML(date string) {
	s.xmlCache.Delete(date)
	go s.refreshMarketDataAndMPC()
}

// refreshMarketDataAndMPC invalidates cached prices market data and triggers GetMarketData
// followed by RunMPCOptimize to ensure MPC optimization decisions are updated immediately.
func (s *MinerScheduler) refreshMarketDataAndMPC() {
	s.mu.Lock()
	s.pricesMarketData = nil
	s.pricesMarketDataExpiry = time.Time{}
	s.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if _, err := s.GetMarketData(ctx); err != nil {
		s.logger.Printf("Warning: failed to refresh market data after cache update: %v", err)
	}

	if err := s.RunMPCOptimize(ctx); err != nil {
		s.logger.Printf("Warning: failed to run MPC optimization after market data cache update: %v", err)
	}
}

// GetXMLCacheEntries returns a snapshot of all entries currently held in the
// XML document cache.
func (s *MinerScheduler) GetXMLCacheEntries() []entsoe.CacheEntry {
	return s.xmlCache.ListEntries()
}

// GetPricesMarketData returns the cached PublicationMarketData without downloading
func (s *MinerScheduler) GetPricesMarketData() *entsoe.PublicationMarketData {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.pricesMarketData
}

// GetMarketData returns the latest PublicationMarketData, downloading new data if needed
func (s *MinerScheduler) GetMarketData(ctx context.Context) (*entsoe.PublicationMarketData, error) {

	location, err := time.LoadLocation(s.config.Location)
	if err != nil {
		return nil, err
	}

	now := time.Now().In(location)

	s.mu.RLock()
	marketData := s.pricesMarketData
	expiry := s.pricesMarketDataExpiry
	s.mu.RUnlock()

	// Check if we have cached data and it hasn't expired
	if marketData != nil && now.Before(expiry) {
		return marketData, nil
	}

	// Cache expired or no cached document, download new data
	if marketData != nil {
		s.logger.Printf("Cached pricing data expired at %s, downloading new PublicationMarketData...", expiry.Format(time.RFC3339))
	} else {
		s.logger.Printf("No cached pricing data available, downloading new PublicationMarketData...")
	}

	// Calculate next expiry time at 13:30 — 30 minutes before the 14:00 ENTSO-E
	// publish time so the MarketDataRefresh task has a window to pre-download the
	// next day's prices before PriceCheck and MPC need them.
	nextExpiry := time.Date(now.Year(), now.Month(), now.Day(), 13, 30, 0, 0, location)

	// Fetch next-day data when the current time is at or past today's 13:30.
	fetchNextDay := !now.Before(nextExpiry)

	// If it's already past 13:30 today, set expiry to 13:30 tomorrow
	if fetchNextDay {
		nextExpiry = nextExpiry.Add(24 * time.Hour)
	}

	// Perform the network download WITHOUT holding the lock so other goroutines
	// (GetConfig, runStateCheck, etc.) are never blocked during I/O.
	newDoc, err := entsoe.DownloadPublicationMarketData(ctx, s.config.SecurityToken, s.config.URLFormat, location, fetchNextDay, s.xmlCache)
	if err != nil {
		return nil, fmt.Errorf("failed to download PublicationMarketData: %w", err)
	}

	// Re-acquire the write lock only to store the result.
	// Double-check: another goroutine may have already refreshed while we were
	// downloading; keep whichever copy is fresher.
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.pricesMarketData != nil && now.Before(s.pricesMarketDataExpiry) {
		// Another goroutine beat us to it — return the already-cached copy.
		s.logger.Printf("PublicationMarketData was refreshed concurrently, using existing cache")
		return s.pricesMarketData, nil
	}

	// Store as latest with expiry time
	s.pricesMarketData = newDoc
	s.pricesMarketDataExpiry = nextExpiry

	s.logger.Printf("Successfully downloaded new PublicationMarketData, cache expires at %s", nextExpiry.Format(time.RFC3339))
	return newDoc, nil
}

// runMarketDataRefresh proactively refreshes the market data cache if it has
// expired. It is meant to be run as a frequent periodic task (every minute)
// so that PriceCheck and MPC always find a warm cache and never have to block
// on a download themselves.
//
// The cache expiry is set to 13:30, giving this task a 30-minute window to
// successfully pre-fetch the next day's ENTSO-E prices before the 14:00
// PriceCheck and MPC tasks need them.
func (s *MinerScheduler) runMarketDataRefresh(ctx context.Context) error {
	location, err := time.LoadLocation(s.config.Location)
	if err != nil {
		return fmt.Errorf("failed to load location: %w", err)
	}

	now := time.Now().In(location)

	// Cleanup past-day entries from the XML document cache on every tick.
	// YYYY-MM-DD strings sort lexicographically, so a simple string comparison
	// is sufficient to identify stale entries.
	todayKey := now.Format("2006-01-02")
	if removed := s.xmlCache.Cleanup(todayKey); removed > 0 {
		s.logger.Printf("[MarketDataRefresh] Removed %d past-day XML document(s) from cache", removed)
	}

	s.mu.RLock()
	marketData := s.pricesMarketData
	expiry := s.pricesMarketDataExpiry
	s.mu.RUnlock()

	// Cache is still warm — nothing to do.
	if marketData != nil && now.Before(expiry) {
		return nil
	}

	if marketData != nil {
		s.logger.Printf("[MarketDataRefresh] Cache expired at %s, pre-fetching new data...", expiry.Format(time.RFC3339))
	} else {
		s.logger.Printf("[MarketDataRefresh] No cached market data, fetching...")
	}

	if _, err = s.GetMarketData(ctx); err != nil {
		return fmt.Errorf("failed to refresh market data: %w", err)
	}

	s.logger.Printf("[MarketDataRefresh] Market data cache refreshed successfully")
	return nil
}

// runPriceCheck executes the main scheduler task
func (s *MinerScheduler) runPriceCheck(ctx context.Context) error {
	s.logger.Printf("Starting price check task at %s", time.Now().Format(time.RFC3339))

	// Step 1: Get current electricity price
	currentPrice, err := s.getCurrentPrice(ctx)
	if err != nil {
		s.logger.Printf("Error getting current price: %v", err)
		return err
	}

	s.logger.Printf("Current electricity price: %.2f EUR/MWh", currentPrice)
	s.logger.Printf("Price limit: %.2f EUR/MWh", s.config.PriceLimit)

	// Step 2: Manage miners based on price
	if err := s.manageMiners(ctx, currentPrice); err != nil {
		s.logger.Printf("Error managing miners: %v", err)
		return err
	}

	s.logger.Printf("Price check task completed successfully")
	return nil
}

// getCurrentPrice gets the current electricity price at the exact time, downloading new data if needed
func (s *MinerScheduler) getCurrentPrice(ctx context.Context) (float64, error) {
	location, err := time.LoadLocation(s.config.Location)
	if err != nil {
		return 0, fmt.Errorf("failed to load location: %w", err)
	}

	now := time.Now().In(location)

	marketData, err := s.GetMarketData(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to get market prices: %w", err)
	}

	if price, found := marketData.LookupPriceByTime(now); found {
		return price, nil
	}

	return 0, fmt.Errorf("price not found for time: %s", now.Format(time.RFC3339))
}
