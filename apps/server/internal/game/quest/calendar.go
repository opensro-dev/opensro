package quest

// The server's prerequisite check: mode 1 admits before 04:00 or from 20:00; mode 2 admits
// 04:00 through 19:59. LuaStartDayLimit is a shared first-come quota,
// not a character deadline. Time already spent on a quest is unaffected.
func questHourAvailable(mode, hour uint8) bool {
	if hour > 23 {
		return false
	}
	switch mode {
	case 0:
		return true
	case 1:
		return hour < 4 || hour >= 20
	case 2:
		return hour >= 4 && hour < 20
	default:
		return false
	}
}

func (rt *Runtime) calendarAvailableLocked(def *Definition, marker bool) bool {
	if def.DayOrNight == 0 {
		return true
	}
	return rt.CalendarNow != nil && questHourAvailable(def.DayOrNight, rt.CalendarNow().Hour) &&
		(marker || def.PeriodStartLimit == 0 || rt.periodStarts[def.RefID] > 0)
}

func (rt *Runtime) calendarAvailable(def *Definition, marker bool) bool {
	rt.calendarMu.Lock()
	defer rt.calendarMu.Unlock()
	return rt.calendarAvailableLocked(def, marker)
}

// The server's cancel path restores a slot after voluntary cancellation.
// Successful completion of the ordinary generic quest does not restore it;
// expiry invokes the server's abort-and-cleanup routine directly and likewise does not run this release wrapper.
func (rt *Runtime) releasePeriodStart(def *Definition) {
	if def.PeriodStartLimit == 0 {
		return
	}
	rt.calendarMu.Lock()
	defer rt.calendarMu.Unlock()
	if rt.periodStarts[def.RefID] < def.PeriodStartLimit {
		rt.periodStarts[def.RefID]++
	}
}

// Existing simulation/action ticks drive the native 5000ms definition pulse
// (the server's quest-definition tick). The native reset requires adjacent sampled hours 19->20
// or 3->4. A delayed pulse must not invent skipped reset transitions.
func (rt *Runtime) AdvanceCalendar(nowMs int64) {
	rt.calendarMu.Lock()
	defer rt.calendarMu.Unlock()
	if nowMs < rt.calendarNextMs || rt.CalendarNow == nil {
		return
	}
	rt.calendarNextMs = nowMs + 5000
	hour := rt.CalendarNow().Hour
	if hour == rt.calendarHour {
		return
	}
	for _, def := range rt.Defs.All() {
		if def.PeriodStartLimit > 0 && (def.DayOrNight == 1 && rt.calendarHour == 19 && hour == 20 || def.DayOrNight == 2 && rt.calendarHour == 3 && hour == 4) {
			rt.periodStarts[def.RefID] = def.PeriodStartLimit
		}
	}
	rt.calendarHour = hour
}
