/*
===========================================================================

wiring_weather.go - the climate cycle's composition

Reads SRO_WEATHER once at boot. On, every climate of the reference data
starts its weather cycle on the runtime's clock (world/climate owns the
cycle, action/weather.go its delivery), the default; off, no weather is
ever sent and every client stays clear.

===========================================================================
*/
package main

import (
	"os"

	log "github.com/sirupsen/logrus"

	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/game/world/climate"
)

/*
================
startWeatherFromEnv
================
*/
func startWeatherFromEnv(items *action.Runtime) {
	if !climate.Enabled(os.Getenv(climate.Env)) {
		return
	}
	items.StartWeather(items.Now().UnixMilli(), nil)
	log.Infof("weather: %d climates run their rain, snow and clear cycle (%s)", climate.Count, climate.Env)
}
