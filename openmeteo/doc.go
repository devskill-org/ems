// Package openmeteo provides a Go client for the Open-Meteo solar forecast API.
//
// This package allows you to retrieve solar irradiance forecast data from the
// Open-Meteo API (https://open-meteo.com). It supports both hourly and 15-minute
// resolution forecasts for shortwave radiation, direct radiation, diffuse radiation,
// and direct normal irradiance.
//
// Basic Usage:
//
//	client := openmeteo.NewClient()
//
//	params := openmeteo.QueryParams{
//		Location: openmeteo.Location{
//			Latitude:  56.9529,
//			Longitude: 24.1114,
//		},
//		ForecastDays: 3,
//		Timezone:     "UTC",
//	}
//
//	forecast, err := client.GetSolarForecast(params)
//	if err != nil {
//		log.Fatal(err)
//	}
//
//	// Use forecast data...
//	for _, dp := range forecast.DataPoints() {
//		fmt.Printf("Time: %v, GHI: %.1f W/m², DNI: %.1f W/m²\n",
//			dp.Time,
//			dp.ShortwaveRadiation,
//			dp.DirectNormalIrradiance)
//	}
//
// The client fetches data from the Open-Meteo forecast endpoint at
// https://api.open-meteo.com/v1/forecast and returns both minutely_15 and hourly
// resolution data. The DataPoints method merges both resolutions into a single
// sorted slice, preferring the higher-resolution minutely_15 data when available.
//
// For more information about the API, visit: https://open-meteo.com/en/docs
package openmeteo