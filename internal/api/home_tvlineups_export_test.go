package api

// SetGracenoteBaseForTest points the TV listings at a fake server and gives
// back a function that puts the real one back.
func SetGracenoteBaseForTest(base string) (restore func()) {
	old := gracenoteBase
	gracenoteBase = base
	providerCache.Range(func(k, _ any) bool { providerCache.Delete(k); return true })
	guideMu.Lock()
	guideCache = map[string]guideFound{}
	guideMu.Unlock()
	return func() { gracenoteBase = old }
}
